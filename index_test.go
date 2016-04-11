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

package codesearch

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"reflect"
	"testing"
)

func TestBoundary(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x the"))
	b.AddFile("f1", []byte("reader"))
	searcher := searcherForTest(t, b)

	res, err := searcher.Search(&SubstringQuery{Pattern: "there"})
	if err != nil {
		t.Errorf("search: %v", err)
	}

	if len(res.Files) > 0 {
		t.Fatalf("got %v, want no matches", res.Files)
	}
}

var _ = log.Println

func TestBasic(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f2", []byte("to carry water in the no later bla"))
	// -------------------- 0123456789012345678901234567890123456789

	searcher := searcherForTest(t, b)
	res, err := searcher.Search(&SubstringQuery{Pattern: "water"})
	if err != nil {
		t.Errorf("search: %v", err)
	}
	fmatches := res.Files
	if len(fmatches) != 1 {
		t.Fatalf("got %v, want 1 matches", fmatches)
	}

	got := fmt.Sprintf("%s:%d", fmatches[0].Name, fmatches[0].Matches[0].Offset)
	want := "f2:9"
	if got != want {
		t.Errorf("1: got %s, want %s", got, want)
	}
}

type memSeeker struct {
	data []byte
	off  int64
}

func (s *memSeeker) Close() error { return nil }
func (s *memSeeker) Read(b []byte) (int, error) {
	var err error
	n := int64(len(b)) + s.off
	if n > int64(len(s.data)) {
		err = io.EOF
		n = int64(len(s.data))
	}

	m := copy(b, s.data[s.off:n])
	s.off = n
	return m, err
}

func (s *memSeeker) Seek(off int64, whence int) (int64, error) {
	var n int64
	switch whence {
	case 0:
		n = off
	case 1:
		n = s.off + off
	case 2:
		n = int64(len(s.data)) + off
	}

	if n > int64(len(s.data)) || n < 0 {
		return s.off, fmt.Errorf("out of range")
	}
	s.off = n
	return s.off, nil
}

func TestNewlines(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFile("filename", []byte("line1\nline2\nbla"))
	//----------------------------012345 678901 23456

	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes(), 0}

	r := reader{r: f}

	var toc indexTOC
	r.readTOC(&toc)
	data := r.readIndexData(&toc)
	nls := r.readNewlines(data, 0)

	if want := []uint32{5, 11}; !reflect.DeepEqual(nls, want) {
		t.Errorf("got newlines %v, want %v", nls, want)
	}

	f = &memSeeker{buf.Bytes(), 0}

	searcher, err := NewSearcher(f)
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}
	sres, err := searcher.Search(&SubstringQuery{Pattern: "ne2"})
	matches := sres.Files
	want := []FileMatch{{
		Rank: 0,
		Name: "filename",
		Matches: []Match{
			{
				Offset:      8,
				Line:        "line2",
				LineNum:     2,
				LineOff:     2,
				MatchLength: 3,
			},
		}}}

	if !reflect.DeepEqual(matches, want) {
		t.Errorf("got %v, want %v", matches, want)
	}
}

func TestCaseBits(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFile("filename", []byte("abCDE"))

	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes(), 0}

	r := reader{r: f}

	var toc indexTOC
	r.readTOC(&toc)
	if r.err != nil {
		t.Errorf("got read error %v", r.err)
	}
	data := r.readIndexData(&toc)
	got := r.readContents(data, 0)

	if want := []byte("abcde"); bytes.Compare(got, want) != 0 {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadWrite(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFile("filename", []byte("abcde"))

	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes(), 0}

	r := reader{r: f}

	var toc indexTOC
	r.readTOC(&toc)

	if r.err != nil {
		t.Errorf("got read error %v", r.err)
	}
	if toc.contents.data.sz != 5 {
		t.Errorf("got contents size %d, want 5", toc.contents.data.sz)
	}

	data := r.readIndexData(&toc)
	if want := []string{"filename"}; !reflect.DeepEqual(data.fileNames, want) {
		t.Errorf("got filenames %s, want %v", data.fileNames, want)
	}

	if len(data.ngrams) != 3 {
		t.Fatalf("got ngrams %v, want 3 ngrams", data.ngrams)
	}

	if want := []uint32{5}; !reflect.DeepEqual(data.fileEnds, want) {
		t.Fatalf("got fileEnds %v, want %v", data.fileEnds, want)
	}

	if _, ok := data.ngrams[stringToNGram("bcq")]; ok {
		t.Errorf("found ngram bcd in %v", data.ngrams)
	}

	got := fromDeltas(r.readSectionBlob(data.ngrams[stringToNGram("bcd")]))
	if want := []uint32{1}; !reflect.DeepEqual(got, want) {
		t.Errorf("got posting data %v, want %v", got, want)
	}
}

