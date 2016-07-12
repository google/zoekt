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
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"

	"golang.org/x/net/context"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
)

var Funcmap = template.FuncMap{
	"HumanUnit": func(orig int64) string {
		b := orig
		suffix := ""
		if orig > 10*(1<<30) {
			suffix = "G"
			b = orig / (1 << 30)
		} else if orig > 10*(1<<20) {
			suffix = "M"
			b = orig / (1 << 20)
		} else if orig > 10*(1<<10) {
			suffix = "K"
			b = orig / (1 << 10)
		}

		return fmt.Sprintf("%d%s", b, suffix)
	}}

type Server struct {
	Searcher zoekt.Searcher

	DidYouMean *template.Template
	RepoList   *template.Template
	Result     *template.Template
	Print      *template.Template
	SearchBox  *template.Template
}

func NewMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", s.serveSearch)
	mux.HandleFunc("/", s.serveSearchBox)
	if s.Print != nil {
		mux.HandleFunc("/print", s.servePrint)
	}
	return mux
}

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	err := s.serveSearchErr(w, r)

	if suggest, ok := err.(*query.SuggestQueryError); ok {
		var buf bytes.Buffer
		if err := s.DidYouMean.Execute(&buf, suggest); err != nil {
			http.Error(w, err.Error(), http.StatusTeapot)
		}

		w.Write(buf.Bytes())
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func (s *Server) serveSearchErr(w http.ResponseWriter, r *http.Request) error {
	qvals := r.URL.Query()
	queryStr := qvals.Get("q")
	if queryStr == "" {
		return fmt.Errorf("no query found")
	}

	log.Printf("got query %q", queryStr)
	q, err := query.Parse(queryStr)
	if err != nil {
		return err
	}

	repoOnly := true
	query.VisitAtoms(q, func(q query.Q) {
		_, ok := q.(*query.Repo)
		repoOnly = repoOnly && ok
	})
	if repoOnly {
		return s.serveListReposErr(q, queryStr, w, r)
	}

	numStr := qvals.Get("num")

	num, err := strconv.Atoi(numStr)
	if err != nil {
		num = 50
	}

	sOpts := zoekt.SearchOptions{
		ShardMaxImportantMatch: num / 10,
	}

	repoFound := false
	query.VisitAtoms(q, func(q query.Q) {
		if _, ok := q.(*query.Repo); ok {
			repoFound = true
		}
	})

	if !repoFound {
		// If the search is not restricted to any repo, we
		// assume the user doesn't really know what they are
		// looking for, so we restrict the number of matches
		// to avoid overwhelming the search engine.
		sOpts.ShardMaxMatchCount = num * 10
	}
	sOpts.SetDefaults()

	ctx := context.Background()
	result, err := s.Searcher.Search(ctx, q, &sOpts)
	if err != nil {
		return err
	}

	if len(result.Files) > num {
		result.Files = result.Files[:num]
	}

	fileMatches, err := formatResults(result, s.Print != nil)
	if err != nil {
		return err
	}

	res := ResultInput{
		LastQuery:     queryStr,
		Stats:         result.Stats,
		Query:         q.String(),
		QueryStr:      queryStr,
		SearchOptions: sOpts.String(),
		FileMatches:   fileMatches,
	}

	var buf bytes.Buffer
	if err := s.Result.Execute(&buf, &res); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}

func (s *Server) servePrint(w http.ResponseWriter, r *http.Request) {
	err := s.servePrintErr(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func (s *Server) serveSearchBoxErr(w http.ResponseWriter, r *http.Request) error {
	stats, err := s.Searcher.Stats()
	if err != nil {
		return err
	}
	var buf bytes.Buffer

	uniq := map[string]struct{}{}
	for _, r := range stats.Repos {
		uniq[r] = struct{}{}
	}

	stats.Repos = stats.Repos[:0]
	for k := range uniq {
		stats.Repos = append(stats.Repos, k)
	}
	sort.Strings(stats.Repos)
	d := SearchBoxInput{
		LastQuery: "",
		Stats:     stats,
	}
	if err := s.SearchBox.Execute(&buf, &d); err != nil {
		return err
	}
	w.Write(buf.Bytes())
	return nil
}

func (s *Server) serveSearchBox(w http.ResponseWriter, r *http.Request) {
	if err := s.serveSearchBoxErr(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func (s *Server) serveListReposErr(q query.Q, qStr string, w http.ResponseWriter, r *http.Request) error {
	ctx := context.Background()
	repos, err := s.Searcher.List(ctx, q)
	if err != nil {
		return err
	}

	res := RepoListInput{
		LastQuery: qStr,
		QueryStr:  qStr,
		RepoCount: len(repos.Repos),
		Repo:      repos.Repos,
	}

	var buf bytes.Buffer
	if err := s.RepoList.Execute(&buf, &res); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}

func (s *Server) servePrintErr(w http.ResponseWriter, r *http.Request) error {
	if s.Print == nil {
		return fmt.Errorf("no printing template defined.")
	}

	qvals := r.URL.Query()
	fileStr := qvals.Get("f")
	repoStr := qvals.Get("r")
	queryStr := qvals.Get("q")

	qs := []query.Q{
		&query.Substring{Pattern: fileStr, FileName: true},
		&query.Repo{Pattern: repoStr},
	}

	if branchStr := qvals.Get("b"); branchStr != "" {
		qs = append(qs, &query.Branch{Pattern: branchStr})
	}

	q := &query.And{qs}

	sOpts := zoekt.SearchOptions{
		Whole: true,
	}

	ctx := context.Background()
	result, err := s.Searcher.Search(ctx, q, &sOpts)
	if err != nil {
		return err
	}

	if len(result.Files) != 1 {
		return fmt.Errorf("got %d matches, want 1", len(result.Files))
	}

	f := result.Files[0]
	d := PrintInput{
		Name:      f.Name,
		Repo:      f.Repo,
		Content:   string(f.Content),
		LastQuery: queryStr,
	}

	var buf bytes.Buffer
	if err := s.Print.Execute(&buf, &d); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}
