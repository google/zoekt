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

package web

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
	"golang.org/x/net/context"
)

// TODO(hanwen): cut & paste from ../ . Should create internal test
// util package.
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

func searcherForTest(t *testing.T, b *zoekt.IndexBuilder) zoekt.Searcher {
	var buf bytes.Buffer
	b.Write(&buf)
	f := &memSeeker{buf.Bytes()}

	searcher, err := zoekt.NewSearcher(f)
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}

	return searcher
}

func TestJSON(t *testing.T) {
	b := zoekt.NewIndexBuilder()
	b.SetName("name")
	b.SetURLTemplates("repo-url", "{{.Version}}", "url", "line")
	b.AddBranch("master", "1234")

	line := "abc apple orange"
	b.Add(zoekt.Document{
		Name:     "f2",
		Content:  []byte(line),
		Branches: []string{"master"},
	})

	s := searcherForTest(t, b)
	srv := Server{
		Searcher: s,
		Top:      Top,
		RESTAPI:  true,
	}

	mux, err := NewMux(&srv)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	ts := httptest.NewServer(mux)
	defer ts.Close()

	req := `{"Query": "apple", "Restrict":[{"Repo": "name", "Branches": ["master"]}]}`
	res, err := http.Post(ts.URL+"/api/search", jsonContentType, bytes.NewBufferString(req))

	if err != nil {
		t.Fatal(err)
	}

	if got := res.Header.Get("Content-Type"); got != jsonContentType {
		t.Errorf("got Content-Type %q, want %q", got, jsonContentType)
	}

	resultBytes, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	result := string(resultBytes)
	for _, want := range []string{`"LineNumber":1`, `"Line":"` + line + `"`} {
		if !strings.Contains(result, want) {
			t.Errorf("got %s, missing %s", result, want)
		}
	}
}

func TestBasic(t *testing.T) {
	b := zoekt.NewIndexBuilder()
	b.SetName("name")
	b.SetURLTemplates("repo-url", "{{.Version}}", "url", "line")
	b.AddBranch("master", "1234")
	b.Add(zoekt.Document{
		Name:    "f2",
		Content: []byte("to carry water in the no later bla"),
		// ------------- 0123456789012345678901234567890123456789
		Branches: []string{"master"},
	})

	s := searcherForTest(t, b)
	srv := Server{
		Searcher: s,
		Top:      Top,
		HTML:     true,
	}

	mux, err := NewMux(&srv)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	ts := httptest.NewServer(mux)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/search?q=water")
	if err != nil {
		t.Fatal(err)
	}
	resultBytes, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	result := string(resultBytes)
	for req, needles := range map[string][]string{
		"/search?q=water": []string{
			"href=\"url#line",
			"carry <b>water</b>",
		},
		"/search?q=r:": []string{
			"1234\">master",
			"repo-url\">name",
		},
	} {
		res, err := http.Get(ts.URL + req)
		if err != nil {
			t.Fatal(err)
		}
		resultBytes, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			log.Fatal(err)
		}

		result := string(resultBytes)
		for _, want := range needles {
			if !strings.Contains(result, want) {
				t.Errorf("result did not have %q: %s", want, result)
			}
		}
	}
	if notWant := "crashed"; strings.Contains(result, notWant) {
		t.Errorf("result has %q: %s", notWant, result)
	}
}

type crashSearcher struct {
	zoekt.Searcher
}

func (s *crashSearcher) Search(ctx context.Context, q query.Q, opts *zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	res := zoekt.SearchResult{}
	res.Stats.Crashes = 1
	return &res, nil
}

func TestCrash(t *testing.T) {
	srv := Server{
		Searcher: &crashSearcher{},
		Top:      Top,
		HTML:     true,
	}

	mux, err := NewMux(&srv)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	ts := httptest.NewServer(mux)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/search?q=water")
	if err != nil {
		t.Fatal(err)
	}
	resultBytes, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	result := string(resultBytes)
	if want := "1 shards crashed"; !strings.Contains(result, want) {
		t.Errorf("result did not have %q: %s", want, result)
	}
}

func TestUnicodeOffset(t *testing.T) {
	b := zoekt.NewIndexBuilder()
	b.SetName("name")
	b.Add(zoekt.Document{
		Name:    "f2",
		Content: []byte("orange\u2318apple"),
		// --------------0123456     78901
		Branches: []string{"master"},
	})

	s := searcherForTest(t, b)

	rep, err := serveSearchAPIStructured(s, &SearchRequest{
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
		t.Errorf("got end %d, want 11", end)
	}
}
