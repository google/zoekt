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

// Package gitindex provides functions for indexing Git repositories.
package gitindex

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"

	git "gopkg.in/src-d/go-git.v4"
	plumcfg "gopkg.in/src-d/go-git.v4/plumbing/format/config"
)

// RepoModTime returns the time of last fetch of a git repository.
func RepoModTime(dir string) (time.Time, error) {
	var last time.Time
	refDir := filepath.Join(dir, "refs")
	if _, err := os.Lstat(refDir); err == nil {
		if err := filepath.Walk(refDir,
			func(name string, fi os.FileInfo, err error) error {
				if !fi.IsDir() && last.Before(fi.ModTime()) {
					last = fi.ModTime()
				}
				return nil
			}); err != nil {
			return last, err
		}
	}

	// git gc compresses refs into the following file:
	for _, fn := range []string{"info/refs", "packed-refs"} {
		if fi, err := os.Lstat(filepath.Join(dir, fn)); err == nil && !fi.IsDir() && last.Before(fi.ModTime()) {
			last = fi.ModTime()
		}
	}

	return last, nil
}

// FindGitRepos finds directories holding git repositories below the
// given directory. It will find both bare and the ".git" dirs in
// non-bare repositories. It returns the full path including the dir
// passed in.
func FindGitRepos(dir string) ([]string, error) {
	arg, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	if err := filepath.Walk(arg, func(name string, fi os.FileInfo, err error) error {
		// Best-effort, ignore filepath.Walk failing
		if err != nil {
			return nil
		}

		if fi, err := os.Lstat(filepath.Join(name, ".git")); err == nil && fi.IsDir() {
			dirs = append(dirs, filepath.Join(name, ".git"))
			return filepath.SkipDir
		}

		if !strings.HasSuffix(name, ".git") || !fi.IsDir() {
			return nil
		}

		fi, err = os.Lstat(filepath.Join(name, "objects"))
		if err != nil || !fi.IsDir() {
			return nil
		}

		dirs = append(dirs, name)
		return filepath.SkipDir
	}); err != nil {
		return nil, err
	}

	return dirs, nil
}

// setTemplates fills in URL templates for known git hosting
// sites.
func setTemplates(repo *zoekt.Repository, u *url.URL, typ string) error {
	repo.URL = u.String()
	switch typ {
	case "gitiles":
	case "browse": // as of gerrit 3, the default gitiles weblink is nammd browse
		/// eg. https://gerrit.googlesource.com/gitiles/+/master/tools/run_dev.sh#20
		repo.CommitURLTemplate = u.String() + "/+/{{.Version}}"
		repo.FileURLTemplate = u.String() + "/+/{{.Version}}/{{.Path}}"
		repo.LineFragmentTemplate = "#{{.LineNumber}}"
	case "github":
		// eg. https://github.com/hanwen/go-fuse/blob/notify/genversion.sh#L10
		repo.CommitURLTemplate = u.String() + "/commit/{{.Version}}"
		repo.FileURLTemplate = u.String() + "/blob/{{.Version}}/{{.Path}}"
		repo.LineFragmentTemplate = "#L{{.LineNumber}}"
	case "cgit":
		// http://git.savannah.gnu.org/cgit/lilypond.git/tree/elisp/lilypond-mode.el?h=dev/philh&id=b2ca0fefe3018477aaca23b6f672c7199ba5238e#n100
		repo.CommitURLTemplate = u.String() + "/commit/?id={{.Version}}"
		repo.FileURLTemplate = u.String() + "/tree/{{.Path}}/?id={{.Version}}"
		repo.LineFragmentTemplate = "#n{{.LineNumber}}"
	case "gitweb":
		// https://gerrit.libreoffice.org/gitweb?p=online.git;a=blob;f=Makefile.am;h=cfcfd7c36fbae10e269653dc57a9b68c92d4c10b;hb=848145503bf7b98ce4a4aa0a858a0d71dd0dbb26#l10
		repo.FileURLTemplate = u.String() + ";a=blob;f={{.Path}};hb={{.Version}}"
		repo.CommitURLTemplate = u.String() + ";a=commit;h={{.Version}}"
		repo.LineFragmentTemplate = "#l{{.LineNumber}}"
	case "source.bazel.build":
		// https://source.bazel.build/bazel/+/57bc201346e61c62a921c1cbf32ad24f185c10c9
		// https://source.bazel.build/bazel/+/57bc201346e61c62a921c1cbf32ad24f185c10c9:tools/cpp/BUILD.empty;l=10
		repo.CommitURLTemplate = u.String() + "/+/{{.Version}}"
		repo.FileURLTemplate = u.String() + "/+/{{.Version}}:{{.Path}}"
		repo.LineFragmentTemplate = ";l={{.LineNumber}}"
	case "bitbucket-server":
		// https://<bitbucketserver-host>/projects/<project>/repos/<repo>/commits/5be7ca73b898bf17a08e607918accfdeafe1e0bc
		// https://<bitbucketserver-host>/projects/<project>/repos/<repo>/browse/<file>?at=5be7ca73b898bf17a08e607918accfdeafe1e0bc
		repo.CommitURLTemplate = u.String() + "/commits/{{.Version}}"
		repo.FileURLTemplate = u.String() + "/{{.Path}}?at={{.Version}}"
		repo.LineFragmentTemplate = "#{{.LineNumber}}"
	case "gitlab":
		repo.CommitURLTemplate = u.String() + "/commit/{{.Version}}"
		repo.FileURLTemplate = u.String() + "/blob/{{.Version}}/{{.Path}}"
		repo.LineFragmentTemplate = "#L{{.LineNumber}}"
	default:
		return fmt.Errorf("URL scheme type %q unknown", typ)
	}
	return nil
}

