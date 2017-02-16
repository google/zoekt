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
	"log"
	"reflect"
	"regexp/syntax"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/context"

	"github.com/google/zoekt/query"
)

func clearScores(r *SearchResult) {
	for i := range r.Files {
		r.Files[i].Score = 0.0
		for j := range r.Files[i].LineMatches {
			r.Files[i].LineMatches[j].Score = 0.0
		}
	}
}

func testIndexBuilder(t *testing.T, repo *Repository, docs ...Document) *IndexBuilder {
	b, err := NewIndexBuilder(repo)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	for i, d := range docs {
		if err := b.Add(d); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	return b
}

func TestBoundary(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("x the")},
		Document{Name: "f1", Content: []byte("reader")})
	res := searchForTest(t, b, &query.Substring{Pattern: "there"})
	if len(res.Files) > 0 {
		t.Fatalf("got %v, want no matches", res.Files)
	}
}

var _ = log.Println

func TestBasic(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f2",
			Content: []byte("to carry water in the no later bla"),
			// ------------- 0123456789012345678901234567890123456789
		})

	res := searchForTest(t, b, &query.Substring{Pattern: "water"})
	fmatches := res.Files
	if len(fmatches) != 1 || len(fmatches[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 matches", fmatches)
	}

	got := fmt.Sprintf("%s:%d", fmatches[0].FileName, fmatches[0].LineMatches[0].LineFragments[0].Offset)
	want := "f2:9"
	if got != want {
		t.Errorf("1: got %s, want %s", got, want)
	}
}

func TestEmptyIndex(t *testing.T) {
	b := testIndexBuilder(t, nil)
	searcher := searcherForTest(t, b)

	var opts SearchOptions
	if _, err := searcher.Search(context.Background(), &query.Substring{}, &opts); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if _, err := searcher.List(context.Background(), &query.Repo{}); err != nil {
		t.Fatalf("List: %v", err)
	}

	if _, err := searcher.Search(context.Background(), &query.Substring{Pattern: "java", FileName: true}, &opts); err != nil {
		t.Fatalf("Search: %v", err)
	}
}

type memSeeker struct {
	data []byte
}

func (s *memSeeker) Name() string {
	return "memseeker"
}

func (s *memSeeker) Close() {}
func (s *memSeeker) Read(off, sz uint32) ([]byte, error) {
	return s.data[off : off+sz], nil
}

func (s *memSeeker) Size() (uint32, error) {
	return uint32(len(s.data)), nil
}

func TestNewlines(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "filename", Content: []byte("line1\nline2\nbla")})

	sres := searchForTest(t, b, &query.Substring{Pattern: "ne2"})

	matches := sres.Files
	want := []FileMatch{{
		FileName: "filename",
		LineMatches: []LineMatch{
			{
				LineFragments: []LineFragmentMatch{{
					Offset:      8,
					LineOffset:  2,
					MatchLength: 3,
				}},
				Line:       []byte("line2"),
				LineStart:  6,
				LineEnd:    11,
				LineNumber: 2,
			},
		}}}

	if !reflect.DeepEqual(matches, want) {
		t.Errorf("got %v, want %v", matches, want)
	}
}

func searchForTest(t *testing.T, b *IndexBuilder, q query.Q, o ...SearchOptions) *SearchResult {
	searcher := searcherForTest(t, b)
	var opts SearchOptions
	if len(o) > 0 {
		opts = o[0]
	}
	res, err := searcher.Search(context.Background(), q, &opts)
	if err != nil {
		t.Fatalf("Search(%s): %v", q, err)
	}
	clearScores(res)
	return res
}

func searcherForTest(t *testing.T, b *IndexBuilder) Searcher {
	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes()}

	searcher, err := NewSearcher(f)
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}

	return searcher
}

func TestFileBasedSearch(t *testing.T) {
	c1 := []byte("I love bananas without skin")
	// -----------0123456789012345678901234567890123456789
	c2 := []byte("In Dutch, ananas means pineapple")
	// -----------0123456789012345678901234567890123456789
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: c1},
		Document{Name: "f2", Content: c2},
	)
	sres := searchForTest(t, b, &query.Substring{Pattern: "ananas"})

	matches := sres.Files
	if len(matches) != 2 {
		t.Fatalf("got %v, want 2 matches", matches)
	}
	if matches[0].FileName != "f2" || matches[1].FileName != "f1" {
		t.Fatalf("got %v, want matches {f1,f2}", matches)
	}
	if matches[0].LineMatches[0].LineFragments[0].Offset != 10 || matches[1].LineMatches[0].LineFragments[0].Offset != 8 {
		t.Fatalf("got %#v, want offsets 10,8", matches)
	}
}

