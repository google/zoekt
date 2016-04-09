package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanwen/codesearch"
)

type httpServer struct {
	searcher codesearch.Searcher
}

func (s *httpServer) serveSearch(w http.ResponseWriter, r *http.Request) {
	if err := s.serveSearchErr(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func (s *httpServer) serveSearchBox(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`<html>
<head>
</head>
<body>
<div style="margin: 3em; padding 3em; position: center;">
  <form action="search">
    Search some code: <input type="text" name="q"> Max results:  <input type="text" name="num" value="50">
  <select name="case">
    <option value="auto">auto</option>
    <option value="no">no</option>
    <option value="yes">yes</option>
  </select>
<br>
    <input type="submit" value="Search">
  </form>
</div>
</body>
</html>
`))
}

type MatchLine struct {
	LineNum int
	Line    string
}

type MatchData struct {
	FileName  string
	Pre       string
	MatchText string
	Post      string
	LineNum   int
}

type ResultsPage struct {
	Query         string
	CaseSensitive bool
	MatchCount    int
	Duration      time.Duration
	Matches       []MatchData
}

var resultTemplate = template.Must(template.New("page").Parse(`<html>
  <head>
    <title>Search results</title>
  </head>
<body>
  Found {{.MatchCount}} results for
  {{if .CaseSensitive}}case sensitive{{end}} search of
  <pre style="background: #ffc;">{{.Query}}</pre>
  in {{.Duration}}:
  <p>
  {{range .Matches}}
    <tt>{{.FileName}}:{{.LineNum}}</tt>
    <br>
    <div style="background: #eef;">
      <pre>{{.Pre}}<b>{{.MatchText}}</b>{{.Post}}</pre>
    </div>
  {{end}}
</body>
</html>
`))

func (s *httpServer) serveSearchErr(w http.ResponseWriter, r *http.Request) error {
	qvals := r.URL.Query()
	query := qvals.Get("q")
	numStr := qvals.Get("num")
	if query == "" {
		return fmt.Errorf("no query found")
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		num = 50
	}

	startT := time.Now()

	q := codesearch.SubstringQuery{
		Pattern: query,
	}

	if c := qvals.Get("case"); c != "" {
		switch c {
		case "auto":
			q.CaseSensitive = strings.ToLower(query) != query
		default:
			q.CaseSensitive = c == "yes"
		}
	}

	matches, err := s.searcher.Search(&q)
	if err != nil {
		return err
	}

	res := ResultsPage{
		Query:         query,
		CaseSensitive: q.CaseSensitive,
		MatchCount:    len(matches),
		Duration:      time.Now().Sub(startT),
	}

	if len(matches) > num {
		matches = matches[:num]
	}

	for _, m := range matches {
		// TODO - visualize all the matches.
		l := m.Matches[0].LineOff
		res.Matches = append(res.Matches, MatchData{
			FileName:  m.Name,
			LineNum:   m.Matches[0].LineNum,
			Pre:       m.Matches[0].Line[:l],
			MatchText: m.Matches[0].Line[l : l+len(query)],
			Post:      m.Matches[0].Line[l+len(query):],
		})
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
	index := flag.String("index", ".csindex.*", "index file glob to use")
	flag.Parse()

	searcher, err := codesearch.NewShardedSearcher(*index)
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
