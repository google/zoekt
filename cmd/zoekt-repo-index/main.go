// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/* zoekt-repo-index indexes a repo-based repository.  The constituent
git repositories should already have been downloaded to the
--repo_cache directory, eg.

    go install github.com/google/zoekt/cmd/zoekt-repo-index &&

    zoekt-repo-index -base_url https://android.googlesource.com/ \
      -name Android \
      -manifest_repo ~/android-orig/.repo/manifests.git/ \
      -manifest_rev_prefix=refs/remotes/origin/ \
      -rev_prefix="refs/remotes/aosp/" \
      --repo_cache ~/android-repo-cache/ \
      -shard_limit 50000000
       master:default.xml
*/
package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/slothfs/manifest"
	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	gitindex "github.com/google/zoekt/git"
	git "github.com/libgit2/git2go"
)

var _ = log.Println

type branchFile struct {
	branch, file string
	mf           *manifest.Manifest
	manifestPath string
}

func parseBranches(manifestRepo, revPrefix string, args []string) ([]branchFile, error) {
	var branches []branchFile
	if manifestRepo != "" {
		repo, err := git.OpenRepository(manifestRepo)
		if err != nil {
			log.Fatalf("OpenRepository(%s): %v", manifestRepo, err)
		}
		for _, f := range args {
			fs := strings.SplitN(f, ":", 2)
			if len(fs) != 2 {
				return nil, fmt.Errorf("cannot parse %q as BRANCH:FILE", f)
			}

			mf, err := getManifest(repo, revPrefix+fs[0], fs[1])
			if err != nil {
				return nil, fmt.Errorf("manifest %s:%s: %v", fs[0], fs[1], err)
			}

			branches = append(branches, branchFile{
				branch:       fs[0],
				file:         fs[1],
				mf:           mf,
				manifestPath: manifestRepo,
			})
		}
		repo.Free()
	} else {
		if len(args) == 0 {
			return nil, fmt.Errorf("must give XML file argument")
		}
		for _, f := range args {
			mf, err := manifest.ParseFile(f)
			if err != nil {
				return nil, err
			}

			branches = append(branches, branchFile{
				file:         filepath.Base(f),
				mf:           mf,
				manifestPath: f,
			})
		}
	}
	return branches, nil
}

func main() {
	var sizeMax = flag.Int("file_limit", 128<<10, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 1, "maximum number of parallel indexing processes")

	revPrefix := flag.String("rev_prefix", "refs/remotes/origin/", "prefix for references")
	baseURLStr := flag.String("base_url", "", "base url to interpret repository names")
	repoCacheDir := flag.String("repo_cache", "", "root for repository cache")
	indexDir := flag.String("index", build.DefaultDir, "index directory for *.zoekt files")
	manifestRepo := flag.String("manifest_repo", "", "set path a git repository holding manifest XML file. Provide the BRANCH:XML-FILE as further command-line arguments")
	manifestRevPrefix := flag.String("manifest_rev_prefix", "refs/remotes/origin/", "prefixes for branches in manifest repository")
	repoName := flag.String("name", "", "set repository name")
	repoURL := flag.String("url", "", "set repository URL")
	flag.Parse()

	if *repoCacheDir == "" {
		log.Fatal("must set --repo_cache")
	}
	repoCache := newRepoCache(*repoCacheDir)

	opts := build.Options{
		Parallelism: *parallelism,
		SizeMax:     *sizeMax,
		ShardMax:    *shardLimit,
		IndexDir:    *indexDir,
		RepositoryDescription: zoekt.Repository{
			Name: *repoName,
			URL:  *repoURL,
		},
	}
	opts.SetDefaults()
	baseURL, err := url.Parse(*baseURLStr)
	if err != nil {
		log.Fatal("Parse baseURL %q: %v", baseURLStr, err)
	}

	branches, err := parseBranches(*manifestRepo, *manifestRevPrefix, flag.Args())
	if err != nil {
		log.Fatal(err)
	}
	if len(branches) == 0 {
		log.Fatal("must specify at least one branch")
	}

	opts.RepoDir = branches[0].manifestPath
	perBranch := map[string]map[locationKey]locator{}
	for _, br := range branches {
		br.mf.Filter()
		files, err := iterateManifest(br.mf, *baseURL, *revPrefix, repoCache)
		if err != nil {
			log.Fatal("iterateManifest", err)
		}

		key := br.branch + ":" + br.file
		perBranch[key] = files
	}

	// key => branch
	all := map[locationKey][]string{}
	for br, files := range perBranch {
		for k := range files {
			all[k] = append(all[k], br)
		}
	}

	builder, err := build.NewBuilder(opts)
	if err != nil {
		log.Fatal(err)
	}

	for k, branches := range all {
		loc := perBranch[branches[0]][k]
		data, err := loc.Blob(&k.id)
		if err != nil {
			log.Fatal(err)
		}
		doc := zoekt.Document{
			Name:    k.path,
			Content: data,
		}

		for _, br := range branches {
			doc.Branches = append(doc.Branches, br)
		}

		builder.Add(doc)
	}
	builder.Finish()

}

