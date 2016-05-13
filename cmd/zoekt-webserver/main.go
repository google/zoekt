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

package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	"github.com/google/zoekt/query"
)

// TODO - split this into a library.

type httpServer struct {
	searcher   zoekt.Searcher
	localPrint bool
}

var didYouMeanTemplate = template.Must(template.New("didyoumean").Parse(`<html>
  <head>
    <title>Error</title>
  </head>
  <body>
    <p>{{.Message}}. Did you mean <a href="/search?q={{.Suggestion}}">{{.Suggestion}}</a> ?
  </body>
</html>
`))

func (s *httpServer) serveSearch(w http.ResponseWriter, r *http.Request) {
	err := s.serveSearchErr(w, r)

	if suggest, ok := err.(*query.SuggestQueryError); ok {
		var buf bytes.Buffer
		if err := didYouMeanTemplate.Execute(&buf, suggest); err != nil {
			http.Error(w, err.Error(), http.StatusTeapot)
		}

		w.Write(buf.Bytes())
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func (s *httpServer) servePrint(w http.ResponseWriter, r *http.Request) {
	err := s.servePrintErr(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

const searchBox = `
  <form action="search">
    Search some code: <input type="text" name="q"> Max results:  <input style="width: 5em;" type="text" name="num" value="50"> <input type="submit" value="Search">
  </form>
`

func (s *httpServer) serveSearchBox(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`<html>
<head>
<style>
dt {
    font-family: monospace;
}
</style>
</head>
<body>
<div style="margin: 3em; padding 3em; position: center;">
` + searchBox + `
</div>

Examples:
<div style="margin-left: 4em;">
<dl>
  <dt>needle</dt><dd>search for "needle"
</dd>
  <dt>class needle</dt><dd>search for files containing both "class" and "needle"
</dd>
  <dt>class Needle</dt><dd>search for files containing both "class" (case insensitive) and "Needle" (case sensitive)
</dd>
  <dt>class Needle case:yes</dt><dd>search for files containing "class" and "Needle", both case sensitively
</dd>
  <dt>"class Needle"</dt><dd>search for files with the phrase "class Needle"
</dd>
  <dt>needle -hay</dt><dd>search for files with the word "needle" but not the word "hay"
</dd>
  <dt>path file:java</dt><dd>search for the word "path" in files whose name contains "java"
</dd>
  <dt>f:\.c$</dt><dd>search for files whose name ends with ".c"
</dd>
  <dt>path -file:java</dt><dd>search for the word "path" excluding files whose name contains "java"</dd>
  <dt>foo.*bar</dt><dd>search for the regular expression "foo.*bar"</dd>
  <dt>-(Path File) Stream</dt><dd>search "Stream", but exclude files containing both "Path" and "File"</dd>
  <dt>-Path\ File Stream</dt><dd>search "Stream", but exclude files containing "Path File"</dd>
  <dt>repo:android</dt><dd>restrict to the "android" repository</dd>
  <dt>branch:master</dt><dd>for Git repos, only look for files in the "master" branch.</dd>
</dl>
</div>
</body>
</html>
`))
}

type MatchLine struct {
	LineNum int
	Line    string
}

type FileMatchData struct {
	FileName string
	Repo     string
	Branches []string
	Matches  []MatchData
	URL      string
}

type MatchData struct {
	FileName  string
	Pre       string
	MatchText string
	Post      string
	LineNum   int
}

type ResultsPage struct {
	Query       string
	Stats       zoekt.Stats
	Duration    time.Duration
	FileMatches []FileMatchData
}

var resultTemplate = template.Must(template.New("page").Parse(`<html>
  <head>
    <title>Search results</title>
  </head>
<body>` + searchBox +
	`  <hr>
  Found {{.Stats.MatchCount}} results in {{.Stats.FileCount}} files ({{.Stats.NgramMatches}} ngram matches,
    {{.Stats.FilesConsidered}} docs considered, {{.Stats.FilesLoaded}} docs ({{.Stats.HumanBytesLoaded}}B) loaded,
    {{.Stats.FilesSkipped}} docs skipped): for
  <pre style="background: #ffc;">{{.Query}}</pre>
  in {{.Stats.Duration}}
  <p>
  {{range .FileMatches}}
    {{if .URL}}<a href="{{.URL}}">{{end}}
    <tt><b>{{.Repo}}</b>:<b>{{.FileName}}</b>{{if .URL}}</a>{{end}}:{{if .Branches}}<small>[{{range .Branches}}{{.}}, {{end}}]</small>{{end}} </tt>

      <div style="background: #eef;">
    {{range .Matches}}
        <pre>{{.LineNum}}: {{.Pre}}<b>{{.MatchText}}</b>{{.Post}}</pre>
    {{end}}
      </div>
  {{end}}
</body>
</html>
`))

func (s *httpServer) serveSearchErr(w http.ResponseWriter, r *http.Request) error {
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

	numStr := qvals.Get("num")

	num, err := strconv.Atoi(numStr)
	if err != nil {
		num = 50
	}

	sOpts := zoekt.SearchOptions{}

	result, err := s.searcher.Search(q, &sOpts)
	if err != nil {
		return err
	}

	res := ResultsPage{
		Stats: result.Stats,
		Query: q.String(),
	}

	if len(result.Files) > num {
		result.Files = result.Files[:num]
	}

	for _, f := range result.Files {
		fMatch := FileMatchData{
			FileName: f.Name,
			Repo:     f.Repo,
			Branches: f.Branches,
		}

		if s.localPrint {
			v := make(url.Values)
			v.Add("r", f.Repo)
			v.Add("f", f.Name)
			if len(f.Branches) > 0 {
				v.Add("b", f.Branches[0])
			}
			fMatch.URL = "print?" + v.Encode()
		} else if len(f.Branches) > 0 {
			urlTemplate := result.RepoURLs[f.Repo]
			t, err := template.New("url").Parse(urlTemplate)
			if err != nil {
				log.Println("url template: %v", err)
			} else {
				var buf bytes.Buffer
				err := t.Execute(&buf, map[string]string{
					"Branch": f.Branches[0],
					"Path":   f.Name,
				})
				if err != nil {
					log.Println("url template: %v", err)
				} else {
					fMatch.URL = buf.String()
				}
			}
		}

		for _, m := range f.Matches {
			l := m.LineOff
			e := l + m.MatchLength
			if e > len(m.Line) {
				e = len(m.Line)
				log.Printf("%s %#v", f.Name, m)
			}
			fMatch.Matches = append(fMatch.Matches, MatchData{
				FileName:  f.Name,
				LineNum:   m.LineNum,
				Pre:       string(m.Line[:l]),
				MatchText: string(m.Line[l:e]),
				Post:      string(m.Line[e:]),
			})
		}
		res.FileMatches = append(res.FileMatches, fMatch)
	}

	var buf bytes.Buffer
	if err := resultTemplate.Execute(&buf, res); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}

var printTemplate = template.Must(template.New("print").Parse(`
  <head>
    <title>{{.Repo}}:{{.Name}}</title>
  </head>
<body>` + searchBox +
	`  <hr>

<pre>{{.Content}}
</pre>`))

func (s *httpServer) servePrintErr(w http.ResponseWriter, r *http.Request) error {
	qvals := r.URL.Query()
	fileStr := qvals.Get("f")
	repoStr := qvals.Get("r")
	branchStr := qvals.Get("b")

	q := &query.And{[]query.Q{
		&query.Substring{Pattern: fileStr, FileName: true},
		&query.Repo{Name: repoStr},
		&query.Branch{Name: branchStr},
	}}

	sOpts := zoekt.SearchOptions{
		Whole: true,
	}

	result, err := s.searcher.Search(q, &sOpts)
	if err != nil {
		return err
	}

	if len(result.Files) != 1 {
		return fmt.Errorf("got %d matches, want 1", len(result.Files))
	}

	f := result.Files[0]
	type fData struct {
		Repo, Name, Content string
	}

	d := fData{
		Name:    f.Name,
		Repo:    f.Repo,
		Content: string(f.Content),
	}

	var buf bytes.Buffer
	if err := printTemplate.Execute(&buf, d); err != nil {
		return err
	}

	w.Write(buf.Bytes())
	return nil
}

func main() {
	listen := flag.String("listen", ":6070", "address to listen on.")
	index := flag.String("index", build.DefaultDir, "index file glob to use")
	print := flag.Bool("print", false, "local result URLs")
	flag.Parse()

	searcher, err := zoekt.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	serv := httpServer{
		searcher:   searcher,
		localPrint: *print,
	}

	http.HandleFunc("/search", serv.serveSearch)
	http.HandleFunc("/", serv.serveSearchBox)
	if *print {
		http.HandleFunc("/print", serv.servePrint)
	}

	log.Printf("serving on %s", *listen)
	err = http.ListenAndServe(*listen, nil)
	log.Printf("ListenAndServe: %v", err)
}