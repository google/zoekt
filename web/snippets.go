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
	"html/template"
	"log"
	"net/url"
	"strconv"

	"github.com/google/zoekt"
)

func formatResults(result *zoekt.SearchResult, localPrint bool) ([]*FileMatch, error) {
	var fmatches []*FileMatch

	templateMap := map[string]*template.Template{}
	fragmentMap := map[string]*template.Template{}
	if !localPrint {
		for repo, str := range result.RepoURLs {
			tpl, err := template.New("url").Parse(str)
			if err != nil {
				log.Println("url template: %v", err)
				tpl = nil
			}
			templateMap[repo] = tpl
		}
		for repo, str := range result.LineFragments {
			tpl, err := template.New("lineFragment").Parse(str)
			if err != nil {
				log.Println("fragment template: %v", err)
				tpl = nil
			}
			fragmentMap[repo] = tpl
		}
	}
	getFragment := func(repo string, linenum int) string {
		if tpl := fragmentMap[repo]; tpl != nil {
			var buf bytes.Buffer
			if err := tpl.Execute(&buf, map[string]string{
				"LineNumber": strconv.Itoa(linenum),
			}); err != nil {
				log.Println("fragment template: %v", err)
				return ""
			}
			return buf.String()
		}
		return ""
	}
	getURL := func(repo, filename string, branches []string) string {
		if localPrint {
			v := make(url.Values)
			v.Add("r", repo)
			v.Add("f", filename)
			if len(branches) > 0 {
				v.Add("b", branches[0])
			}
			return "print?" + v.Encode()
		}

		if tpl := templateMap[repo]; tpl != nil {
			var buf bytes.Buffer
			b := ""
			if len(branches) > 0 {
				b = branches[0]
			}
			err := tpl.Execute(&buf, map[string]string{
				"Branch": b,
				"Path":   filename,
			})
			if err != nil {
				log.Println("url template: %v", err)
				return ""
			}
			return buf.String()
		}
		return ""
	}

	for _, f := range result.Files {
		fMatch := FileMatch{
			FileName: f.Name,
			Repo:     f.Repo,
			Branches: f.Branches,
			URL:      getURL(f.Repo, f.Name, f.Branches),
		}

		for _, m := range f.Matches {
			md := Match{
				FileName: f.Name,
				LineNum:  m.LineNum,
				URL:      fMatch.URL + "#" + getFragment(f.Repo, m.LineNum),
			}

			lastEnd := 0
			line := m.Line
			for i, f := range m.Fragments {
				l := f.LineOff
				e := l + f.MatchLength

				frag := Fragment{
					Pre:   string(line[lastEnd:l]),
					Match: string(line[l:e]),
				}
				if i == len(m.Fragments)-1 {
					frag.Post = string(m.Line[e:])
				}

				md.Fragments = append(md.Fragments, frag)
				lastEnd = e
			}
			fMatch.Matches = append(fMatch.Matches, md)
		}
		fmatches = append(fmatches, &fMatch)
	}
	return fmatches, nil
}
