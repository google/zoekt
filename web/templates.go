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
	"html/template"
	"log"
)

// Top provides the standard templates in parsed form
var Top = template.New("top").Funcs(Funcmap)

// TemplateText contains the text of the standard templates.
var TemplateText = map[string]string{

	"didyoumean": `<html>
  <head>
    <title>Error</title>
  </head>
  <body>
    <p>{{.Message}}. Did you mean <a href="/search?q={{.Suggestion}}">{{.Suggestion}}</a> ?
  </body>
</html>
`,

	// the template for the search box.
	"searchbox": `
  <form action="search">
    Search some code: <input
      autofocus
      onfocus="this.value = this.value;"
      {{if .Query}}value={{.Query}}
      {{end}}type="text" name="q"> Max results:  <input style="width: 5em;" type="text" name="num" value="{{.Num}}"> <input type="submit" value="Search">
  </form>
`,

	// search box for the entry page.
	"search": `<html>
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
{{template "searchbox" .Last}}
</div>

<div style="display: flex; justify-content: space-around; flex-direction: row;">

<div>
  Search examples:
  <div style="margin-left: 4em;">
  <dl>
    <dt><a href="search?q=needle">needle</a></dt><dd>search for "needle"
  </dd>
    <dt><a href="search?q=thread+or+needle">thread or needle</a></dt><dd>search for either "thread" or "needle"
  </dd>
    <dt><a href="search?q=class+needle">class needle</a></dt><dd>search for files containing both "class" and "needle"
  </dd>
    <dt><a href="search?q=class+Needle">class Needle</a></dt><dd>search for files containing both "class" (case insensitive) and "Needle" (case sensitive)
  </dd>
    <dt><a href="search?q=class+Needle+case:yes">class Needle case:yes</a></dt><dd>search for files containing "class" and "Needle", both case sensitively
  </dd>
    <dt><a href="search?q=%22class Needle%22">"class Needle"</a></dt><dd>search for files with the phrase "class Needle"
  </dd>
     <dt><a href="search?q=needle+-hay">needle -hay</a></dt><dd>search for files with the word "needle" but not the word "hay"
  </dd>
    <dt><a href="search?q=path+file:java">path file:java</a></dt><dd>search for the word "path" in files whose name contains "java"
  </dd>
    <dt><a href="search?q=f:%5C.c%24">f:\.c$</a></dt><dd>search for files whose name ends with ".c"
  </dd>
    <dt><a href="search?q=path+-file:java">path -file:java</a></dt><dd>search for the word "path" excluding files whose name contains "java"</dd>
    <dt><a href="search?q=foo.*bar">foo.*bar</a></dt><dd>search for the regular expression "foo.*bar"</dd>
    <dt><a href="search?q=-%28Path File%29 Stream">-(Path File) Stream</a></dt><dd>search "Stream", but exclude files containing both "Path" and "File"</dd>
    <dt><a href="search?q=-Path%5c+file+Stream">-Path\ file Stream</a></dt><dd>search "Stream", but exclude files containing "Path File"</dd>
    <dt><a href="search?q=phone+r:droid">phone r:droid</a></dt><dd>search for "phone" in repositories whose name contains "droid"</dd>
    <dt><a href="search?q=phone+b:master">phone b:aster</a></dt><dd>for Git repos, find "phone" in files in branches whose name contains "master".</dd>
    <dt><a href="search?q=phone+b:HEAD">phone b:HEAD</a></dt><dd>for Git repos, find "phone" in the default ('HEAD') branch.</dd>
  </dl>
  </div>
</div>

<div>

<p>
Used {{HumanUnit .Stats.IndexBytes}} memory for
{{.Stats.Documents}} documents ({{HumanUnit .Stats.ContentBytes}})
from {{.Stats.Repos}} repositories.


<p>
To list repositories, try:
  <div style="margin-left: 4em;">
  <dl>
    <dt><a href="search?q=r:droid">r:droid</a></dt><dd>list repositories whose name contains "droid".</dd>
    <dt><a href="search?q=r:go+-r:google">r:go -r:google</a></dt><dd>list repositories whose name contains "go" but not "google".</dd>
  </dl>
  </div>
</p>
</div>
</div>

<hr>

<div>
<a href="about">About</a>
</div>

</body>
</html>
`,

	"results": `<html>
  <head>
    <title>Results for {{.QueryStr}}</title>
  </head>
<body>
  {{template "searchbox" .Last}}
<hr>
  {{if .Stats.Crashes}}<br><b>{{.Stats.Crashes}} shards crashed</b><br>{{end}}
  Found {{.Stats.MatchCount}} results in {{.Stats.FileCount}} files
    ({{HumanUnit .Stats.IndexBytesLoaded}}B index data,
     {{.Stats.NgramMatches}} ngram matches,
     {{.Stats.FilesConsidered}} docs considered,
     {{.Stats.FilesLoaded}} docs ({{HumanUnit .Stats.ContentBytesLoaded}}B) loaded,
     {{.Stats.FilesSkipped}} docs skipped): for
  <pre style="background: #ffc;">{{.Query}} with options {{.SearchOptions}}</pre>
  in {{.Stats.Duration}} (queued: {{.Stats.Wait}})
  <p>
  {{range .FileMatches}}
    {{if .URL}}<a href="{{.URL}}">{{end}}
    <tt><b>{{.Repo}}</b>:<b>{{.FileName}}</b>{{if .URL}}</a>{{end}}:{{if .Branches}}<small>[{{range .Branches}}{{.}}, {{end}}]</small>{{end}} </tt>
      {{if .DuplicateFile}}
         duplicate result <tt>{{.DuplicateFile}}</tt><br>
      {{else}}
        <div style="background: #eef;">
        {{range .Matches}}
          <pre>{{if .URL}}<a href="{{.URL}}">{{end}}{{.LineNum}}{{if .URL}}</a>{{end}}: {{range .Fragments}}{{.Pre}}<b>{{.Match}}</b>{{.Post}}{{end}}</pre>
        {{end}}
        </div>
      {{end}}
  {{end}}
</body>
</html>
`,

	"repolist": `<html>
  <head>
    <title>Repo search result for {{.Last.Query}}</title>
  </head>
<body>
{{template "searchbox" .Last}}
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
      <small>{{HumanUnit .Files}} files ({{HumanUnit .Size}})</small>
   </li>
  {{end}}
  </ul>
</body>
</html>
`,

	"print": `
<html>
  <head>
    <title>{{.Repo}}:{{.Name}}</title>
  </head>
<body>{{template "searchbox" .Last}}
<hr>
<p>
  <tt>{{.Repo}} : {{.Name}}</tt>
</p>


<div style="background: #eef;">
{{ range $index, $ln := .Lines}}
  <pre><a name="l{{Inc $index}}" href="#l{{Inc $index}}">{{Inc $index}}</a>: {{$ln}}</pre>
{{end}}
<pre>
</pre>
</div>
</body>
</html>
`,

	"about": `
  <head>
    <title>About <em>zoekt</em></title>
  </head>
<body>

<p>
  This is <a href="http://github.com/google/zoekt"><em>zoekt</em></a>,
  an open-source full text search engine.
</p>

<p>
Used {{HumanUnit .Stats.IndexBytes}} memory for
{{.Stats.Documents}} documents ({{HumanUnit .Stats.ContentBytes}})
from {{.Stats.Repos}} repositories.
</p>

<p>

{{if .Version}}<em>Zoekt</em> version {{.Version}}, uptime{{else}}Uptime{{end}} {{.Uptime}}

</p>
`,
}

func init() {
	for k, v := range TemplateText {
		_, err := Top.New(k).Parse(v)
		if err != nil {
			log.Panicf("parse(%s): %v:", k, err)
		}
	}
}
