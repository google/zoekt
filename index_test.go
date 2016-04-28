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
	"fmt"
	"io"
	"log"
	"reflect"
	"regexp/syntax"
	"testing"

	"github.com/google/zoekt/query"
)

func clearScores(r *SearchResult) {
	for i := range r.Files {
		r.Files[i].Score = 0.0
		for j := range r.Files[i].Matches {
			r.Files[i].Matches[j].Score = 0.0
		}
	}
}

func TestBoundary(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x the"))
	b.AddFile("f1", []byte("reader"))
	searcher := searcherForTest(t, b)

	res, err := searcher.Search(&query.Substring{Pattern: "there"})
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
	res, err := searcher.Search(&query.Substring{Pattern: "water"})
	if err != nil {
		t.Errorf("search: %v", err)
	}
	fmatches := res.Files
	if len(fmatches) != 1 || len(fmatches[0].Matches) != 1 {
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

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(&query.Substring{Pattern: "ne2"})
	if err != nil {
		t.Fatal(err)
	}
	clearScores(sres)

	matches := sres.Files
	want := []FileMatch{{
		Name: "filename",
		Matches: []Match{
			{
				Offset:      8,
				Line:        []byte("line2"),
				LineStart:   6,
				LineEnd:     11,
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
	sres, err := searcher.Search(&query.Substring{Pattern: "ananas"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)

	matches := sres.Files
	if len(matches) != 2 || matches[0].Name != "f2" || matches[1].Name != "f1" {
		t.Fatalf("got %v, want matches {f1,f2}", matches)
	}
	if matches[0].Matches[0].Offset != 10 || matches[1].Matches[0].Offset != 8 {
		t.Fatalf("got %#v, want offsets 10,8", matches)
	}
}

func TestCaseFold(t *testing.T) {
	b := NewIndexBuilder()

	c1 := []byte("I love BaNaNAS.")
	// ---------- 012345678901234567890123456
	b.AddFile("f1", c1)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.Substring{
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
		&query.Substring{
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
		&query.And{
			Children: []query.Query{
				&query.Substring{
					Pattern: "banana",
				},
				&query.Substring{
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
		FilesLoaded:     1,
		BytesLoaded:     16,
		NgramMatches:    4,
		MatchCount:      2,
		FileCount:       1,
		FilesConsidered: 3,
	}
	if !reflect.DeepEqual(sres.Stats, wantStats) {
		t.Errorf("got stats %#v, want %#v", sres.Stats, wantStats)
	}
}

func TestAndNegateSearch(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x banana y"))
	b.AddFile("f4", []byte("x banana apple y"))
	// ---------------------0123456789012345
	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.And{
			Children: []query.Query{
				&query.Substring{
					Pattern: "banana",
				},
				&query.Not{&query.Substring{
					Pattern: "apple",
				}},
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

func TestNegativeMatchesOnlyShortcut(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("f1", []byte("x banana y"))

	b.AddFile("f2", []byte("x appelmoes y"))
	b.AddFile("f3", []byte("x appelmoes y"))
	b.AddFile("f3", []byte("x appelmoes y"))

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.And{
			Children: []query.Query{
				&query.Substring{
					Pattern: "banana",
				},
				&query.Not{&query.Substring{
					Pattern: "appel",
				}},
			},
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if sres.Stats.FilesConsidered != 1 {
		t.Errorf("got %#v, want FilesConsidered: 1", sres.Stats)
	}
}

func TestFileSearch(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("banzana", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("x apple y"))
	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.Substring{
			Pattern:  "anan",
			FileName: true,
		})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}

	got := matches[0].Matches[0]
	want := Match{
		Line:        []byte("banana"),
		Offset:      1,
		LineOff:     1,
		MatchLength: 4,
		FileName:    true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestFileSearchBruteForce(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("banzana", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("x apple y"))
	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.Regexp{
			Regexp:  mustParseRE("[qn][zx]"),
			FileName: true,
		})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)

	matches := sres.Files
	if len(matches) != 1 || matches[0].Name != "banzana" {
		t.Fatalf("got %v, want 1 match on 'banzana'", matches)
	}
}

func TestFileRestriction(t *testing.T) {
	b := NewIndexBuilder()

	b.AddFile("banana1", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana2", []byte("x apple y"))
	b.AddFile("orange", []byte("x apple y"))
	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.And{[]query.Query{
			&query.Substring{
				Pattern:  "banana",
				FileName: true,
			},
			&query.Substring{
				Pattern: "apple",
			},
		}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}

	match := matches[0].Matches[0]
	got := string(match.Line)
	want := "x apple y"
	if got != want {
		t.Errorf("got match %#v, want line %q", match, want)
	}
}

func TestFileNameBoundary(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFile("banana2", []byte("x apple y"))
	b.AddFile("helpers.go", []byte("x apple y"))
	b.AddFile("foo", []byte("x apple y"))
	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.Substring{
			Pattern:  "helpers.go",
			FileName: true,
		})
	clearScores(sres)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}
}

func TestWordBoundaryRanking(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFile("f1", []byte("xbytex xbytex"))
	b.AddFile("f2", []byte("xbytex bytex byte bla"))
	// ---------------------012345678901234567890
	b.AddFile("f3", []byte("xbytex ybytex"))

	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.Substring{
			Pattern: "byte",
		})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(sres.Files) != 3 || sres.Files[0].Name != "f2" || len(sres.Files[0].Matches) != 3 {
		t.Fatalf("got %#v, want 3 matches in files f2", sres.Files)
	}
	if sres.Files[0].Matches[0].Offset != 13 {
		t.Fatalf("got first match %#v, want full word match", sres.Files[0].Matches[0])
	}
	if sres.Files[0].Matches[1].Offset != 7 {
		t.Fatalf("got second match %#v, want partial word match", sres.Files[0].Matches[0])
	}
}

func TestBranchMask(t *testing.T) {
	b := NewIndexBuilder()
	b.AddFileBranches("f1", []byte("needle"), []string{"master"})
	b.AddFileBranches("f2", []byte("needle"), []string{"stable", "master"})

	searcher := searcherForTest(t, b)

	sres, err := searcher.Search(
		&query.And{[]query.Query{
			&query.Substring{
				Pattern: "needle",
			},
			&query.Branch{
				Name: "stable",
			},
		}})

	if err != nil {
		t.Fatalf("Search", err)
	}

	if len(sres.Files) != 1 || sres.Files[0].Name != "f2" {
		t.Fatalf("got %v, want 1 result from f2", sres.Files)
	}

	if len(sres.Files[0].Branches) != 1 || sres.Files[0].Branches[0] != "stable" {
		t.Fatalf("got %v, want 1 branch 'stable'", sres.Files[0].Branches)
	}
}

func TestBranchReport(t *testing.T) {
	b := NewIndexBuilder()

	branches := []string{"stable", "master"}
	b.AddFileBranches("f2", []byte("needle"), branches)
	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.Substring{
			Pattern: "needle",
		})
	if err != nil {
		t.Fatalf("Search", err)
	}

	if len(sres.Files) != 1 {
		t.Fatalf("got %v, want 1 result from f2", sres.Files)
	}

	f := sres.Files[0]
	if !reflect.DeepEqual(f.Branches, branches) {
		t.Fatalf("got branches %q, want %q", f.Branches, branches)
	}
}

func TestCoversContent(t *testing.T) {
	b := NewIndexBuilder()

	branches := []string{"stable", "master"}
	b.AddFileBranches("f1", []byte("needle the bla"), branches)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.And{
			Children: []query.Query{
				&query.Substring{
					Pattern: "needle",
				},
				&query.Not{&query.Substring{
					Pattern: "the",
				}},
			},
		})

	if err != nil || len(sres.Files) > 0 {
		t.Fatalf("got %v, %v, want success without results", sres.Files, err)
	}

	if sres.Stats.FilesLoaded > 0 {
		t.Errorf("got %#v, want no FilesLoaded", sres.Stats)
	}
}

func mustParseRE(s string) *syntax.Regexp {
	r, err := syntax.Parse(s, 0)
	if err != nil {
		panic(err)
	}

	return r
}

func TestRegexp(t *testing.T) {
	b := NewIndexBuilder()

	content := []byte("needle the bla")
	// ----------------01234567890123
	b.AddFile("f1", content)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.Regexp{
			Regexp: mustParseRE("dle.*bla"),
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)
	if len(sres.Files) != 1 || len(sres.Files[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match in 1 file", sres.Files)
	}

	got := sres.Files[0].Matches[0]
	want := Match{
		LineOff:     3,
		Offset:      3,
		MatchLength: 11,
		Line:        content,
		FileName:    false,
		LineNum:     1,
		LineStart:   0,
		LineEnd:     14,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRegexpFile(t *testing.T) {
	b := NewIndexBuilder()

	content := []byte("needle the bla")
	// ----------------01234567890123
	name := "let's play: find the mussel"
	b.AddFile(name, content)
	b.AddFile("play.txt", content)
	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.Regexp{
			Regexp:   mustParseRE("play.*mussel"),
			FileName: true,
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)
	if len(sres.Files) != 1 || len(sres.Files[0].Matches) != 1 {
		t.Fatalf("got %v, want 1 match in 1 file", sres.Files)
	}

	if sres.Files[0].Name != name {
		t.Errorf("got match %#v, want name %q", sres.Files[0])
	}
}

func TestRegexpOrder(t *testing.T) {
	b := NewIndexBuilder()

	content := []byte("bla the needle")
	// ----------------01234567890123
	b.AddFile("f1", content)

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.Regexp{
			Regexp: mustParseRE("dle.*bla"),
		})

	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	clearScores(sres)
	if len(sres.Files) != 0 {
		t.Fatalf("got %v, want 0 matches", sres.Files)
	}
}

func TestRepoName(t *testing.T) {
	b := NewIndexBuilder()

	content := []byte("bla the needle")
	// ----------------01234567890123
	b.AddFile("f1", content)
	b.SetName("bla")

	searcher := searcherForTest(t, b)
	sres, err := searcher.Search(
		&query.And{[]query.Query{
			&query.Substring{Pattern: "needle"},
			&query.Repo{Name: "foo"},
		}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(sres.Files) != 0 {
		t.Fatalf("got %v, want 0 matches", sres.Files)
	}

	if sres.Stats.FilesConsidered > 0 {
		t.Fatalf("got FilesConsidered %d, should have short circuited", sres.Stats.FilesConsidered)
	}

	sres, err = searcher.Search(
		&query.And{[]query.Query{
			&query.Substring{Pattern: "needle"},
			&query.Repo{Name: "bla"},
		}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, want 1 match", sres.Files)
	}

}
