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

package zoekt

import (
	"bytes"
	"log"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/google/zoekt/query"
)

type testLoader struct {
	searchers []Searcher
}

func (l *testLoader) Close() {}
func (l *testLoader) getShards() []Searcher {
	return l.searchers
}

func (l *testLoader) rlock()         {}
func (l *testLoader) runlock()       {}
func (l *testLoader) String() string { return "test" }

type crashSearcher struct{}

func (s *crashSearcher) Search(ctx context.Context, q query.Q, opts *SearchOptions) (*SearchResult, error) {
	panic("search")
}

func (s *crashSearcher) List(ctx context.Context, q query.Q) (*RepoList, error) {
	panic("list")
}

func (s *crashSearcher) Stats() (*RepoStats, error) {
	return &RepoStats{}, nil
}

func (s *crashSearcher) Close() {}

func (s *crashSearcher) String() string { return "crashSearcher" }

func TestCrashResilience(t *testing.T) {
	out := &bytes.Buffer{}
	log.SetOutput(out)
	defer log.SetOutput(os.Stderr)
	ss := &shardedSearcher{&testLoader{[]Searcher{&crashSearcher{}}}}

	q := &query.Substring{Pattern: "hoi"}
	opts := &SearchOptions{}
	if res, err := ss.Search(context.Background(), q, opts); err != nil {
		t.Fatalf("Search: %v", err)
	} else if res.Stats.Crashes != 1 {
		t.Errorf("got stats %#v, want crashes = 1", res.Stats)
	}

	if res, err := ss.List(context.Background(), q); err != nil {
		t.Fatalf("List: %v", err)
	} else if res.Crashes != 1 {
		t.Errorf("got result %#v, want crashes = 1", res)
	}
}