func TestCaseFold(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("I love BaNaNAS.")},
		// ---------- 012345678901234567890123456
	)
	sres := searchForTest(t, b, &query.Substring{
		Pattern:       "bananas",
		CaseSensitive: true,
	})
	matches := sres.Files
	if len(matches) != 0 {
		t.Errorf("foldcase: got %#v, want 0 matches", matches)
	}

	sres = searchForTest(t, b,
		&query.Substring{
			Pattern:       "BaNaNAS",
			CaseSensitive: true,
		})
	matches = sres.Files
	if len(matches) != 1 {
		t.Errorf("no foldcase: got %v, want 1 matches", matches)
	} else if matches[0].LineMatches[0].LineFragments[0].Offset != 7 {
		t.Errorf("foldcase: got %v, want offsets 7", matches)
	}
}

func TestAndSearch(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("f1", []byte("x banana y"))
	b.AddFile("f2", []byte("x apple y"))
	b.AddFile("f3", []byte("x banana apple y"))
	// ---------------------0123456789012345
	sres := searchForTest(t, b, query.NewAnd(
		&query.Substring{
			Pattern: "banana",
		},
		&query.Substring{
			Pattern: "apple",
		},
	))
	matches := sres.Files
	if len(matches) != 1 || len(matches[0].LineMatches) != 1 || len(matches[0].LineMatches[0].LineFragments) != 2 {
		t.Fatalf("got %#v, want 1 match with 2 fragments", matches)
	}

	if matches[0].LineMatches[0].LineFragments[0].Offset != 2 || matches[0].LineMatches[0].LineFragments[1].Offset != 9 {
		t.Fatalf("got %#v, want offsets 2,9", matches)
	}

	wantStats := Stats{
		FilesLoaded:     1,
		BytesLoaded:     16,
		NgramMatches:    4,
		MatchCount:      1,
		FileCount:       1,
		FilesConsidered: 3,
	}
	if !reflect.DeepEqual(sres.Stats, wantStats) {
		t.Errorf("got stats %#v, want %#v", sres.Stats, wantStats)
	}
}

