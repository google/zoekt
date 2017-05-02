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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	"github.com/google/zoekt/query"
)

func createSubmoduleRepo(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	script := `mkdir adir bdir
cd adir
git init
mkdir subdir
echo acont > afile
echo sub-cont > subdir/sub-file
git add afile subdir/sub-file
git commit -am amsg

cd ..
cd bdir
git init
echo bcont > bfile
git add bfile
git commit -am bmsg

cd ../adir
git submodule add --name bname -- ../bdir bname
git commit -am bmodmsg
cat .gitmodules
cd ..
mkdir gerrit.googlesource.com
git clone --bare adir gerrit.googlesource.com/adir.git
git clone --bare bdir gerrit.googlesource.com/bdir.git

cat << EOF  > gerrit.googlesource.com/adir.git/config
[core]
	repositoryformatversion = 0
	filemode = true
	bare = true
[remote "origin"]
	url = http://gerrit.googlesource.com/adir
[branch "master"]
	remote = origin
	merge = refs/heads/master
EOF
`
	cmd := exec.Command("/bin/sh", "-euxc", script)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("execution error: %v, output %s", err, out)
	}
	return nil
}

func TestTreeToFiles(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}

	if err := createSubmoduleRepo(dir); err != nil {
		t.Fatalf("TempDir: %v", err)
	}

	cache := NewRepoCache(dir)
	defer cache.Close()

	aURL, _ := url.Parse("http://gerrit.googlesource.com/adir")
	repo, err := cache.Open(aURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	obj, err := repo.RevparseSingle("HEAD:")
	if err != nil {
		t.Fatalf("HEAD tree: %v", err)
	}

	defer obj.Free()
	tree, err := obj.AsTree()
	if err != nil {
		t.Fatalf("AsTree: %v", err)
	}

	files, versions, err := TreeToFiles(repo, tree, aURL.String(), cache)
	if err != nil {
		t.Fatalf("TreeToFiles: %v", err)
	}

	if e, v := tree.EntryByName("bname"), versions["bname"]; e == nil || bytes.Compare(e.Id[:], v[:]) != 0 {
		t.Fatalf("got 'bname' versions %v, want %v", v, e)
	}

	var paths []string
	for k := range files {
		paths = append(paths, k.FullPath())
	}
	sort.Strings(paths)

	want := []string{".gitmodules", "afile", "bname/bfile", "subdir/sub-file"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
}

func TestSubmoduleIndex(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}

	if err := createSubmoduleRepo(dir); err != nil {
		t.Fatalf("createSubmoduleRepo: %v", err)
	}

	indexDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(indexDir)

	buildOpts := build.Options{
		IndexDir: indexDir,
		RepoDir:  filepath.Join(dir, "gerrit.googlesource.com", "adir.git"),
	}
	buildOpts.SetDefaults()

	opts := Options{
		BuildOptions: buildOpts,
		BranchPrefix: "refs/heads/",
		Branches:     []string{"master"},
		Submodules:   true,
		Incremental:  true,
		RepoCacheDir: dir,
	}
	if err := IndexGitRepo(opts); err != nil {
		t.Fatalf("IndexGitRepo: %v", err)
	}

	searcher, err := zoekt.NewShardedSearcher(indexDir)
	if err != nil {
		t.Fatal("NewShardedSearcher", err)
	}
	defer searcher.Close()

	results, err := searcher.Search(context.Background(),
		&query.Substring{Pattern: "bcont"},
		&zoekt.SearchOptions{})
	if err != nil {
		t.Fatal("Search", err)
	}

	if len(results.Files) != 1 {
		t.Fatalf("got %v, want 1 file", results.Files)
	}

	file := results.Files[0]
	if got, want := file.SubRepositoryName, "gerrit.googlesource.com/bdir"; got != want {
		t.Errorf("got subrepo name %q, want %q", got, want)
	}
	if got, want := file.SubRepositoryPath, "bname"; got != want {
		t.Errorf("got subrepo path %q, want %q", got, want)
	}

	subVersion := file.Version
	if len(subVersion) != 40 {
		t.Fatalf("got %q, want hex sha1", subVersion)
	}

	if results, err := searcher.Search(context.Background(), &query.Substring{Pattern: "acont"}, &zoekt.SearchOptions{}); err != nil {
		t.Fatalf("Search('acont'): %v", err)
	} else if len(results.Files) != 1 {
		t.Errorf("got %v, want 1 result", results.Files)
	} else if f := results.Files[0]; f.Version == subVersion {
		t.Errorf("version in super repo matched version is subrepo.")
	}
}

func TestAllowMissingBranch(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)
	if err := createSubmoduleRepo(dir); err != nil {
		t.Fatalf("createSubmoduleRepo: %v", err)
	}

	indexDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(indexDir)

	buildOpts := build.Options{
		IndexDir: indexDir,
		RepoDir:  filepath.Join(dir, "gerrit.googlesource.com", "adir.git"),
	}
	buildOpts.SetDefaults()

	opts := Options{
		BuildOptions: buildOpts,
		BranchPrefix: "refs/heads/",
		Branches:     []string{"master", "nonexist"},
		Submodules:   true,
		Incremental:  true,
		RepoCacheDir: dir,
	}
	if err := IndexGitRepo(opts); err == nil {
		t.Fatalf("IndexGitRepo(nonexist) succeeded")
	}
	opts.AllowMissingBranch = true
	if err := IndexGitRepo(opts); err != nil {
		t.Fatalf("IndexGitRepo(nonexist, allow): %v", err)
	}
}

func createMultibranchRepo(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	script := `mkdir repo
cd repo
git init
mkdir subdir
echo acont > afile
echo sub-cont > subdir/sub-file
git add afile subdir/sub-file
git commit -am amsg

git branch branchdir/a

echo acont >> afile
git add afile subdir/sub-file
git commit -am amsg

git branch branchdir/b

git branch c
`
	cmd := exec.Command("/bin/sh", "-euxc", script)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("execution error: %v, output %s", err, out)
	}
	return nil
}

func TestBranchWildcard(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := createMultibranchRepo(dir); err != nil {
		t.Fatalf("createMultibranchRepo: %v", err)
	}

	indexDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(indexDir)

	buildOpts := build.Options{
		IndexDir: indexDir,
		RepoDir:  filepath.Join(dir + "/repo"),
	}
	buildOpts.SetDefaults()

	opts := Options{
		BuildOptions: buildOpts,
		BranchPrefix: "refs/heads",
		Branches:     []string{"branchdir/*"},
		Submodules:   true,
		Incremental:  true,
	}
	if err := IndexGitRepo(opts); err != nil {
		t.Fatalf("IndexGitRepo: %v", err)
	}

	searcher, err := zoekt.NewShardedSearcher(indexDir)
	if err != nil {
		t.Fatal("NewShardedSearcher", err)
	}
	defer searcher.Close()

	if rlist, err := searcher.List(context.Background(), &query.Repo{Pattern: ""}); err != nil {
		t.Fatalf("List(): %v", err)
	} else if len(rlist.Repos) != 1 {
		t.Errorf("got %v, want 1 result", rlist.Repos)
	} else if repo := rlist.Repos[0]; len(repo.Repository.Branches) != 2 {
		t.Errorf("got branches %v, want 2", repo.Repository.Branches)
	}
}
