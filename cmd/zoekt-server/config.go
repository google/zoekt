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
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type configEntry struct {
	GithubUser string
	GitilesURL string
	Name       string
	Exclude    string
}

func readConfig(filename string) ([]configEntry, error) {
	var result []configEntry

	if filename == "" {
		return result, nil
	}

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func watchFile(path string) (<-chan struct{}, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(filepath.Dir(path)); err != nil {
		return nil, err
	}

	out := make(chan struct{}, 1)
	go func() {
		var last time.Time
		for {
			select {
			case <-watcher.Events:
				fi, err := os.Stat(path)
				if err == nil && fi.ModTime() != last {
					out <- struct{}{}
					last = fi.ModTime()
				}
			case err := <-watcher.Errors:
				if err != nil {
					log.Printf("watcher error: %v", err)
				}
			}
		}
	}()
	return out, nil
}

func periodicMirror(repoDir string, cfgFile string, interval time.Duration) {
	t := time.NewTicker(interval)
	watcher, err := watchFile(cfgFile)
	if err != nil {
		log.Printf("watchFile(%q): %v", cfgFile, err)
	}

	var lastCfg []configEntry
	for {

		select {
		case <-watcher:
			log.Printf("mirror config %s changed", cfgFile)
		case <-t.C:
		}

		cfg, err := readConfig(cfgFile)
		if err != nil {
			log.Printf("readConfig(%s): %v", cfgFile, err)
			continue
		} else {
			lastCfg = cfg
		}
		for _, c := range lastCfg {
			if c.GithubUser != "" {
				cmd := exec.Command("zoekt-mirror-github", "-user", c.GithubUser, "-name", c.Name,
					"-exclude", c.Exclude, "-dest", repoDir)
				loggedRun(cmd)
			} else if c.GitilesURL != "" {
				cmd := exec.Command("zoekt-mirror-gitiles",
					"-exclude", c.Exclude, "-dest", repoDir, "-name", c.Name, c.GitilesURL)
				loggedRun(cmd)
			}
		}
	}
}