// getCommit returns a tree object for the given reference.
func getCommit(repo *git.Repository, prefix, ref string) (*object.Commit, error) {
	sha1, err := repo.ResolveRevision(plumbing.Revision(ref))
	// ref might be a branch name (e.g. "master") add branch prefix and try again.
	if err != nil {
		sha1, err = repo.ResolveRevision(plumbing.Revision(filepath.Join(prefix, ref)))
	}
	if err != nil {
		return nil, err
	}

	commitObj, err := repo.CommitObject(*sha1)
	if err != nil {
		return nil, err
	}
	return commitObj, nil
}

func configLookupRemoteURL(cfg *config.Config, key string) string {
	rc := cfg.Remotes[key]
	if rc == nil || len(rc.URLs) == 0 {
		return ""
	}
	return rc.URLs[0]
}

func configLookupString(sec *plumcfg.Section, key string) string {
	for _, o := range sec.Options {
		if o.Key != key {
			continue
		}
		return o.Value
	}

	return ""
}

func isMissingBranchError(err error) bool {
	return err != nil && err.Error() == "reference not found"
}

func setTemplatesFromConfig(desc *zoekt.Repository, repoDir string) error {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return err
	}

	cfg, err := repo.Config()
	if err != nil {
		return err
	}

	sec := cfg.Raw.Section("zoekt")

	webURLStr := configLookupString(sec, "web-url")
	webURLType := configLookupString(sec, "web-url-type")

	if webURLType != "" && webURLStr != "" {
		webURL, err := url.Parse(webURLStr)
		if err != nil {
			return err
		}
		if err := setTemplates(desc, webURL, webURLType); err != nil {
			return err
		}
	}

	name := configLookupString(sec, "name")
	if name != "" {
		desc.Name = name
	} else {
		remoteURL := configLookupRemoteURL(cfg, "origin")
		if remoteURL == "" {
			return nil
		}
		u, err := url.Parse(remoteURL)
		if err != nil {
			return err
		}
		if err := SetTemplatesFromOrigin(desc, u); err != nil {
			return err
		}
	}

	if desc.RawConfig == nil {
		desc.RawConfig = map[string]string{}
	}
	for _, o := range sec.Options {
		desc.RawConfig[o.Key] = o.Value
	}

	// Ranking info.

	// Github:
	traction := 0
	for _, s := range []string{"github-stars", "github-forks", "github-watchers", "github-subscribers"} {
		f, err := strconv.Atoi(configLookupString(sec, s))
		if err == nil {
			traction += f
		}
	}

	if strings.Contains(desc.Name, "googlesource.com/") && traction == 0 {
		// Pretend everything on googlesource.com has 1000
		// github stars.
		traction = 1000
	}

	if traction > 0 {
		l := math.Log(float64(traction))
		desc.Rank = uint16((1.0 - 1.0/math.Pow(1+l, 0.6)) * 10000)
	}

	return nil
}

