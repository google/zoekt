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

package main

import (
	"log"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/hanwen/zoekt"
	"github.com/speedata/gogit"
)

var _ = log.Println

func treeToFiles(tree *gogit.Tree) (map[string]gogit.Oid, error) {
	res := map[string]gogit.Oid{}
	err := tree.Walk(func(n string, e *gogit.TreeEntry) int {
		switch e.Filemode {
		case gogit.FileModeBlob, gogit.FileModeBlobExec:
		default:
			return 0
		}

		if e.Type != gogit.ObjectBlob {
			return 0
		}
		res[filepath.Join(n, e.Name)] = *e.Id
		return 0
	})

	return res, err
}

type indexEntry struct {
	name     string
	cont     []byte
	branches []string
}

func buildGitShards(repoDir string, tpl *template.Template, source <-chan *indexEntry) error {
	repoName := repoDir
	if filepath.Base(repoDir) == ".git" {
		repoName = filepath.Dir(repoDir)
	}

	b := zoekt.NewIndexBuilder()

	shardLimit := 80 << 20
	shardNum := 0
	var writeErr error
	for e := range source {
		b.AddFileBranches(e.name, e.cont, e.branches)

		if b.ContentSize() > uint32(shardLimit) {
			nm, err := shardName(tpl, repoName, shardNum)
			if err != nil {
				writeErr = err
				break
			}
			shardNum++

			if err := writeShard(nm, b); err != nil {
				writeErr = err
				break
			}
			b = zoekt.NewIndexBuilder()
		}
	}

	// drain.
	for _ = range source {
	}

	if b.ContentSize() > 0 && writeErr == nil {
		nm, err := shardName(tpl, repoName, shardNum)
		if err != nil {
			writeErr = err
		} else if err := writeShard(nm, b); err != nil {
			writeErr = err
		}
	}
	return writeErr
}

func indexGitRepo(tpl *template.Template, repoDir string, branches []string) error {
	comm := make(chan *indexEntry, 10)
	errs := make(chan error, 10)
	go func() {
		errs <- buildGitShards(repoDir, tpl, comm)
	}()

	if err := readGitRepo(repoDir, branches, comm); err != nil {
		return err
	}
	return <-errs
}

func readGitRepo(repoDir string, branches []string, sink chan<- *indexEntry) error {
	defer close(sink)

	repo, err := gogit.OpenRepository(repoDir)
	if err != nil {
		return err
	}

	// name => branch
	allfiles := map[string][]string{}

	var names []string

	// branch => name => sha1
	data := map[string]map[string]gogit.Oid{}

	for _, b := range branches {
		ref, err := repo.LookupReference(b)
		if err != nil {
			return err
		}

		commit, err := repo.LookupCommit(ref.Oid)
		if err != nil {
			return err
		}

		fs, err := treeToFiles(commit.Tree)
		if err != nil {
			return err
		}

		for f := range fs {
			allfiles[f] = append(allfiles[f], b)
		}
		data[b] = fs
	}

	for n := range allfiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		shas := map[gogit.Oid][]string{}
		for _, b := range allfiles[n] {
			shas[data[b][n]] = append(shas[data[b][n]], b)
		}

		for sha, branches := range shas {
			sz, err := repo.ObjectSize(&sha)
			if err != nil {
				return err
			}

			const maxSz = 128 << 10
			if sz > maxSz {
				continue
			}

			blob, err := repo.LookupBlob(&sha)
			if err != nil {
				return err
			}

			sink <- &indexEntry{n, blob.Contents(), branches}
		}
	}

	return nil
}