func TestAndNegateSearch(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("f1", []byte("x banana y"))
	b.AddFile("f4", []byte("x banana apple y"))
	// ---------------------0123456789012345
	sres := searchForTest(t, b, query.NewAnd(
		&query.Substring{
			Pattern: "banana",
		},
		&query.Not{&query.Substring{
			Pattern: "apple",
		}}))

	matches := sres.Files

	if len(matches) != 1 || len(matches[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}
	if matches[0].FileName != "f1" {
		t.Fatalf("got match %#v, want FileName: f1", matches[0])
	}
	if matches[0].LineMatches[0].LineFragments[0].Offset != 2 {
		t.Fatalf("got %v, want offsets 2,9", matches)
	}
}

func TestNegativeMatchesOnlyShortcut(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("f1", []byte("x banana y"))

	b.AddFile("f2", []byte("x appelmoes y"))
	b.AddFile("f3", []byte("x appelmoes y"))
	b.AddFile("f3", []byte("x appelmoes y"))

	sres := searchForTest(t, b, query.NewAnd(
		&query.Substring{
			Pattern: "banana",
		},
		&query.Not{&query.Substring{
			Pattern: "appel",
		}}))

	if sres.Stats.FilesConsidered != 1 {
		t.Errorf("got %#v, want FilesConsidered: 1", sres.Stats)
	}
}

func TestFileSearch(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banzana", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("x apple y"))
	sres := searchForTest(t, b, &query.Substring{
		Pattern:  "anan",
		FileName: true,
	})

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}

	got := matches[0].LineMatches[0]
	want := LineMatch{
		Line: []byte("banana"),
		LineFragments: []LineFragmentMatch{{
			Offset:      1,
			LineOffset:  1,
			MatchLength: 4,
		}},
		FileName: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestFileCase(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("BANANA", []byte("x orange y"))
	sres := searchForTest(t, b, &query.Substring{
		Pattern:  "banana",
		FileName: true,
	})

	matches := sres.Files
	if len(matches) != 1 || matches[0].FileName != "BANANA" {
		t.Fatalf("got %v, want 1 match 'BANANA'", matches)
	}
}

func TestFileRegexpSearchBruteForce(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banzana", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("x apple y"))
	sres := searchForTest(t, b, &query.Regexp{
		Regexp:   mustParseRE("[qn][zx]"),
		FileName: true,
	})

	matches := sres.Files
	if len(matches) != 1 || matches[0].FileName != "banzana" {
		t.Fatalf("got %v, want 1 match on 'banzana'", matches)
	}
}

func TestFileRegexpSearchShortString(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banana.py", []byte("x orange y"))
	sres := searchForTest(t, b, &query.Regexp{
		Regexp:   mustParseRE("ana.py"),
		FileName: true,
	})

	matches := sres.Files
	if len(matches) != 1 || matches[0].FileName != "banana.py" {
		t.Fatalf("got %v, want 1 match on 'banana.py'", matches)
	}
}

func TestFileSubstringSearchBruteForce(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("BANZANA", []byte("x orange y"))
	b.AddFile("banana", []byte("x apple y"))

	q := &query.Substring{
		Pattern:  "z",
		FileName: true,
	}

	res := searchForTest(t, b, q)
	if len(res.Files) != 1 || res.Files[0].FileName != "BANZANA" {
		t.Fatalf("got %v, want 1 match on 'BANZANA''", res.Files)
	}
}

func TestFileSubstringSearchBruteForceEnd(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("BANZANA", []byte("x orange y"))
	b.AddFile("bananaq", []byte("x apple y"))

	q := &query.Substring{
		Pattern:  "q",
		FileName: true,
	}

	res := searchForTest(t, b, q)
	if want := "bananaq"; len(res.Files) != 1 || res.Files[0].FileName != want {
		t.Fatalf("got %v, want 1 match in %q", res.Files, want)
	}
}

func TestSearchMatchAll(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banzana", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("x apple y"))
	sres := searchForTest(t, b, &query.Const{true})

	matches := sres.Files
	if len(matches) != 2 {
		t.Fatalf("got %v, want 2 matches", matches)
	}
}

func TestSearchNewline(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banzana", []byte("abcd\ndefg"))
	sres := searchForTest(t, b, &query.Substring{Pattern: "d\nd"})

	// Just check that we don't crash.

	matches := sres.Files
	if len(matches) != 1 {
		t.Fatalf("got %v, want 1 matches", matches)
	}
}

func TestSearchMatchAllRegexp(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banzana", []byte("abcd"))
	// --------------------------0123456879
	b.AddFile("banana", []byte("pqrs"))
	sres := searchForTest(t, b, &query.Regexp{Regexp: mustParseRE(".")})

	matches := sres.Files
	if len(matches) != 2 || sres.Stats.MatchCount != 2 {
		t.Fatalf("got %v, want 2 matches", matches)
	}
	if len(matches[0].LineMatches[0].Line) != 4 || len(matches[1].LineMatches[0].Line) != 4 {
		t.Fatalf("want 4 chars in every file, got %#v", matches)
	}
}

func TestFileRestriction(t *testing.T) {
	b, err := NewIndexBuilder(nil)
	if err != nil {
		t.Fatalf("NewIndexBuilder: %v", err)
	}

	b.AddFile("banana1", []byte("x orange y"))
	// --------------------------0123456879
	b.AddFile("banana2", []byte("x apple y"))
	b.AddFile("orange", []byte("x apple y"))
	sres := searchForTest(t, b, query.NewAnd(
		&query.Substring{
			Pattern:  "banana",
			FileName: true,
		},
		&query.Substring{
			Pattern: "apple",
		}))

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}

	match := matches[0].LineMatches[0]
	got := string(match.Line)
	want := "x apple y"
	if got != want {
		t.Errorf("got match %#v, want line %q", match, want)
	}
}

func TestFileNameBoundary(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "banana2", Content: []byte("x apple y")},
		Document{Name: "helpers.go", Content: []byte("x apple y")},
		Document{Name: "foo", Content: []byte("x apple y")})
	sres := searchForTest(t, b, &query.Substring{
		Pattern:  "helpers.go",
		FileName: true,
	})

	matches := sres.Files
	if len(matches) != 1 || len(matches[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match", matches)
	}
}