// SetTemplates fills in templates based on the origin URL.
func SetTemplatesFromOrigin(desc *zoekt.Repository, u *url.URL) error {
	desc.Name = filepath.Join(u.Host, strings.TrimSuffix(u.Path, ".git"))

	if strings.HasSuffix(u.Host, ".googlesource.com") {
		return setTemplates(desc, u, "gitiles")
	} else if u.Host == "github.com" {
		u.Path = strings.TrimSuffix(u.Path, ".git")
		return setTemplates(desc, u, "github")
	} else {
		return fmt.Errorf("unknown git hosting site %q", u)
	}
}

// The Options structs controls details of the indexing process.
type Options struct {
	// The repository to be indexed.
	RepoDir string

	// If set, follow submodule links. This requires RepoCacheDir to be set.
	Submodules bool

	// If set, skip indexing if the existing index shard is newer
	// than the refs in the repository.
	Incremental bool

	// Don't error out if some branch is missing
	AllowMissingBranch bool

	// Specifies the root of a Repository cache. Needed for submodule indexing.
	RepoCacheDir string

	// Indexing options.
	BuildOptions build.Options

	// Prefix of the branch to index, e.g. `remotes/origin`.
	BranchPrefix string

	// List of branch names to index, e.g. []string{"HEAD", "stable"}
	Branches []string
}

func expandBranches(repo *git.Repository, bs []string, prefix string) ([]string, error) {
	var result []string
	for _, b := range bs {
		if b == "HEAD" {
			ref, err := repo.Head()
			if err != nil {
				return nil, err
			}

			result = append(result, strings.TrimPrefix(ref.Name().String(), prefix))
			continue
		}

		if strings.Contains(b, "*") {
			iter, err := repo.Branches()
			if err != nil {
				return nil, err
			}

			defer iter.Close()
			for {
				ref, err := iter.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, err
				}

				name := ref.Name().Short()
				if matched, err := filepath.Match(b, name); err != nil {
					return nil, err
				} else if !matched {
					continue
				}

				result = append(result, strings.TrimPrefix(name, prefix))
			}
			continue
		}

		result = append(result, b)
	}

	return result, nil
}

