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
	"net/url"
	"path"
	"path/filepath"
	"strings"

	git "github.com/libgit2/git2go"
)

// repoWalker walks a tree, recursing into submodules.
type repoWalker struct {
	repo *git.Repository

	repoURL *url.URL
	tree    map[FileKey]BlobLocation

	// Path => SubmoduleEntry
	submodules map[string]*SubmoduleEntry

	// Path => commit SHA1
	subRepoVersions map[string]git.Oid
	err             error
	repoCache       *RepoCache

	// If set, don't gasp on missing submodules.
	ignoreMissingSubmodules bool
}

// subURL returns the URL for a submodule.
func (w *repoWalker) subURL(relURL string) (*url.URL, error) {
	if w.repoURL == nil {
		return nil, fmt.Errorf("no URL for base repo.")
	}
	if strings.HasPrefix(relURL, "../") {
		u := *w.repoURL
		u.Path = path.Join(u.Path, relURL)
		return &u, nil
	}

	return url.Parse(relURL)
}

// newRepoWalker creates a new repoWalker.
func newRepoWalker(r *git.Repository, repoURL string, repoCache *RepoCache) *repoWalker {
	u, _ := url.Parse(repoURL)
	return &repoWalker{
		repo:                    r,
		repoURL:                 u,
		tree:                    map[FileKey]BlobLocation{},
		repoCache:               repoCache,
		subRepoVersions:         map[string]git.Oid{},
		ignoreMissingSubmodules: true,
	}
}

// parseModuleMap initializes rw.submodules.
func (rw *repoWalker) parseModuleMap(t *git.Tree) error {
	modEntry := t.EntryByName(".gitmodules")
	if modEntry != nil {
		blob, err := rw.repo.LookupBlob(modEntry.Id)
		if err != nil {
			return err
		}

		mods, err := ParseGitModules(blob.Contents())
		if err != nil {
			return err
		}
		rw.submodules = map[string]*SubmoduleEntry{}
		for _, entry := range mods {
			rw.submodules[entry.Path] = entry
		}
	}
	return nil
}

// TreeToFiles fetches the blob SHA1s for a tree. If repoCache is
// non-nil, recurse into submodules. In addition, it returns a mapping
// that indicates in which repo each SHA1 can be found.
func TreeToFiles(r *git.Repository, t *git.Tree,
	repoURL string, repoCache *RepoCache) (map[FileKey]BlobLocation, map[string]git.Oid, error) {
	ref := newRepoWalker(r, repoURL, repoCache)

	if err := ref.parseModuleMap(t); err != nil {
		return nil, nil, err
	}

	t.Walk(ref.cbInt)
	if ref.err != nil {
		return nil, nil, ref.err
	}
	return ref.tree, ref.subRepoVersions, nil
}

// cb is the git2go callback
func (r *repoWalker) cb(n string, e *git.TreeEntry) error {
	p := filepath.Join(n, e.Name)
	if e.Type == git.ObjectCommit && r.repoCache != nil {
		submod := r.submodules[p]
		if submod == nil {
			if r.ignoreMissingSubmodules {
				return nil
			}
			return fmt.Errorf("in repo %s: no entry for submodule path %q", r.repoURL, p)
		}

		subURL, err := r.subURL(submod.URL)
		if err != nil {
			return err
		}

		subRepo, err := r.repoCache.Open(subURL)
		if err != nil {
			return err
		}

		obj, err := subRepo.Lookup(e.Id)
		if err != nil {
			return err
		}
		defer obj.Free()

		r.subRepoVersions[p] = *e.Id
		treeObj, err := obj.Peel(git.ObjectTree)
		if err != nil {
			return err
		}
		if treeObj != obj {
			defer treeObj.Free()
		}
		tree, err := treeObj.AsTree()
		if err != nil {
			return err
		}
		subTree, subVersions, err := TreeToFiles(subRepo, tree, subURL.String(), r.repoCache)
		if err != nil {
			return err
		}
		for k, repo := range subTree {
			r.tree[FileKey{
				SubRepoPath: filepath.Join(p, k.SubRepoPath),
				Path:        k.Path,
				ID:          k.ID,
			}] = repo
		}
		for k, v := range subVersions {
			r.subRepoVersions[filepath.Join(p, k)] = v
		}
		return nil
	}

	switch e.Filemode {
	case git.FilemodeBlob, git.FilemodeBlobExecutable:
	default:
		return nil
	}

	if e.Type != git.ObjectBlob {
		return nil
	}
	r.tree[FileKey{
		Path: p,
		ID:   *e.Id,
	}] = BlobLocation{
		Repo: r.repo,
		URL:  r.repoURL,
	}
	return nil
}

// cbInt is the callback suitable for use with git2go.
func (r *repoWalker) cbInt(n string, e *git.TreeEntry) int {
	err := r.cb(n, e)
	if err != nil {
		r.err = err
		return 1
	}
	return 0
}

// FileKey describes a blob at a location in the final tree. We also
// record the subrepository from where it came.
type FileKey struct {
	SubRepoPath string
	Path        string
	ID          git.Oid
}

func (k *FileKey) FullPath() string {
	return filepath.Join(k.SubRepoPath, k.Path)
}

// BlobLocation holds data where a blob can be found.
type BlobLocation struct {
	Repo *git.Repository
	URL  *url.URL
}

func (l *BlobLocation) Blob(id *git.Oid) ([]byte, error) {
	blob, err := l.Repo.LookupBlob(id)
	if err != nil {
		return nil, err
	}
	defer blob.Free()
	return blob.Contents(), nil
}