func TestWordBoundaryRanking(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("xbytex xbytex")},
		Document{Name: "f2", Content: []byte("xbytex\nbytex\nbyte bla")},
		// -----------------------------------0123456 789012 34567890
		Document{Name: "f3", Content: []byte("xbytex ybytex")})

	sres := searchForTest(t, b, &query.Substring{
		Pattern: "byte",
	})

	if len(sres.Files) != 3 {
		t.Fatalf("got %#v, want 3 files", sres.Files)
	}

	file0 := sres.Files[0]
	if file0.FileName != "f2" || len(file0.LineMatches) != 3 {
		t.Fatalf("got file %s, num matches %d (%#v), want 3 matches in file f2", file0.FileName, len(file0.LineMatches), file0)
	}

	if file0.LineMatches[0].LineFragments[0].Offset != 13 {
		t.Fatalf("got first match %#v, want full word match", sres.Files[0].LineMatches[0])
	}
	if file0.LineMatches[1].LineFragments[0].Offset != 7 {
		t.Fatalf("got second match %#v, want partial word match", sres.Files[0].LineMatches[1])
	}
}

func TestBranchMask(t *testing.T) {
	b := testIndexBuilder(t, &Repository{
		Branches: []RepositoryBranch{
			{"master", "v-master"},
			{"stable", "v-stable"},
			{"bonzai", "v-bonzai"},
		},
	}, Document{Name: "f1", Content: []byte("needle"), Branches: []string{"master"}},
		Document{Name: "f2", Content: []byte("needle"), Branches: []string{"stable", "master"}},
		Document{Name: "f3", Content: []byte("needle"), Branches: []string{"stable", "master"}},
		Document{Name: "f4", Content: []byte("needle"), Branches: []string{"bonzai"}},
	)

	sres := searchForTest(t, b, query.NewAnd(
		&query.Substring{
			Pattern: "needle",
		},
		&query.Branch{
			Pattern: "table",
		}))

	if len(sres.Files) != 2 || sres.Files[0].FileName != "f3" || sres.Files[1].FileName != "f2" {
		t.Fatalf("got %v, want 1 result from f2", sres.Files)
	}

	if len(sres.Files[0].Branches) != 1 || sres.Files[0].Branches[0] != "stable" {
		t.Fatalf("got %v, want 1 branch 'stable'", sres.Files[0].Branches)
	}
}

func TestBranchReport(t *testing.T) {
	branches := []string{"stable", "master"}
	b := testIndexBuilder(t, &Repository{
		Branches: []RepositoryBranch{
			{"stable", "vs"},
			{"master", "vm"},
		},
	},
		Document{Name: "f2", Content: []byte("needle"), Branches: branches})
	sres := searchForTest(t, b, &query.Substring{
		Pattern: "needle",
	})
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, want 1 result from f2", sres.Files)
	}

	f := sres.Files[0]
	if !reflect.DeepEqual(f.Branches, branches) {
		t.Fatalf("got branches %q, want %q", f.Branches, branches)
	}
}

func TestBranchVersions(t *testing.T) {
	b := testIndexBuilder(t, &Repository{
		Branches: []RepositoryBranch{
			{"stable", "v-stable"},
			{"master", "v-master"},
		},
	}, Document{Name: "f2", Content: []byte("needle"), Branches: []string{"master"}})

	sres := searchForTest(t, b, &query.Substring{
		Pattern: "needle",
	})
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, want 1 result from f2", sres.Files)
	}

	f := sres.Files[0]
	if f.Version != "v-master" {
		t.Fatalf("got file %#v, want version 'v-master'", f)
	}
}

