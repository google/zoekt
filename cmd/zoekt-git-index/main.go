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
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/zoekt/build"
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


func main() {
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")

	branchesStr := flag.String("branches", "master", "git branches to index. If set, arguments should be bare git repositories.")
	branchPrefix := flag.String("branch_prefix", "refs/heads/", "git refs to index")

	indexTemplate := flag.String("index",
		"{{.Home}}/.csindex/{{.Base}}.{{.FP}}.{{.Shard}}",
		"template for index file to use.")

	flag.Parse()

	opts := build.Options{
		Parallelism:      *parallelism,
		SizeMax:          *sizeMax,
		ShardMax:         *shardLimit,
		FileNameTemplate: *indexTemplate,
	}

	var branches []string
	if *branchesStr != "" {
		branches = strings.Split(*branchesStr, ",")
	}

	for _, arg := range flag.Args() {
		if _, err := os.Lstat(filepath.Join(arg, ".git")); err == nil {
			arg = filepath.Join(arg, ".git")
		}
		arg, err := filepath.Abs(arg)
		if err != nil {
			log.Fatal(err)
		}
		if err := indexGitRepo(opts, arg, *branchPrefix, branches); err != nil {
			log.Fatal("indexGitRepo", err)
		}
	}
}


func indexGitRepo(opts build.Options, repoDir, branchPrefix string, branches []string) error {
	repoDir = filepath.Clean(repoDir)
	opts.RepoName = filepath.Base(repoDir)
	if filepath.Base(repoDir) == ".git" {
		opts.RepoName = filepath.Base(filepath.Dir(repoDir))
	}

	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

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
		ref, err := repo.LookupReference(filepath.Join(branchPrefix, b))
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

			builder.AddFileBranches(n, blob.Contents(), branches)
		}
	}
	builder.Finish()

	return nil
}
