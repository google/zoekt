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
	"time"

	"github.com/google/zoekt"
)

type LastInput struct {
	Query string
}

// Result holds the data provided to the search results template.
type ResultInput struct {
	Last          LastInput
	QueryStr      string
	Query         string
	Stats         zoekt.Stats
	Duration      time.Duration
	FileMatches   []*FileMatch
	SearchOptions string
}

// FileMatch holds the per file data provided to search results template
type FileMatch struct {
	FileName string
	Repo     string
	Branches []string
	Matches  []Match
	URL      string
}

// Match holds the per line data provided to the search results template
type Match struct {
	URL      string
	FileName string
	LineNum  int

	Fragments []Fragment
}

// Fragment holds data of a single contiguous match within in a line
// for the results template.
type Fragment struct {
	Pre   string
	Match string
	Post  string
}

// SearchBoxInput is provided to the SearchBox template.
type SearchBoxInput struct {
	Last  LastInput
	Stats *zoekt.RepoStats
}

// RepoListInput is provided to the RepoList template.
type RepoListInput struct {
	Last      LastInput
	RepoCount int
	Repo      []string
}

// PrintInput is provided to the server.Print template.
type PrintInput struct {
	Repo, Name, Content string
	Last                LastInput
}