func TestCoversContent(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("needle the bla")})

	sres := searchForTest(t, b,
		query.NewAnd(
			&query.Substring{
				Pattern: "needle",
			},
			&query.Not{&query.Substring{
				Pattern: "the",
			}}))

	if len(sres.Files) > 0 {
		t.Fatalf("got %v, want no results", sres.Files)
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
	content := []byte("needle the bla")
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content})
	// ------------------------------01234567890123

	sres := searchForTest(t, b,
		&query.Regexp{
			Regexp: mustParseRE("dle.*bla"),
		})

	if len(sres.Files) != 1 || len(sres.Files[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match in 1 file", sres.Files)
	}

	got := sres.Files[0].LineMatches[0]
	want := LineMatch{
		LineFragments: []LineFragmentMatch{{
			LineOffset:  3,
			Offset:      3,
			MatchLength: 11,
		}},
		Line:       content,
		FileName:   false,
		LineNumber: 1,
		LineStart:  0,
		LineEnd:    14,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestRegexpFile(t *testing.T) {
	content := []byte("needle the bla")
	// ----------------01234567890123

	name := "let's play: find the mussel"
	b := testIndexBuilder(t, nil,
		Document{Name: name, Content: content},
		Document{Name: "play.txt", Content: content})

	sres := searchForTest(t, b,
		&query.Regexp{
			Regexp:   mustParseRE("play.*mussel"),
			FileName: true,
		})

	if len(sres.Files) != 1 || len(sres.Files[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match in 1 file", sres.Files)
	}

	if sres.Files[0].FileName != name {
		t.Errorf("got match %#v, want name %q", sres.Files[0])
	}
}

func TestRegexpOrder(t *testing.T) {
	content := []byte("bla the needle")
	// ----------------01234567890123
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})

	sres := searchForTest(t, b,
		&query.Regexp{
			Regexp: mustParseRE("dle.*bla"),
		})

	if len(sres.Files) != 0 {
		t.Fatalf("got %v, want 0 matches", sres.Files)
	}
}

func TestRepoName(t *testing.T) {
	content := []byte("bla the needle")
	// ----------------01234567890123
	b := testIndexBuilder(t, &Repository{Name: "bla"},
		Document{Name: "f1", Content: content})

	sres := searchForTest(t, b,
		query.NewAnd(
			&query.Substring{Pattern: "needle"},
			&query.Repo{Pattern: "foo"},
		))

	if len(sres.Files) != 0 {
		t.Fatalf("got %v, want 0 matches", sres.Files)
	}

	if sres.Stats.FilesConsidered > 0 {
		t.Fatalf("got FilesConsidered %d, should have short circuited", sres.Stats.FilesConsidered)
	}

	sres = searchForTest(t, b,
		query.NewAnd(
			&query.Substring{Pattern: "needle"},
			&query.Repo{Pattern: "bla"},
		))
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, want 1 match", sres.Files)
	}
}

func TestMergeMatches(t *testing.T) {
	content := []byte("blablabla")
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})

	sres := searchForTest(t, b,
		&query.Substring{Pattern: "bla"})
	if len(sres.Files) != 1 || len(sres.Files[0].LineMatches) != 1 {
		t.Fatalf("got %v, want 1 match", sres.Files)
	}
}

func TestRepoURL(t *testing.T) {
	content := []byte("blablabla")
	b := testIndexBuilder(t, &Repository{
		Name:                 "name",
		URL:                  "URL",
		CommitURLTemplate:    "commit",
		FileURLTemplate:      "file-url",
		LineFragmentTemplate: "fragment",
	}, Document{Name: "f1", Content: content})

	sres := searchForTest(t, b, &query.Substring{Pattern: "bla"})

	if sres.RepoURLs["name"] != "file-url" {
		t.Errorf("got RepoURLs %v, want {name: URL}", sres.RepoURLs)
	}
	if sres.LineFragments["name"] != "fragment" {
		t.Errorf("got URLs %v, want {name: URL}", sres.LineFragments)
	}
}

func TestRegexpCaseSensitive(t *testing.T) {
	content := []byte("bla\nfunc unmarshalGitiles\n")
	b := testIndexBuilder(t, nil, Document{
		Name:    "f1",
		Content: content})

	res := searchForTest(t, b,
		&query.Regexp{
			Regexp:        mustParseRE("func.*Gitiles"),
			CaseSensitive: true,
		})

	if len(res.Files) != 1 {
		t.Fatalf("got %v, want one match", res.Files)
	}
}

func TestRegexpCaseFolding(t *testing.T) {
	content := []byte("bla\nfunc unmarshalGitiles\n")

	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})
	res := searchForTest(t, b,
		&query.Regexp{
			Regexp:        mustParseRE("func.*GITILES"),
			CaseSensitive: false,
		})

	if len(res.Files) != 1 {
		t.Fatalf("got %v, want one match", res.Files)
	}
}

