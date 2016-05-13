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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	git "github.com/libgit2/git2go"
)

var _ = log.Println

func treeToFiles(tree *git.Tree) (map[string]git.Oid, error) {
	res := map[string]git.Oid{}
	err := tree.Walk(func(n string, e *git.TreeEntry) int {
		switch e.Filemode {
		case git.FilemodeBlob, git.FilemodeBlobExecutable:
		default:
			return 0
		}

		if e.Type != git.ObjectBlob {
			return 0
		}
		res[filepath.Join(n, e.Name)] = *e.Id
		return 0
	})

	return res, err
}

func guessRepoURL(repoDir string) (string, error) {
	base, err := git.NewConfig()
	if err != nil {
		return "", err
	}
	cfg, err := git.OpenOndisk(base, filepath.Join(repoDir, "config"))
	if err != nil {
		return "", err
	}

	url, err := cfg.LookupString("remote.origin.url")
	if err != nil {
		return "", err
	}

	if strings.Contains(url, "googlesource.com/") {
		/// eg. https://gerrit.googlesource.com/gitiles/+/master/.buckversion
		return url + "/+/{{.Branch}}/{{.Path}}", nil
	} else if strings.Contains(url, "github.com/") {
		// eg. https://github.com/hanwen/go-fuse/blob/notify/genversion.sh
		return url + "/blob/{{.Branch}}/{{.Path}}", nil
	}

	return "", fmt.Errorf("scheme unknown for URL %s", url)
}

func main() {
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")

	branchesStr := flag.String("branches", "master", "git branches to index. If set, arguments should be bare git repositories.")
	branchPrefix := flag.String("prefix", "refs/heads/", "prefix for branch names")

	indexDir := flag.String("index", build.DefaultDir, "index directory for *.zoekt files.")
	flag.Parse()

	opts := build.Options{
		Parallelism: *parallelism,
		SizeMax:     *sizeMax,
		ShardMax:    *shardLimit,
		IndexDir:    *indexDir,
	}
	opts.SetDefaults()

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

func getTreeID(repo *git.Repository, ref string) (*git.Oid, error) {
	obj, err := repo.RevparseSingle(ref)
	if err != nil {
		return nil, err
	}
	defer obj.Free()

	var treeId *git.Oid
	switch obj.Type() {
	case git.ObjectCommit:
		commit, err := repo.LookupCommit(obj.Id())
		if err != nil {
			return nil, err
		}
		treeId = commit.TreeId()
	case git.ObjectTree:
		treeId = obj.Id()
	default:
		return nil, fmt.Errorf("unsupported object type %d", obj.Type())
	}
	return treeId, nil
}

func indexGitRepo(opts build.Options, repoDir, branchPrefix string, branches []string) error {
	repoDir = filepath.Clean(repoDir)
	opts.RepoName = filepath.Base(repoDir)
	url, err := guessRepoURL(repoDir)
	if err != nil {
		log.Println("no repo URL: %s", err)
	} else {
		opts.RepoURL = url
	}

	if opts.RepoName == ".git" {
		opts.RepoName = filepath.Base(filepath.Dir(repoDir))
	}
	opts.RepoName = strings.TrimSuffix(opts.RepoName, ".git")

	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

	repo, err := git.OpenRepository(repoDir)
	if err != nil {
		return err
	}

	// name => branch
	allfiles := map[string][]string{}

	var names []string

	// branch => name => sha1
	data := map[string]map[string]git.Oid{}

	for _, b := range branches {
		treeID, err := getTreeID(repo, filepath.Join(branchPrefix, b))
		if err != nil {
			return err
		}

		tree, err := repo.LookupTree(treeID)
		if err != nil {
			return err
		}

		fs, err := treeToFiles(tree)
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
		shas := map[git.Oid][]string{}
		for _, b := range allfiles[n] {
			shas[data[b][n]] = append(shas[data[b][n]], b)
		}

		for sha, branches := range shas {
			blob, err := repo.LookupBlob(&sha)
			if err != nil {
				return err
			}

			const maxSz = 128 << 10
			if blob.Size() > maxSz {
				continue
			}

			builder.Add(zoekt.Document{
				Name:     n,
				Content:  blob.Contents(),
				Branches: branches,
			})
		}
	}
	builder.Finish()

	return nil
}
