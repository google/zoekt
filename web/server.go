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
	"sync"
	"time"

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

	// If set, show files from the index.
	Print bool

	// This should contain the following templates: "didyoumean"
	// (for suggestions), "repolist" (for the repo search result
	// page), "result" for the search results, "search" (for the
	// opening page), "box" for the search query input element and
	// "print" for the show file functionality.
	Top *template.Template

	didYouMean *template.Template
	repolist   *template.Template
	search     *template.Template
	result     *template.Template
	print      *template.Template

	mu            sync.Mutex
	templateCache map[string]*template.Template
}

func (s *Server) getTemplate(str string) *template.Template {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.templateCache[str]
	if t != nil {
		return t
	}

	t, err := template.New("cache").Parse(str)
	if err != nil {
		log.Println("template parse error: %v", err)
		t = template.Must(template.New("empty").Parse(""))
	}
	s.templateCache[str] = t
	return t
}

func NewMux(s *Server) (*http.ServeMux, error) {
	s.print = s.Top.Lookup("print")
	if s.print == nil {
		return nil, fmt.Errorf("missing template 'print'")
	}

	for k, v := range map[string]**template.Template{
		"didyoumean": &s.didYouMean,
		"results":    &s.result,
		"print":      &s.print,
		"search":     &s.search,
		"repolist":   &s.repolist,
	} {
		*v = s.Top.Lookup(k)
		if *v == nil {
			return nil, fmt.Errorf("missing template %q", k)
		}
	}

	s.templateCache = map[string]*template.Template{}

	mux := http.NewServeMux()
	mux.HandleFunc("/search", s.serveSearch)
	mux.HandleFunc("/", s.serveSearchBox)
	if s.Print {
		mux.HandleFunc("/print", s.servePrint)
	}
	return mux, nil
}

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	err := s.serveSearchErr(w, r)

	if suggest, ok := err.(*query.SuggestQueryError); ok {
		var buf bytes.Buffer
		if err := s.didYouMean.Execute(&buf, suggest); err != nil {
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
		MaxWallTime:            10 * time.Second,
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

	fileMatches, err := s.formatResults(result, s.Print)
	if err != nil {
		return err
	}

	res := ResultInput{
		Last:          LastInput{Query: queryStr},
		Stats:         result.Stats,
		Query:         q.String(),
		QueryStr:      queryStr,
		SearchOptions: sOpts.String(),
		FileMatches:   fileMatches,
	}

	var buf bytes.Buffer
	if err := s.result.Execute(&buf, &res); err != nil {
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
		Stats: stats,
	}
	if err := s.search.Execute(&buf, &d); err != nil {
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
		Last:      LastInput{Query: qStr},
		RepoCount: len(repos.Repos),
	}
	for _, r := range repos.Repos {
		t := s.getTemplate(r.CommitURLTemplate)

		repo := Repository{
			Name:      r.Name,
			URL:       r.URL,
			IndexTime: r.IndexTime,
		}
		for _, b := range r.Branches {
			var buf bytes.Buffer
			if err := t.Execute(&buf, b); err != nil {
				return err
			}
			repo.Branches = append(repo.Branches,
				Branch{
					Name:    b.Name,
					Version: b.Version,
					URL:     buf.String(),
				})
		}
		res.Repos = append(res.Repos, repo)
	}

	var buf bytes.Buffer
	if err := s.repolist.Execute(&buf, &res); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}

func (s *Server) servePrintErr(w http.ResponseWriter, r *http.Request) error {
	if !s.Print {
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
		Name:    f.Name,
		Repo:    f.Repo,
		Content: string(f.Content),
		Last:    LastInput{Query: queryStr},
	}

	var buf bytes.Buffer
	if err := s.print.Execute(&buf, &d); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}
