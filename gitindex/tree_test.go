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

	files, err := TreeToFiles(repo, tree, aURL.String(), cache)
	if err != nil {
		t.Fatalf("TreeToFiles: %v", err)
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

	opts := build.Options{
		IndexDir: indexDir,
		RepoDir:  filepath.Join(dir, "gerrit.googlesource.com", "adir.git"),
	}
	opts.SetDefaults()

	if err := IndexGitRepo(opts, "refs/heads/", []string{"master"}, true, dir); err != nil {
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
		t.Fatal("NewShardedSearcher", err)
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
}