func TestCaseRegexp(t *testing.T) {
	content := []byte("BLABLABLA")
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})
	res := searchForTest(t, b,
		&query.Regexp{
			Regexp:        mustParseRE("[xb][xl][xa]"),
			CaseSensitive: true,
		})

	if len(res.Files) > 0 {
		t.Fatalf("got %v, want no matches", res.Files)
	}
}

func TestNegativeRegexp(t *testing.T) {
	content := []byte("BLABLABLA needle bla")
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})
	res := searchForTest(t, b,
		query.NewAnd(
			&query.Substring{
				Pattern: "needle",
			},
			&query.Not{
				&query.Regexp{
					Regexp: mustParseRE(".cs"),
				},
			}))

	if len(res.Files) != 1 {
		t.Fatalf("got %v, want 1 match", res.Files)
	}
}

func TestSymbolRank(t *testing.T) {

	content := []byte("func bla() blub")
	// ----------------012345678901234
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
		}, Document{
			Name:    "f2",
			Content: content,
			Symbols: []DocumentSection{{5, 8}},
		}, Document{
			Name:    "f3",
			Content: content,
		})

	res := searchForTest(t, b,
		&query.Substring{
			Pattern: "bla",
		})

	if len(res.Files) != 3 {
		t.Fatalf("got %#v, want 3 files", res.Files)
	}
	if res.Files[0].FileName != "f2" {
		t.Errorf("got %#v, want 'f2' as top match", res.Files[0])
	}
}

func TestPartialSymbolRank(t *testing.T) {
	content := []byte("func bla() blub")
	// ----------------012345678901234

	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
			Symbols: []DocumentSection{{4, 9}},
		}, Document{
			Name:    "f2",
			Content: content,
			Symbols: []DocumentSection{{4, 8}},
		}, Document{
			Name:    "f3",
			Content: content,
			Symbols: []DocumentSection{{4, 9}},
		})

	res := searchForTest(t, b,
		&query.Substring{
			Pattern: "bla",
		})

	if len(res.Files) != 3 {
		t.Fatalf("got %#v, want 3 files", res.Files)
	}
	if res.Files[0].FileName != "f2" {
		t.Errorf("got %#v, want 'f2' as top match", res.Files[0])
	}
}

func TestNegativeRepo(t *testing.T) {
	content := []byte("bla the needle")
	// ----------------01234567890123
	b := testIndexBuilder(t, &Repository{
		Name: "bla",
	}, Document{Name: "f1", Content: content})

	sres := searchForTest(t, b,
		query.NewAnd(
			&query.Substring{Pattern: "needle"},
			&query.Not{&query.Repo{Pattern: "bla"}},
		))

	if len(sres.Files) != 0 {
		t.Fatalf("got %v, want 0 matches", sres.Files)
	}
}

func TestListRepos(t *testing.T) {
	content := []byte("bla the needle")
	// ----------------01234567890123
	b := testIndexBuilder(t, &Repository{
		Name: "reponame",
	},
		Document{Name: "f1", Content: content},
		Document{Name: "f2", Content: content})

	searcher := searcherForTest(t, b)
	q := &query.Repo{Pattern: "epo"}
	res, err := searcher.List(context.Background(), q)
	if err != nil {
		t.Fatalf("List(%v): %v", q, err)
	}
	if len(res.Repos) != 1 || res.Repos[0].Repository.Name != "reponame" {
		t.Fatalf("got %v, want 1 matches", res)
	}
	q = &query.Repo{Pattern: "bla"}
	res, err = searcher.List(context.Background(), q)
	if err != nil {
		t.Fatalf("List(%v): %v", q, err)
	}
	if len(res.Repos) != 0 {
		t.Fatalf("got %v, want 0 matches", res)
	}
}

func TestMetadata(t *testing.T) {
	content := []byte("bla the needle")
	// ----------------01234567890123
	b := testIndexBuilder(t, &Repository{
		Name: "reponame",
	}, Document{Name: "f1", Content: content},
		Document{Name: "f2", Content: content})

	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes()}

	rd, _, err := ReadMetadata(f)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if got, want := rd.Name, "reponame"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOr(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("needle")},
		Document{Name: "f2", Content: []byte("banana")})
	sres := searchForTest(t, b, query.NewOr(
		&query.Substring{Pattern: "needle"},
		&query.Substring{Pattern: "banana"}))

	if len(sres.Files) != 2 {
		t.Fatalf("got %v, want 2 files", sres.Files)
	}
}

