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

package rest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
	"golang.org/x/net/context"
)

const jsonContentType = "application/json; charset=utf-8"

type httpError struct {
	msg    string
	status int
}

func (e *httpError) Error() string { return fmt.Sprintf("%d: %s", e.status, e.msg) }

func Search(s zoekt.Searcher, w http.ResponseWriter, r *http.Request) {
	if err := serveSearchAPIErr(s, w, r); err != nil {
		if e, ok := err.(*httpError); ok {
			http.Error(w, e.msg, e.status)
		}
		http.Error(w, err.Error(), http.StatusTeapot)
	}
}

func verifyAPIInput(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Method != http.MethodPost {
		return nil, &httpError{"must use POST", http.StatusMethodNotAllowed}
	}

	if got := r.Header.Get("Content-Type"); got != jsonContentType {
		return nil, &httpError{"must use " + jsonContentType, http.StatusNotAcceptable}

	}

	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, &httpError{err.Error(), http.StatusBadRequest}
	}

	return content, nil
}

func serveSearchAPIErr(s zoekt.Searcher, w http.ResponseWriter, r *http.Request) error {
	content, err := verifyAPIInput(w, r)
	if err != nil {
		return err
	}

	var req SearchRequest
	if err := json.Unmarshal(content, &req); err != nil {
		return &httpError{err.Error(), http.StatusBadRequest}
	}

	rep, err := serveSearchAPIStructured(r.Context(), s, &req)
	if err != nil {
		return err
	}

	return dumpAPIOutput(w, rep)
}

func dumpAPIOutput(w http.ResponseWriter, rep interface{}) error {
	content, err := json.Marshal(rep)
	if err != nil {
		return &httpError{err.Error(), http.StatusInternalServerError}
	}

	w.Header().Set("Content-Type", jsonContentType)
	if _, err := w.Write(content); err != nil {
		return &httpError{err.Error(), http.StatusInternalServerError}
	}
	return nil
}

func serveSearchAPIStructured(ctx context.Context, searcher zoekt.Searcher, req *SearchRequest) (*SearchResponse, error) {
	q, err := query.Parse(req.Query)
	if err != nil {
		msg := "parse error: " + err.Error()
		return &SearchResponse{Error: &msg}, nil
	}

	var restrictions []query.Q
	for _, r := range req.Restrict {
		var branchQs []query.Q
		for _, b := range r.Branches {
			branchQs = append(branchQs, &query.Branch{Pattern: b})
		}

		restrictions = append(restrictions,
			query.NewAnd(&query.Repo{Pattern: r.Repo}, query.NewOr(branchQs...)))
	}

	finalQ := query.Simplify(query.NewAnd(q, query.NewOr(restrictions...)))
	var options zoekt.SearchOptions
	options.SetDefaults()

	result, err := searcher.Search(ctx, finalQ, &options)
	if err != nil {
		return nil, &httpError{err.Error(), http.StatusInternalServerError}
	}

	// TODO - make this tunable. Use a query param or a JSON struct?
	num := 50
	if len(result.Files) > num {
		result.Files = result.Files[:num]
	}
	var resp SearchResponse
	for _, f := range result.Files {
		srf := SearchResponseFile{
			Repo:     f.Repository,
			Branches: f.Branches,
			FileName: f.FileName,
			// TODO - set version
		}
		for _, m := range f.LineMatches {
			srl := &SearchResponseLine{
				LineNumber: m.LineNumber,
				Line:       string(m.Line),
			}

			// Convert to unicode indices.
			charOffsets := make([]int, len(m.Line), len(m.Line)+1)
			j := 0
			for i := range srl.Line {
				charOffsets[i] = j
				j++
			}
			charOffsets = append(charOffsets, j)

			for _, fr := range m.LineFragments {
				srfr := SearchResponseMatch{
					Start: charOffsets[fr.LineOffset],
					End:   charOffsets[fr.LineOffset+fr.MatchLength],
				}

				srl.Matches = append(srl.Matches, &srfr)
			}
			srf.Lines = append(srf.Lines, srl)
		}
		resp.Files = append(resp.Files, &srf)
	}

	return &resp, nil
}
