package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/hanwen/codesearch"
)

type httpServer struct {
	searcher codesearch.Searcher
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

	if suggest, ok := err.(*codesearch.SuggestQueryError); ok {
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
</head>
<body>
<div style="margin: 3em; padding 3em; position: center;">
` + searchBox + `
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
	MatchCount    int
	Duration      time.Duration
	Matches       []MatchData
}

var resultTemplate = template.Must(template.New("page").Parse(`<html>
  <head>
    <title>Search results</title>
  </head>
<body>` + searchBox +
`  <hr>
  Found {{.MatchCount}} results for
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
	q, err := codesearch.Parse(query)
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

	startT := time.Now()

	matches, err := s.searcher.Search(q)
	if err != nil {
		return err
	}

	res := ResultsPage{
		Query:         q.String(),
		MatchCount:    len(matches),
		Duration:      time.Now().Sub(startT),
	}

	if len(matches) > num {
		matches = matches[:num]
	}

	for _, m := range matches {
		// TODO - visualize all the matches.
		l := m.Matches[0].LineOff
		e := l+len(query)
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
