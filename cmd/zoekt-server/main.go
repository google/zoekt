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
// * (re)starting a webserver,
// * recycling logs
// * periodically fetching new data.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gitindex "github.com/google/zoekt/git"
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

func refresh(repoDir, indexDir string, fetchInterval time.Duration) {
	// Start with indexing something, so we can start the webserver.
	runIndexCommand(indexDir, repoDir)

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
		for dir := range repos {
			cmd := exec.Command("git", "--git-dir", dir, "fetch", "origin")
			// Prevent prompting
			cmd.Stdin = &bytes.Buffer{}
			loggedRun(cmd)
		}

		runIndexCommand(indexDir, repoDir)
		<-t.C
	}
}

func runIndexCommand(indexDir, repoDir string) {
	cmd := exec.Command("zoekt-git-index",
		"-parallelism", "1",
		"-index", indexDir, "-incremental", "-recursive", repoDir)
	loggedRun(cmd)
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

// runServer runs the webserver in a loop. The webserver runs as a subprocess so
// any fatal bugs will be recorded in a log file.
func runServer(logDir, indexDir string, refresh time.Duration, args []string) {
	for {
		start := time.Now()

		nm := fmt.Sprintf("%s/zoekt-webserver.%s.stderr", logDir, time.Now().Format(time.RFC3339Nano))
		nm = strings.Replace(nm, ":", "_", -1)

		f, err := os.Create(nm)
		if err != nil {
			log.Fatal("cannot create log file", err)
		}

		log.Printf("restarting server, log in %s", nm)
		cmd := exec.Command("zoekt-webserver", "-index", indexDir, "-log_dir", logDir, "-log_refresh", refresh.String())
		cmd.Args = append(cmd.Args, args...)
		cmd.Stderr = f

		if err := cmd.Start(); err != nil {
			log.Fatal("could not start %s: %v", cmd, err)
		}

		f.Close()
		if err := cmd.Wait(); err != nil {
			log.Printf("webserver died, see %s", nm)

			if time.Now().Sub(start) < 2*time.Second {
				log.Println("crash loop?")
				time.Sleep(5 * time.Second)
			}
		}

	}
}

func main() {
	maxLogAge := flag.Duration("max_log_age", 3*day, "recycle logs after this much time")
	fetchInterval := flag.Duration("fetch_interval", time.Hour, "run fetches this often")
	dataDir := flag.String("data_dir",
		filepath.Join(os.Getenv("HOME"), "zoekt-serving"), "directory holding all data.")
	mirrorConfig := flag.String("mirror_config",
		"", "JSON file holding mirror configuration.")
	mirrorInterval := flag.Duration("mirror_duration", 24*time.Hour, "clone new repos at this frequency.")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("must set --data_dir")
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
	go refresh(repoDir, indexDir, *fetchInterval)
	go deleteLogs(logDir, *maxLogAge)
	runServer(logDir, indexDir, *maxLogAge/10, flag.Args())
}
