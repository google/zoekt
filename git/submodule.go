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
	"io/ioutil"
	"os"
	"regexp"

	"github.com/libgit2/git2go"
)

type SubmoduleEntry struct {
	Path   string
	URL    string
	Branch string
}

const submodREStr = "^submodule.([^.]*)\\.(.*)"

var submodRE = regexp.MustCompile(submodREStr)

// ParseGitModules parses the contents of a .gitmodules file.
func ParseGitModules(content []byte) (map[string]*SubmoduleEntry, error) {
	base, err := git.NewConfig()
	if err != nil {
		return nil, err
	}
	defer base.Free()

	// git2go has no API for parsing from contents. Sigh.
	f, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(content); err != nil {
		return nil, err
	}
	f.Close()

	cfg, err := git.OpenOndisk(base, f.Name())
	if err != nil {
		return nil, err
	}
	defer cfg.Free()
	iter, err := cfg.NewIteratorGlob(submodREStr)
	if err != nil {
		return nil, err
	}

	result := map[string]*SubmoduleEntry{}
	for {
		entry, err := iter.Next()
		if err != nil {
			if ge, ok := err.(*git.GitError); ok && ge.Code == git.ErrIterOver {
				break
			}
			return nil, err
		}

		m := submodRE.FindStringSubmatch(entry.Name)

		name := m[1]
		if result[name] == nil {
			result[name] = &SubmoduleEntry{}
		}

		e := result[name]
		switch m[2] {
		case "branch":
			e.Branch = entry.Value
		case "path":
			e.Path = entry.Value
		case "url":
			e.URL = entry.Value
		}
	}

	return result, nil
}
