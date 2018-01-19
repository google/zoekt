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

package rest

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/zoekt"
)

type memSeeker struct {
	data []byte
}

func (s *memSeeker) Close() {}
func (s *memSeeker) Read(off, sz uint32) ([]byte, error) {
	return s.data[off : off+sz], nil
}

func (s *memSeeker) Size() (uint32, error) {
	return uint32(len(s.data)), nil
}
func (s *memSeeker) Name() string {
	return "memSeeker"
}

func TestUnicodeOffset(t *testing.T) {
	repo := zoekt.Repository{
		Name:     "name",
		Branches: []zoekt.RepositoryBranch{{Name: "master", Version: "master-version"}},
	}
	b, err := zoekt.NewIndexBuilder(&repo)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}
	if err := b.Add(zoekt.Document{
		Name:    "f2",
		Content: []byte("orange\u2318apple"),
		// --------------0123456     78901
		Branches: []string{"master"},
	}); err != nil {
		t.Errorf("Add: %v", err)
	}

	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes()}

	searcher, err := zoekt.NewSearcher(f)
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}

	rep, err := serveSearchAPIStructured(context.Background(), searcher, &SearchRequest{
		Query: "orange.*apple",
		Restrict: []SearchRequestRestriction{
			{
				Repo:     "name",
				Branches: []string{"master"},
			}},
	})

	if err != nil {
		t.Fatalf("serveSearchAPIStructured: %v", err)
	}

	if len(rep.Files) != 1 || len(rep.Files[0].Lines) != 1 || len(rep.Files[0].Lines[0].Matches) != 1 {
		t.Fatalf("got %#v, want 1 match", rep)
	}

	if end := rep.Files[0].Lines[0].Matches[0].End; end != 12 {
		t.Errorf("got end %d, want 12", end)
	}
}
