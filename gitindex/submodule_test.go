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
	"reflect"
	"testing"
)

func TestParseGitModules(t *testing.T) {
	testData := `[submodule "plugins/abc"]
	path = plugins/abc
	url = ../plugins/abc
	branch = .`

	got, err := ParseGitModules([]byte(testData))
	if err != nil {
		t.Fatalf("ParseGitModules: %T", err)
	}

	want := map[string]*SubmoduleEntry{
		"plugins/abc": &SubmoduleEntry{
			Path:   "plugins/abc",
			URL:    "../plugins/abc",
			Branch: ".",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
