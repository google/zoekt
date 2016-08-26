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

import "html/template"

var Top = template.New("top").Funcs(Funcmap)

var DidYouMeanTemplate = template.Must(Top.New("didyoumean").Parse(`<html>
  <head>
    <title>Error</title>
  </head>
  <body>
    <p>{{.Message}}. Did you mean <a href="/search?q={{.Suggestion}}">{{.Suggestion}}</a> ?
  </body>
</html>
`))

var QueryTemplate = template.Must(Top.New("box").Parse(`
  <form action="search">
    Search some code: <input autofocus {{if .Query}}value={{.Query}} {{end}} type="text" name="q"> Max results:  <input style="width: 5em;" type="text" name="num" value="50"> <input type="submit" value="Search">
  </form>
`))

var SearchBoxTemplate = template.Must(Top.New("search").Parse(
	`<html>
<head>
<style>
dt {
    font-family: monospace;
}
</style>
</head>
<title>Zoekt, en gij zult spinazie eten</title>
<body>
<div style="margin: 3em; padding 3em; position: center;">
{{template "box"}}
</div>

<div style="display: flex; justify-content: space-around; flex-direction: row;">

<div>
  Search examples:
  <div style="margin-left: 4em;">
  <dl>
    <dt>needle</dt><dd>search for "needle"
  </dd>
    <dt>thread or needle</dt><dd>search for either "thread" or "needle"
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
    <dt>phone repo:droid</dt><dd>search for "phone" in repositories whose name contains "droid"</dd>
    <dt>phone r:droid</dt><dd>search for "phone" to repositories whose name contains "droid"</dd>
    <dt>r:droid</dt><dd>list repositories whose name contains "droid"</dd>
    <dt>phone branch:aster</dt><dd>for Git repos, find "phone" in files in branches whose name contains "aster".</dd>
  </dl>
  </div>
</div>

<div>

<p> Used {{HumanUnit .Stats.IndexBytes}} memory for
{{.Stats.Documents}} documents ({{HumanUnit .Stats.ContentBytes}})
from {{len .Stats.Repos}} repositories.

<p>
To list repositories, try:
  <div style="margin-left: 4em;">
  <dl>
    <dt>r:droid</dt><dd>list repositories whose name contains "droid".</dd>
    <dt>r:go -r:google</dt><dd>list repositories whose name contains "go" but not "google".</dd>
  </dl>
  </div>
</p>
</div>
</body>
</html>
`))

var ResultsTemplate = template.Must(Top.New("results").Parse(`<html>
  <head>
    <title>Results for {{.QueryStr}}</title>
  </head>
<body>
  {{template "box" .Last}}
<hr>
  Found {{.Stats.MatchCount}} results in {{.Stats.FileCount}} files ({{.Stats.NgramMatches}} ngram matches,
    {{.Stats.FilesConsidered}} docs considered, {{.Stats.FilesLoaded}} docs ({{HumanUnit .Stats.BytesLoaded}}B) loaded,
    {{.Stats.FilesSkipped}} docs skipped): for
  <pre style="background: #ffc;">{{.Query}} with options {{.SearchOptions}}</pre>
  in {{.Stats.Duration}} (queued: {{.Stats.Wait}})
  <p>
  {{range .FileMatches}}
    {{if .URL}}<a href="{{.URL}}">{{end}}
    <tt><b>{{.Repo}}</b>:<b>{{.FileName}}</b>{{if .URL}}</a>{{end}}:{{if .Branches}}<small>[{{range .Branches}}{{.}}, {{end}}]</small>{{end}} </tt>

      <div style="background: #eef;">
    {{range .Matches}}
        <pre>{{if .URL}}<a href="{{.URL}}">{{end}}{{.LineNum}}{{if .URL}}</a>{{end}}: {{range .Fragments}}{{.Pre}}<b>{{.Match}}</b>{{.Post}}{{end}}</pre>
    {{end}}
      </div>
  {{end}}
</body>
</html>
`))

var RepoListTemplate = template.Must(Top.New("repolist").Parse(`<html>
  <head>
    <title>Repo search result for {{.Last.Query}}</title>
  </head>
<body>
{{template "box" .Last}}
 <hr>
  Found {{.RepoCount}} repositories:
  <p>
  {{range .Repos}}
    <li>
      <tt>{{if .URL}}<a href="{{.URL}}">{{end}}{{.Name}}{{if .URL}}</a>{{end}}
      </tt> (<small>{{.IndexTime.Format "Jan 02, 2006 15:04"}}</small>). Branches:
      {{range .Branches}}
         {{if .URL}}<a href="{{.URL}}">{{end}}{{.Name}}{{if .URL}}</a>{{end}},
      {{end}}
   </li>
  {{end}}
  </ul>
</body>
</html>
`))

var PrintTemplate = template.Must(Top.New("print").Parse(`
  <head>
    <title>{{.Repo}}:{{.Name}}</title>
  </head>
<body>{{template "box" .Last}}
 <hr>

<pre>{{.Content}}
</pre>`))
