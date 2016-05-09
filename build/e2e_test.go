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

package build

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
)

func TestBasic(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}

	opts := Options{
		IndexDir:    dir,
		ShardMax:    1024,
		RepoName:    "repo",
		RepoURL:     "url",
		Parallelism: 2,
		SizeMax:     1 << 20,
	}

	b, err := NewBuilder(opts)
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}

	for i := 0; i < 4; i++ {
		s := fmt.Sprintf("%d", i)
		b.AddFile("F"+s, []byte(strings.Repeat(s, 1000)))
	}

	b.Finish()

	fs, _ := filepath.Glob(dir + "/*")
	if len(fs) <= 1 {
		t.Fatalf("want multiple shards, got %v", fs)
	}

	ss, err := zoekt.NewShardedSearcher(dir)
	if err != nil {
		t.Fatalf("NewShardedSearcher(%s): %v", dir, err)
	}

	q, err := query.Parse("111")
	if err != nil {
		t.Fatalf("Parse(111): %v", err)
	}
	result, err := ss.Search(q)
	if err != nil {
		t.Fatalf("Parse(111): %v", err)
	}

	if len(result.Files) != 1 || result.Files[0].Name != "F1" {
		t.Errorf("got %v, want 1 file.", result.Files)
	}
	defer ss.Close()

}