// getManifest parses the manifest XML at the given branch/path inside a Git repository.
func getManifest(repo *git.Repository, branch, path string) (*manifest.Manifest, error) {
	obj, err := repo.RevparseSingle(branch + ":" + path)
	if err != nil {
		return nil, err
	}
	defer obj.Free()
	blob, err := obj.AsBlob()
	if err != nil {
		return nil, err
	}
	return manifest.Parse(blob.Contents())
}

// locator holds data where a file can be found. It's a struct so we
// can insert additional data into the index (eg. subrepository URLs).
type locator struct {
	repo *git.Repository
}

func (l *locator) Blob(id *git.Oid) ([]byte, error) {
	blob, err := l.repo.LookupBlob(id)
	if err != nil {
		return nil, err
	}
	defer blob.Free()
	return blob.Contents(), nil
}

type repoCache interface {
	open(url *url.URL) (*git.Repository, error)
}

type repoCacheImpl struct {
	baseDir string

	reposMu sync.Mutex
	repos   map[string]*git.Repository
}

func newRepoCache(dir string) *repoCacheImpl {
	return &repoCacheImpl{
		baseDir: dir,
		repos:   make(map[string]*git.Repository),
	}
}

func (rc *repoCacheImpl) open(u *url.URL) (*git.Repository, error) {
	key := filepath.Join(u.Host, u.Path)
	if !strings.HasSuffix(key, ".git") {
		key += ".git"
	}

	rc.reposMu.Lock()
	defer rc.reposMu.Unlock()

	r := rc.repos[key]
	if r != nil {
		return r, nil
	}

	d := filepath.Join(rc.baseDir, key)
	repo, err := git.OpenRepository(d)
	if err == nil {
		rc.repos[key] = repo
	}
	return repo, err
}

// locationKey is a single file version (possibly from multiple
// branches).
type locationKey struct {
	path string
	id   git.Oid
}

// iterateManifest constructs a complete tree from the given Manifest.
func iterateManifest(mf *manifest.Manifest,
	baseURL url.URL, revPrefix string,
	cache repoCache) (map[locationKey]locator, error) {
	allFiles := map[locationKey]locator{}
	for _, p := range mf.Project {
		rev := mf.ProjectRevision(&p)

		projURL := baseURL
		projURL.Path = path.Join(projURL.Path, p.Name)

		repo, err := cache.open(&projURL)
		if err != nil {
			return nil, err
		}

		obj, err := repo.RevparseSingle(revPrefix + rev + ":")
		if err != nil {
			return nil, fmt.Errorf("RevparseSingle(%s, %s): %v", p.Name, rev, err)
		}
		// Since the number of projects is small, it's OK to
		// free this at the end of the function.
		defer obj.Free()
		tree, err := obj.AsTree()
		if err != nil {
			return nil, err
		}

		submodules := false
		files, _, err := gitindex.TreeToFiles(repo, tree, submodules)
		if err != nil {
			return nil, err
		}

		for path, sha := range files {
			fullPath := filepath.Join(p.GetPath(), path)
			allFiles[locationKey{fullPath, sha}] = locator{
				repo: repo,
			}
		}
	}

	return allFiles, nil
}
