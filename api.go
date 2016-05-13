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

package zoekt

import (
	"time"

	"github.com/google/zoekt/query"
)

// FileMatch contains all the matches within a file.
type FileMatch struct {
	// Ranking; the higher, the better.
	Score    float64 // TODO - hide this field?
	Name     string
	Repo     string
	Branches []string
	Matches  []Match

	// Only set if requested
	Content []byte
}

// Match is a match for a single atomic query within a file.
type Match struct {
	// The line in which a match was found.
	Line      []byte
	LineStart int
	LineEnd   int
	LineNum   int

	// Offset within the line.
	LineOff int

	// Offset from file start
	Offset      uint32
	MatchLength int

	// If set, this was a match on the filename.
	FileName bool

	// The higher the better. Only ranks the quality of the match
	// within the file, does not take rank of file into account
	Score float64
}

// Stats contains interesting numbers on the search
type Stats struct {
	NgramMatches    int
	FilesConsidered int
	FilesLoaded     int
	BytesLoaded     int64
	FileCount       int
	MatchCount      int
	Duration        time.Duration
	FilesSkipped    int
}

func (s *Stats) Add(o Stats) {
	s.NgramMatches += o.NgramMatches
	s.FilesLoaded += o.FilesLoaded
	s.MatchCount += o.MatchCount
	s.FileCount += o.FileCount
	s.FilesConsidered += o.FilesConsidered
	s.BytesLoaded += o.BytesLoaded
	s.FilesSkipped += o.FilesSkipped
}

// SearchResult contains search matches and extra data
type SearchResult struct {
	Stats
	Files []FileMatch

	// RepoURLs holds a repo => template string map.
	RepoURLs map[string]string
}

type Searcher interface {
	Search(q query.Q, opts *SearchOptions) (*SearchResult, error)
	Stats() (*RepoStats, error)
	Close()
}

type RepoStats struct {
	Repos        []string
	IndexBytes   int64
	ContentBytes int64
}

func (s *RepoStats) Add(o *RepoStats) {
	s.Repos = append(s.Repos, o.Repos...)
	s.IndexBytes += o.IndexBytes
	s.ContentBytes += o.ContentBytes
}

type SearchOptions struct {
	// Return the whole file.
	Whole bool
}