// IndexGitRepo indexes the git repository as specified by the options.
func IndexGitRepo(opts Options) error {
	// Set max thresholds, since we use them in this function.
	opts.BuildOptions.SetDefaults()
	if opts.RepoDir == "" {
		return fmt.Errorf("gitindex: must set RepoDir")
	}

	opts.BuildOptions.RepositoryDescription.Source = opts.RepoDir
	repo, err := git.PlainOpen(opts.RepoDir)
	if err != nil {
		return err
	}

	if err := setTemplatesFromConfig(&opts.BuildOptions.RepositoryDescription, opts.RepoDir); err != nil {
		log.Printf("setTemplatesFromConfig(%s): %s", opts.RepoDir, err)
	}

	repoCache := NewRepoCache(opts.RepoCacheDir)

	// branch => (path, sha1) => repo.
	repos := map[fileKey]BlobLocation{}

	// fileKey => branches
	branchMap := map[fileKey][]string{}

	// Branch => Repo => SHA1
	branchVersions := map[string]map[string]plumbing.Hash{}

	branches, err := expandBranches(repo, opts.Branches, opts.BranchPrefix)
	if err != nil {
		return err
	}
	for _, b := range branches {
		commit, err := getCommit(repo, opts.BranchPrefix, b)
		if opts.AllowMissingBranch && isMissingBranchError(err) {
			continue
		}

		if err != nil {
			return err
		}
		opts.BuildOptions.RepositoryDescription.Branches = append(opts.BuildOptions.RepositoryDescription.Branches, zoekt.RepositoryBranch{
			Name:    b,
			Version: commit.Hash.String(),
		})

		tree, err := commit.Tree()
		if err != nil {
			return err
		}

		files, subVersions, err := TreeToFiles(repo, tree, opts.BuildOptions.RepositoryDescription.URL, repoCache)
		if err != nil {
			return err
		}
		for k, v := range files {
			repos[k] = v
			branchMap[k] = append(branchMap[k], b)
		}

		branchVersions[b] = subVersions
	}

	if opts.Incremental {
		versions := opts.BuildOptions.IndexVersions()
		if reflect.DeepEqual(versions, opts.BuildOptions.RepositoryDescription.Branches) {
			return nil
		}
	}

	reposByPath := map[string]BlobLocation{}
	for key, location := range repos {
		reposByPath[key.SubRepoPath] = location
	}

	opts.BuildOptions.SubRepositories = map[string]*zoekt.Repository{}
	for path, location := range reposByPath {
		tpl := opts.BuildOptions.RepositoryDescription
		if path != "" {
			tpl = zoekt.Repository{URL: location.URL.String()}
			if err := SetTemplatesFromOrigin(&tpl, location.URL); err != nil {
				log.Printf("setTemplatesFromOrigin(%s, %s): %s", path, location.URL, err)
			}
		}
		opts.BuildOptions.SubRepositories[path] = &tpl
	}
	for _, br := range opts.BuildOptions.RepositoryDescription.Branches {
		for path, repo := range opts.BuildOptions.SubRepositories {
			id := branchVersions[br.Name][path]
			repo.Branches = append(repo.Branches, zoekt.RepositoryBranch{
				Name:    br.Name,
				Version: id.String(),
			})
		}
	}

	builder, err := build.NewBuilder(opts.BuildOptions)
	if err != nil {
		return err
	}
	defer builder.Finish()

	var names []string
	fileKeys := map[string][]fileKey{}
	for key := range repos {
		n := key.FullPath()
		fileKeys[n] = append(fileKeys[n], key)
		names = append(names, n)
	}

	sort.Strings(names)
	names = uniq(names)

	for _, name := range names {
		keys := fileKeys[name]

		for _, key := range keys {
			brs := branchMap[key]
			blob, err := repos[key].Repo.BlobObject(key.ID)
			if err != nil {
				return err
			}

			if blob.Size > int64(opts.BuildOptions.SizeMax) && !opts.BuildOptions.IgnoreSizeMax(key.FullPath()) {
				if err := builder.Add(zoekt.Document{
					SkipReason:        fmt.Sprintf("file size %d exceeds maximum size %d", blob.Size, opts.BuildOptions.SizeMax),
					Name:              key.FullPath(),
					Branches:          brs,
					SubRepositoryPath: key.SubRepoPath,
				}); err != nil {
					return err
				}
				continue
			}

			contents, err := blobContents(blob)
			if err != nil {
				return err
			}
			if err := builder.Add(zoekt.Document{
				SubRepositoryPath: key.SubRepoPath,
				Name:              key.FullPath(),
				Content:           contents,
				Branches:          brs,
			}); err != nil {
				return err
			}
		}
	}
	return builder.Finish()
}

func blobContents(blob *object.Blob) ([]byte, error) {
	r, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	c, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func uniq(ss []string) []string {
	result := ss[:0]
	var last string
	for i, s := range ss {
		if i == 0 || s != last {
			result = append(result, s)
		}
		last = s
	}
	return result
}