func TestAtomCountScore(t *testing.T) {
	b := testIndexBuilder(t,
		&Repository{
			Branches: []RepositoryBranch{
				{"branches", "v1"},
				{"needle", "v2"},
			},
		},
		Document{Name: "f1", Content: []byte("needle the bla"), Branches: []string{"branches"}},
		Document{Name: "needle-file-branch", Content: []byte("needle content"), Branches: []string{"needle"}},
		Document{Name: "needle-file", Content: []byte("needle content"), Branches: []string{"branches"}})

	sres := searchForTest(t, b,
		query.NewOr(
			&query.Substring{Pattern: "needle"},
			&query.Substring{Pattern: "needle", FileName: true},
			&query.Branch{Pattern: "needle"},
		))
	var got []string
	for _, f := range sres.Files {
		got = append(got, f.FileName)
	}
	want := []string{"needle-file-branch", "needle-file", "f1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImportantCutoff(t *testing.T) {
	content := []byte("func bla() blub")
	// ----------------012345678901234
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
			Symbols: []DocumentSection{{5, 8}},
		}, Document{
			Name:    "f2",
			Content: content,
		})
	opts := SearchOptions{
		ShardMaxImportantMatch: 1,
	}

	sres := searchForTest(t, b, &query.Substring{Pattern: "bla"}, opts)
	if len(sres.Files) != 1 || sres.Files[0].FileName != "f1" {
		t.Errorf("got %v, wanted 1 match 'f1'", sres.Files)
	}
}

func TestFrequency(t *testing.T) {
	content := []byte("sla _Py_HashDouble(double v sla las las shd dot dot")
	// ----------------012345678901234
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
		})

	sres := searchForTest(t, b, &query.Substring{Pattern: "slashdot"})
	if len(sres.Files) != 0 {
		t.Errorf("got %v, wanted 0 matches", sres.Files)
	}
}

func TestMatchNewline(t *testing.T) {
	re, err := syntax.Parse("[^a]a", syntax.ClassNL)
	if err != nil {
		panic(err)
	}

	content := []byte("pqr\nalex")
	// ----------------0123 4567
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
		})

	sres := searchForTest(t, b, &query.Regexp{Regexp: re, CaseSensitive: true})
	if len(sres.Files) != 1 {
		t.Errorf("got %v, wanted 1 matches", sres.Files)
	} else if l := sres.Files[0].LineMatches[0].Line; bytes.Compare(l, content) != 0 {
		t.Errorf("got match line %q, want %q", l, content)
	}
}

func TestSubRepo(t *testing.T) {
	subRepos := map[string]*Repository{
		"sub": &Repository{
			Name:                 "sub-name",
			LineFragmentTemplate: "sub-line",
		}}

	content := []byte("pqr\nalex")
	// ----------------0123 4567

	b := testIndexBuilder(t, &Repository{
		SubRepoMap: subRepos,
	}, Document{
		Name:              "sub/f1",
		Content:           content,
		SubRepositoryPath: "sub",
	})

	sres := searchForTest(t, b, &query.Substring{Pattern: "alex"})
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, wanted 1 matches", sres.Files)
	}

	f := sres.Files[0]
	if f.SubRepositoryPath != "sub" || f.SubRepositoryName != "sub-name" {
		t.Errorf("got %#v, want SubRepository{Path,Name} = {'sub', 'sub-name'}", f)
	}

	if sres.LineFragments["sub-name"] != "sub-line" {
		t.Errorf("got LineFragmentTemplate %v, want {'sub':'sub-line'}", sres.LineFragments)
	}
}

func TestSearchEither(t *testing.T) {
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte("bla needle bla")},
		Document{Name: "needle-file-branch", Content: []byte("bla content")})

	sres := searchForTest(t, b, &query.Substring{Pattern: "needle"})
	if len(sres.Files) != 2 {
		t.Fatalf("got %v, wanted 2 matches", sres.Files)
	}

	sres = searchForTest(t, b, &query.Substring{Pattern: "needle", Content: true})
	if len(sres.Files) != 1 {
		t.Fatalf("got %v, wanted 1 match", sres.Files)
	}

	if got, want := sres.Files[0].FileName, "f1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnicodeExactMatch(t *testing.T) {
	needle := "néédlÉ"
	content := []byte("blá blá " + needle + " blâ")
	// ----------------01234567    8
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})

	if res := searchForTest(t, b, &query.Substring{Pattern: needle, CaseSensitive: true}); len(res.Files) != 1 {
		t.Fatalf("case sensitive: got %v, wanted 1 match", res.Files)
	}
}

