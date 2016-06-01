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

var _ = log.Println

func guessRepoURL(repoDir string) (string, error) {
	base, err := git.NewConfig()
	if err != nil {
		return "", err
	}
	cfg, err := git.OpenOndisk(base, filepath.Join(repoDir, "config"))
	if err != nil {
		return "", err
	}

	remoteURL, err := cfg.LookupString("remote.origin.url")
	if err != nil {
		return "", err
	}

	if strings.Contains(remoteURL, "googlesource.com/") {
		/// eg. https://gerrit.googlesource.com/gitiles/+/master/.buckversion
		return remoteURL + "/+/{{.Branch}}/{{.Path}}", nil
	} else if strings.Contains(remoteURL, "github.com/") {
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", nil
		}

		// CloneURL from the JSON API has .git
		parsed.Path = strings.TrimSuffix(parsed.Path, ".git")

		// eg. https://github.com/hanwen/go-fuse/blob/notify/genversion.sh
		return parsed.String() + "/blob/{{.Branch}}/{{.Path}}", nil
	}

	return "", fmt.Errorf("scheme unknown for URL %s", remoteURL)
}

func main() {
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")
	var recursive = flag.Bool("recursive", false, "recurse into directories to index all git repos")
	submodules := flag.Bool("submodules", true, "if set to false, do not recurse into submodules")
	branchesStr := flag.String("branches", "master", "git branches to index. If set, arguments should be bare git repositories.")
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
			repos, err := findGitRepos(arg)
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
		opts.RepoName = name
		opts.RepoDir = filepath.Clean(dir)

		if mod, err := repoModTime(opts.RepoDir); *incremental && err == nil && mod.Before(opts.Timestamp()) {
			continue
		}

		log.Printf("indexing %s (%s)", dir, name)
		if err := indexGitRepo(opts, *branchPrefix, branches, *submodules, *incremental); err != nil {
			log.Printf("indexGitRepo(%s): %v", dir, err)
			exitStatus = 1
		}
	}
	os.Exit(exitStatus)
}

// findGitRepos finds git repositories and returns repodir => name map.
func findGitRepos(arg string) (map[string]string, error) {
	arg, err := filepath.Abs(arg)
	if err != nil {
		return nil, err
	}
	var dirs []string
	gitDirs := map[string]string{}
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

	for _, dir := range dirs {
		name := strings.TrimSuffix(dir, ".git")
		name = strings.TrimSuffix(name, "/")
		name = strings.TrimPrefix(name, arg)
		name = strings.TrimPrefix(name, "/")
		gitDirs[dir] = name
	}

	return gitDirs, nil
}

func repoModTime(dir string) (time.Time, error) {
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

func indexGitRepo(opts build.Options, branchPrefix string, branches []string, submodules, incremental bool) error {
	repo, err := git.OpenRepository(opts.RepoDir)
	if err != nil {
		return err
	}

	url, err := guessRepoURL(opts.RepoDir)
	if err != nil {
		log.Printf("guessRepoURL(%s): %s", opts.RepoDir, err)
	} else {
		opts.RepoURL = url
	}

	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

	// name => branch
	allfiles := map[string][]string{}

	var names []string

	// branch => name => sha1
	data := map[string]map[string]git.Oid{}
	repos := map[git.Oid]*git.Repository{}
	for _, b := range branches {
		tree, err := getTree(repo, filepath.Join(branchPrefix, b))
		if err != nil {
			return err
		}

		fs, subRepos, err := treeToFiles(repo, tree, submodules)
		if err != nil {
			return err
		}
		for k, v := range subRepos {
			repos[k] = v
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
			r := repos[sha]
			if r == nil {
				return fmt.Errorf("no repo found for %s (%s)", n, branches)
			}
			blob, err := r.LookupBlob(&sha)
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
