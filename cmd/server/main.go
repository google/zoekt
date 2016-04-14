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
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hanwen/zoekt"
)

type httpServer struct {
	searcher zoekt.Searcher
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

	if suggest, ok := err.(*zoekt.SuggestQueryError); ok {
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
  <dt>file</dt><dd>search for "file"
</dd>
  <dt>class file</dt><dd>search for files containing both "class" and "file"
</dd>
  <dt>class File</dt><dd>search for files containing both "class" (case insensitive) and "File" (case sensitive)
</dd>
  <dt>class File case:yes</dt><dd>search for files containing both "class" and "File", case sensitively
</dd>
  <dt>"class file"</dt><dd>search for files with the phrase "class file"
</dd>
  <dt>class -file</dt><dd>search for files with the word "class" but not the word "file"
</dd>
  <dt>path file:java</dt><dd>search for the word "path" in files whose name contains "java"
</dd>
  <dt>path -file:java</dt><dd>search for the word "path" excluding files whose name contains "java"
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
	Matches  []MatchData
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
  Found {{.Stats.MatchCount}} results in {{.Stats.FileCount}} files ({{.Stats.NgramMatches}} ngram matches, {{.Stats.FilesConsidered}} docs considered,{{.Stats.FilesLoaded}} docs loaded): for
  <pre style="background: #ffc;">{{.Query}}</pre>
  in {{.Stats.Duration}}
  <p>
  {{range .FileMatches}}
    <b><tt>{{.FileName}}:</tt></b>
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
	query := qvals.Get("q")
	q, err := zoekt.Parse(query)
	if err != nil {
		return err
	}

	numStr := qvals.Get("num")
	if query == "" {
		return fmt.Errorf("no query found")
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		num = 50
	}

	result, err := s.searcher.Search(q)
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
				Pre:       m.Line[:l],
				MatchText: m.Line[l:e],
				Post:      m.Line[e:],
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

func main() {
	listen := flag.String("listen", ":6070", "address to listen on.")
	index := flag.String("index", filepath.Join(os.Getenv("HOME"), ".csindex/*"), "index file glob to use")
	flag.Parse()

	searcher, err := zoekt.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	serv := httpServer{
		searcher: searcher,
	}

	http.HandleFunc("/search", serv.serveSearch)
	http.HandleFunc("/", serv.serveSearchBox)
	log.Printf("serving on %s", *listen)
	err = http.ListenAndServe(*listen, nil)
	log.Printf("ListenAndServe: %v", err)
}