func TestUnicodeCoverContent(t *testing.T) {
	needle := "néédlÉ"
	content := []byte("blá blá " + needle + " blâ")
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})

	if res := searchForTest(t, b, &query.Substring{Pattern: "NÉÉDLÉ", CaseSensitive: true}); len(res.Files) != 0 {
		t.Fatalf("case sensitive: got %v, wanted 0 match", res.Files)
	}

	res := searchForTest(t, b, &query.Substring{Pattern: "NÉÉDLÉ"})
	if len(res.Files) != 1 {
		t.Fatalf("case insensitive: got %v, wanted 1 match", res.Files)
	}

	if got, want := res.Files[0].LineMatches[0].LineFragments[0].Offset, uint32(strings.Index(string(content), needle)); got != want {
		t.Errorf("got %d want %d", got, want)
	}
}

func TestUnicodeNonCoverContent(t *testing.T) {
	needle := "nééáádlÉ"
	//---------01234567
	content := []byte("blá blá " + needle + " blâ")
	// ----------------01234567    8901234   5678
	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: content})

	res := searchForTest(t, b, &query.Substring{Pattern: "NÉÉÁÁDLÉ", Content: true})
	if len(res.Files) != 1 {
		t.Fatalf("got %v, wanted 1 match", res.Files)
	}

	if got, want := res.Files[0].LineMatches[0].LineFragments[0].Offset, uint32(strings.Index(string(content), needle)); got != want {
		t.Errorf("got %d want %d", got, want)
	}
}

func TestUnicodeVariableLength(t *testing.T) {
	var lower, upper rune
	var buf [4]byte
	for l := rune(0); l < (1 << 21); l++ {
		u := unicode.SimpleFold(l)

		lSz := utf8.EncodeRune(buf[:], l)
		uSz := utf8.EncodeRune(buf[:], u)

		if lSz != uSz {
			lower = l
			upper = u
			t.Logf("char %c (%d sz %d) %c (%d sz %d)", l, l, lSz, u, u, uSz)
			break
		}
	}

	if lower == 0 {
		t.Fatal("rune not found")
	}
	needle := "nee" + string([]rune{lower}) + "eed"
	corpus := []byte("nee" + string([]rune{upper}) + "eed" +
		" ee" + string([]rune{lower}) + "ee" +
		" ee" + string([]rune{upper}) + "ee")

	b := testIndexBuilder(t, nil,
		Document{Name: "f1", Content: []byte(corpus)})

	res := searchForTest(t, b, &query.Substring{Pattern: needle, Content: true})
	if len(res.Files) != 1 {
		t.Fatalf("got %v, wanted 1 match", res.Files)
	}
}

func TestShortUnicode(t *testing.T) {
	world := "世界"
	content := []byte("world = " + world)
	// ----------------012345678901234
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: content,
		})
	q := &query.Substring{Pattern: world}

	searcher := searcherForTest(t, b)
	var opts SearchOptions
	_, err := searcher.Search(context.Background(), q, &opts)
	if err == nil {
		t.Error("search should have failed")
	}
}

func TestUnicodeFileStartOffsets(t *testing.T) {
	unicode := "世界"
	wat := "waaaaaat"
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: []byte(unicode),
		},
		Document{
			Name:    "f2",
			Content: []byte(wat),
		},
	)
	q := &query.Substring{Pattern: wat, Content: true}
	res := searchForTest(t, b, q)
	if len(res.Files) != 1 {
		t.Fatalf("got %v, wanted 1 match", res.Files)
	}
}

func TestLongFileUTF8(t *testing.T) {
	needle := "neeedle"

	// 6 bytes.
	unicode := "世界"
	content := []byte(strings.Repeat(unicode, 100) + needle)
	b := testIndexBuilder(t, nil,
		Document{
			Name:    "f1",
			Content: []byte(strings.Repeat("a", 50)),
		},
		Document{
			Name:    "f2",
			Content: content,
		})

	q := &query.Substring{Pattern: needle, Content: true}
	res := searchForTest(t, b, q)
	if len(res.Files) != 1 {
		t.Errorf("got %v, want 1 result", res)
	}
}
