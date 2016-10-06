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
	"strings"

	"github.com/google/zoekt/build"
	"github.com/google/zoekt/git"
)

var _ = log.Println

func main() {
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")
	var recursive = flag.Bool("recursive", false, "recurse into directories to index all git repos")
	submodules := flag.Bool("submodules", true, "if set to false, do not recurse into submodules")
	branchesStr := flag.String("branches", "HEAD", "git branches to index.")
	branchPrefix := flag.String("prefix", "refs/heads/", "prefix for branch names")

	indexDir := flag.String("index", build.DefaultDir, "index directory for *.zoekt files.")
	incremental := flag.Bool("incremental", true, "only index changed repositories")
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

	gitRepos := map[string]string{}
	if *recursive {
		for _, arg := range flag.Args() {
			repos, err := gitindex.FindGitRepos(arg)
			if err != nil {
				log.Fatal(err)
			}
			for k, v := range repos {
				gitRepos[k] = v
			}
		}
	} else {
		for _, repoDir := range flag.Args() {
			if _, err := os.Lstat(filepath.Join(repoDir, ".git")); err == nil {
				repoDir = filepath.Join(repoDir, ".git")
			}
			repoDir, err := filepath.Abs(repoDir)
			if err != nil {
				log.Fatal(err)
			}

			name := filepath.Base(repoDir)
			if name == ".git" {
				name = filepath.Base(filepath.Dir(repoDir))
			}
			name = strings.TrimSuffix(name, ".git")

			gitRepos[repoDir] = name
		}
	}

	exitStatus := 0
	for dir, name := range gitRepos {
		opts.RepositoryDescription.Name = name
		opts.RepoDir = filepath.Clean(dir)

		if mod, err := gitindex.RepoModTime(opts.RepoDir); *incremental && err == nil && mod.Before(opts.Timestamp()) {
			continue
		}

		log.Printf("indexing %s (%s)", dir, name)
		if err := gitindex.IndexGitRepo(opts, *branchPrefix, branches, *submodules); err != nil {
			log.Printf("indexGitRepo(%s): %v", dir, err)
			exitStatus = 1
		}
	}
	os.Exit(exitStatus)
}
