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

package gitindex

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"

	git "github.com/libgit2/git2go"
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

// FindGitRepos finds directories holding git repositories.
func FindGitRepos(arg string) ([]string, error) {
	arg, err := filepath.Abs(arg)
	if err != nil {
		return nil, err
	}
	var dirs []string
	if err := filepath.Walk(arg, func(name string, fi os.FileInfo, err error) error {
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

type templates struct {
	repo, commit, file, line string
}

// RepoURL returns the canonical URL for a repo, based on its
// configured remotes.
func RepoURL(repoDir string) (*url.URL, error) {
	base, err := git.NewConfig()
	if err != nil {
		return nil, err
	}
	defer base.Free()
	cfg, err := git.OpenOndisk(base, filepath.Join(repoDir, "config"))
	if err != nil {
		return nil, err
	}
	defer cfg.Free()

	remoteURL, err := cfg.LookupString("remote.origin.url")
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return nil, err
	}

	return parsed, nil
}

// Templates fills in URL templates for known git hosting sites.
func Templates(u *url.URL) (*zoekt.Repository, error) {
	if strings.HasSuffix(u.Host, "googlesource.com") {
		/// eg. https://gerrit.googlesource.com/gitiles/+/master/tools/run_dev.sh#20
		return &zoekt.Repository{
			Name:                 filepath.Join(u.Host, u.Path),
			URL:                  u.String(),
			CommitURLTemplate:    u.String() + "/+/{{.Version}}",
			FileURLTemplate:      u.String() + "/+/{{.Version}}/{{.Path}}",
			LineFragmentTemplate: "{{.LineNumber}}",
		}, nil
	} else if u.Host == "github.com" {
		t := *u
		// CloneURL from the JSON API has .git
		t.Path = strings.TrimSuffix(t.Path, ".git")

		// eg. https://github.com/hanwen/go-fuse/blob/notify/genversion.sh#L10
		return &zoekt.Repository{
			Name:                 filepath.Join(t.Host, t.Path),
			URL:                  t.String(),
			CommitURLTemplate:    t.String() + "/commit/{{.Version}}",
			FileURLTemplate:      t.String() + "/blob/{{.Version}}/{{.Path}}",
			LineFragmentTemplate: "L{{.LineNumber}}",
		}, nil
	}

	return nil, fmt.Errorf("scheme unknown for URL %s", u)
}

// getCommit returns a tree object for the given reference.
func getCommit(repo *git.Repository, ref string) (*git.Commit, error) {
	obj, err := repo.RevparseSingle(ref)
	if err != nil {
		return nil, err
	}
	defer obj.Free()

	commitObj, err := obj.Peel(git.ObjectCommit)
	if err != nil {
		return nil, err
	}
	return commitObj.AsCommit()
}

// IndexGitRepo indexes the git repository as specified by the options and arguments.
func IndexGitRepo(opts build.Options, branchPrefix string, branches []string, submodules bool, repoCacheDir string) error {
	repo, err := git.OpenRepository(opts.RepoDir)
	if err != nil {
		return err
	}

	if url, err := RepoURL(opts.RepoDir); err != nil {
		log.Printf("RepoURL(%s): %s", opts.RepoDir, err)
	} else if desc, err := Templates(url); err != nil {
		log.Printf("Templates(%s): %s", url, err)
	} else {
		opts.RepositoryDescription.URL = desc.URL
		opts.RepositoryDescription.CommitURLTemplate = desc.CommitURLTemplate
		opts.RepositoryDescription.FileURLTemplate = desc.FileURLTemplate
		opts.RepositoryDescription.LineFragmentTemplate = desc.LineFragmentTemplate
	}

	repoCache := NewRepoCache(repoCacheDir)
	defer repoCache.Close()

	// branch => (path, sha1) => repo.
	repos := map[FileKey]BlobLocation{}

	// FileKey => branches
	branchMap := map[FileKey][]string{}

	// Branch => Repo => SHA1
	branchVersions := map[string]map[string]git.Oid{}
	for _, b := range branches {
		fullName := b
		if b != "HEAD" {
			fullName = filepath.Join(branchPrefix, b)
		} else {
			_, ref, err := repo.RevparseExt(b)
			if err != nil {
				return err
			}

			fullName = ref.Name()
			b = strings.TrimPrefix(fullName, branchPrefix)
		}
		commit, err := getCommit(repo, fullName)
		if err != nil {
			return err
		}
		defer commit.Free()
		opts.RepositoryDescription.Branches = append(opts.RepositoryDescription.Branches, zoekt.RepositoryBranch{
			Name:    b,
			Version: commit.Id().String(),
		})

		tree, err := commit.Tree()
		if err != nil {
			return err
		}
		defer tree.Free()

		files, subVersions, err := TreeToFiles(repo, tree, opts.RepositoryDescription.URL, repoCache)
		if err != nil {
			return err
		}
		for k, v := range files {
			repos[k] = v
			branchMap[k] = append(branchMap[k], b)
		}

		branchVersions[b] = subVersions
	}

	reposByPath := map[string]BlobLocation{}
	for key, location := range repos {
		reposByPath[key.SubRepoPath] = location
	}

	opts.SubRepositories = map[string]*zoekt.Repository{}
	for path, location := range reposByPath {
		tpl, err := Templates(location.URL)
		if err != nil {
			log.Printf("Templates(%s): %s", location.URL, err)
			tpl = &zoekt.Repository{URL: location.URL.String()}
		}
		opts.SubRepositories[path] = tpl
	}
	for _, br := range opts.RepositoryDescription.Branches {
		for path, repo := range opts.SubRepositories {
			id := branchVersions[br.Name][path]
			repo.Branches = append(repo.Branches, zoekt.RepositoryBranch{
				Name:    br.Name,
				Version: id.String(),
			})
		}
	}

	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

	var names []string
	fileKeys := map[string][]FileKey{}
	for key := range repos {
		n := key.FullPath()
		fileKeys[n] = append(fileKeys[n], key)
		names = append(names, n)
	}
	// not strictly necessary, but nice for reproducibility.
	sort.Strings(names)

	for _, name := range names {
		keys := fileKeys[name]
		for _, key := range keys {
			brs := branchMap[key]
			blob, err := repos[key].Repo.LookupBlob(&key.ID)
			if err != nil {
				return err
			}

			if blob.Size() > int64(opts.SizeMax) {
				continue
			}

			builder.Add(zoekt.Document{
				SubRepositoryPath: key.SubRepoPath,
				Name:              key.FullPath(),
				Content:           blob.Contents(),
				Branches:          brs,
			})
		}
	}
	return builder.Finish()
}
