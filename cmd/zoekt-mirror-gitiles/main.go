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

// This binary fetches all repos of a Gitiles host.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	gitindex "github.com/google/zoekt/git"
)

func main() {
	dest := flag.String("dest", "", "destination directory")
	namePattern := flag.String("name", "", "only clone repos whose name matches the regexp.")
	flag.Parse()

	if len(flag.Args()) < 1 {
		log.Fatal("must provide Gitiles URL as argument.")
	}
	gitilesURL, err := url.Parse(flag.Arg(0))
	if err != nil {
		log.Fatal("url.Parse(): %v", err)
	}

	if *dest == "" {
		log.Fatal("must set --dest")
	}

	destDir := filepath.Join(*dest, gitilesURL.Host)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		log.Fatal(err)
	}

	repos, err := getRepos(gitilesURL)
	if *namePattern != "" {
		re, err := regexp.Compile(*namePattern)
		if err != nil {
			log.Fatal(err)
		}

		trimmed := map[string]string{}
		for k, v := range repos {
			if re.FindString(k) != "" {
				trimmed[k] = v
			}
		}
		repos = trimmed
	}

	if err := gitindex.CloneRepos(destDir, repos); err != nil {
		log.Fatal(err)
	}
}

type Project struct {
	Name     string
	CloneURL string `json:"clone_url"`
}

func getRepos(URL *url.URL) (map[string]string, error) {
	URL.RawQuery = "format=JSON"
	resp, err := http.Get(URL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	const xssTag = ")]}'\n"
	content = bytes.TrimPrefix(content, []byte(xssTag))

	m := map[string]*Project{}
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, err
	}

	result := map[string]string{}
	for k, v := range m {
		result[k] = v.CloneURL
	}
	return result, nil
}
