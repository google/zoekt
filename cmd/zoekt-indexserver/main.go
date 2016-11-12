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

// This program manages a zoekt deployment:
// * recycling logs
// * periodically fetching new data.

package main

import (
	"bytes"
	"flag"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/zoekt/gitindex"
)

const day = time.Hour * 24

func loggedRun(cmd *exec.Cmd) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = errOut

	if err := cmd.Run(); err != nil {
		log.Printf("command %s failed: %v\nOUT: %s\nERR: %s",
			cmd.Args, err, out.String(), errOut.String())
	} else {
		log.Printf("ran successfully %s", cmd.Args)
	}
}

func refresh(repoDir, indexDir, indexConfigFile string, fetchInterval time.Duration) {
	// Start with indexing something, so we can start the webserver.
	runIndexCommand(indexDir, repoDir, indexConfigFile)

	t := time.NewTicker(fetchInterval)
	for {
		repos, err := gitindex.FindGitRepos(repoDir)
		if err != nil {
			log.Println(err)
			continue
		}
		if len(repos) == 0 {
			log.Printf("no repos found under %s", repoDir)
		}
		for _, dir := range repos {
			cmd := exec.Command("git", "--git-dir", dir, "fetch", "origin")
			// Prevent prompting
			cmd.Stdin = &bytes.Buffer{}
			loggedRun(cmd)
		}

		runIndexCommand(indexDir, repoDir, indexConfigFile)
		<-t.C
	}
}

func repoIndexCommand(indexDir, repoDir string, configs []RepoHostConfig) {
	for _, cfg := range configs {
		cmd := exec.Command("zoekt-repo-index",
			"-parallelism=1",
			"-repo_cache", repoDir,
			"-index", indexDir,
			"-base_url", cfg.BaseURL,
			"-rev_prefix", cfg.RevPrefix,
			"-max_sub_projects=5",
			"-manifest_repo_url", cfg.ManifestRepoURL,
			"-manifest_rev_prefix", cfg.ManifestRevPrefix)

		cmd.Args = append(cmd.Args, cfg.BranchXMLs...)
		log.Println(cmd.Args)
		loggedRun(cmd)
	}
}

func repositoryOnRepoHost(repoBaseDir, dir string, repoHosts []RepoHostConfig) bool {
	for _, rh := range repoHosts {
		u, _ := url.Parse(rh.BaseURL)

		if strings.HasPrefix(dir, filepath.Join(repoBaseDir, u.Host)) {
			return true
		}
	}
	return false
}

func runIndexCommand(indexDir, repoDir, indexConfigFile string) {
	var indexConfig *IndexConfig
	if indexConfigFile != "" {
		var err error
		indexConfig, err = readIndexConfig(indexConfigFile)
		if err != nil {
			log.Printf("index config: %v", err)
		}

		repoIndexCommand(indexDir, repoDir, indexConfig.RepoHosts)
	}

	repos, err := gitindex.FindGitRepos(repoDir)
	if err != nil {
		log.Println("FindGitRepos", err)
		return
	}

	for _, dir := range repos {
		if indexConfig != nil {
			// Don't want to index the subrepos of a repo
			// host separately.
			if repositoryOnRepoHost(repoDir, dir, indexConfig.RepoHosts) {
				continue
			}

			// TODO(hanwen): we should have similar
			// functionality for avoiding to index a
			// submodule separately too.
		}

		cmd := exec.Command("zoekt-git-index",
			"-require_ctags",
			"-parallelism=1",
			"-repo_cache", repoDir,
			"-index", indexDir, "-incremental", dir)
		loggedRun(cmd)
	}
}

// deleteLogs deletes old logs.
func deleteLogs(logDir string, maxAge time.Duration) {
	tick := time.NewTicker(maxAge / 100)
	for {
		fs, err := filepath.Glob(filepath.Join(logDir, "*"))
		if err != nil {
			log.Fatal("filepath.Glob(%s): %v", logDir, err)
		}

		threshold := time.Now().Add(-maxAge)
		for _, fn := range fs {

			if fi, err := os.Lstat(fn); err == nil && fi.ModTime().Before(threshold) {

				os.Remove(fn)
			}
		}
		<-tick.C
	}
}

func main() {
	maxLogAge := flag.Duration("max_log_age", 3*day, "recycle logs after this much time")
	fetchInterval := flag.Duration("fetch_interval", time.Hour, "run fetches this often")
	dataDir := flag.String("data_dir",
		filepath.Join(os.Getenv("HOME"), "zoekt-serving"), "directory holding all data.")
	mirrorConfig := flag.String("mirror_config",
		"", "JSON file holding mirror configuration.")
	indexConfig := flag.String("index_config",
		"", "JSON file holding index configuration.")
	mirrorInterval := flag.Duration("mirror_duration", 24*time.Hour, "clone new repos at this frequency.")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("must set --data_dir")
	}

	// Automatically prepend our own path at the front, to minimize
	// required configuration.
	if l, err := os.Readlink("/proc/self/exe"); err == nil {
		os.Setenv("PATH", filepath.Dir(l)+":"+os.Getenv("PATH"))
	}

	logDir := filepath.Join(*dataDir, "logs")
	indexDir := filepath.Join(*dataDir, "index")
	repoDir := filepath.Join(*dataDir, "repos")
	for _, s := range []string{logDir, indexDir, repoDir} {
		if _, err := os.Stat(s); err == nil {
			continue
		}

		if err := os.MkdirAll(s, 0755); err != nil {
			log.Fatal("MkdirAll %s: %v", s, err)
		}
	}

	_, err := readConfig(*mirrorConfig)
	if err != nil {
		log.Fatalf("readConfig(%s): %v", *mirrorConfig, err)
	} else {
		go periodicMirror(repoDir, *mirrorConfig, *mirrorInterval)
	}
	go deleteLogs(logDir, *maxLogAge)
	refresh(repoDir, indexDir, *indexConfig, *fetchInterval)
}
