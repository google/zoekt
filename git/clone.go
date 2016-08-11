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
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func CloneRepos(destDir string, repos map[string]string) error {
	for name, cloneURL := range repos {
		parent := filepath.Join(destDir, filepath.Dir(name))
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}

		repoDest := filepath.Join(parent, filepath.Base(name)+".git")
		if _, err := os.Lstat(repoDest); err == nil {
			continue
		}

		cmd := exec.Command("git", "clone", "--bare", "--verbose", "--progress", "--recursive", cloneURL, repoDest)
		log.Println("running:", cmd.Args)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