func TestDelta(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("abc abc"))
	// ---------------------0123456
	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes(), 0}

	r := reader{r: f}

	var toc indexTOC
	r.readTOC(&toc)
	data := r.readIndexData(&toc)

	got := fromDeltas(r.readSectionBlob(data.ngrams[stringToNGram("abc")]))
	if want := []uint32{0, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("got posting data %v, want %v", got, want)
	}
}

func searcherForTest(t *testing.T, b *IndexBuilder) Searcher {
	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes(), 0}

	searcher, err := NewSearcher(f)
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}
	return searcher
}

func TestFileBasedSearch(t *testing.T) {
	b := NewIndexBuilder()

	c1 := []byte("I love bananas without skin")
	// -----------0123456789012345678901234567890123456789
	b.AddFile("f1", c1)
	c2 := []byte("In Dutch, ananas means pineapple")
	// -----------0123456789012345678901234567890123456789
	b.AddFile("f2", c2)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(&SubstringQuery{Pattern: "ananas"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	matches := sres.Files
	want := []FileMatch{{
		Rank: 0,
		Name: "f1",
		Matches: []Match{{
			Offset:      8,
			Line:        string(c1),
			LineNum:     1,
			LineOff:     8,
			MatchLength: 6,
		}},
	}, {
		Rank: 1,
		Name: "f2",
		Matches: []Match{{
			Line:        string(c2),
			LineNum:     1,
			LineOff:     10,
			Offset:      10,
			MatchLength: 6,
		}},
	}}
	if !reflect.DeepEqual(matches, want) {
		t.Errorf("got matches %#v, want %#v", matches, want)
	}
}

func TestCaseFold(t *testing.T) {
	b := NewIndexBuilder()

	c1 := []byte("I love BaNaNAS.")
	// ---------- 012345678901234567890123456
	b.AddFile("f1", c1)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&SubstringQuery{
			Pattern:       "bananas",
			CaseSensitive: true,
		})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	matches := sres.Files
	if len(matches) != 0 {
		t.Errorf("foldcase: got %v, want 0 matches", matches)
	}

	sres, err = searcher.Search(
		&SubstringQuery{
			Pattern:       "BaNaNAS",
			CaseSensitive: true,
		})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	matches = sres.Files
	if len(matches) != 1 {
		t.Errorf("no foldcase: got %v, want 1 matches", matches)
	} else if matches[0].Matches[0].Offset != 7 {
		t.Errorf("foldcase: got %v, want offsets 7", matches)
	}
}

func TestAndSearch(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x banana y"))
	b.AddFile("f2", []byte("x apple y"))
	b.AddFile("f3", []byte("x banana apple y"))
	// ---------------------0123456789012345
	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&AndQuery{
			Children: []Query{
				&SubstringQuery{
					Pattern: "banana",
				},
				&SubstringQuery{
					Pattern: "apple",
				},
			},
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	matches := sres.Files
	if len(matches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}

	if matches[0].Matches[0].Offset != 2 || matches[0].Matches[1].Offset != 9 {
		t.Fatalf("got %v, want offsets 2,9", matches)
	}

	wantStats := Stats{
		FilesLoaded:  1,
		NgramMatches: 4,
		MatchCount:   2,
		FileCount:    1,
	}
	if !reflect.DeepEqual(sres.Stats, wantStats) {
		t.Errorf("got stats %#v, want %#v", sres.Stats, wantStats)
	}
}

func TestOnlyNegations(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x banana y"))
	searcher := searcherForTest(t, b)

	_, err := searcher.Search(&SubstringQuery{
		Pattern: "bla",
		Negate:  true})
	if err == nil {
		t.Fatalf("should return error on query with negations only.")
	}
}

func TestAndNegateSearch(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x banana y"))
	b.AddFile("f4", []byte("x banana apple y"))
	// ---------------------0123456789012345
	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&AndQuery{
			Children: []Query{
				&SubstringQuery{
					Pattern: "banana",
				},
				&SubstringQuery{
					Pattern: "apple",
					Negate:  true,
				},
			},
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	matches := sres.Files

	if len(matches) != 1 || len(matches[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}
	if matches[0].Name != "f1" {
		t.Fatalf("got match %#v, want FileName: f1", matches[0])
	}
	if matches[0].Matches[0].Offset != 2 {
		t.Fatalf("got %v, want offsets 2,9", matches)
	}
}
