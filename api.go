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
	"fmt"
	"time"

	"golang.org/x/net/context"

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

// Match holds the matches within a single line in a file.
type Match struct {
	// The line in which a match was found.
	Line      []byte
	LineStart int
	LineEnd   int
	LineNum   int

	// If set, this was a match on the filename.
	FileName bool

	// The higher the better. Only ranks the quality of the match
	// within the file, does not take rank of file into account
	Score     float64
	Fragments []MatchFragment
}

// MatchFragment holds a segment of matching text within a line.
type MatchFragment struct {
	// Offset within the line.
	LineOff int

	// Offset from file start
	Offset uint32

	// Number bytes that match.
	MatchLength int
}

// Stats contains interesting numbers on the search
type Stats struct {
	// Number of candidate matches as a result of searching ngrams.
	NgramMatches int

	// Files that we evaluated. Equivalent to files for which all
	// atom matches (including negations) evaluated to true.
	FilesConsidered int

	// Files for which we loaded file content to verify substring matches
	FilesLoaded int

	// Total length of files thus loaded.
	BytesLoaded int64

	// Number of files containing a match.
	FileCount int

	// Number of non-overlapping matches
	MatchCount int

	// Wall clock time for this search
	Duration time.Duration

	// Wall clock time for queued search.
	Wait time.Duration

	// Candidate files whose contents weren't examined because we
	// gathered enough matches.
	FilesSkipped int
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

	// FragmentNames holds a repo => template string map, for
	// the line number fragment.
	LineFragments map[string]string
}

type RepoList struct {
	Repos []string
}

type Searcher interface {
	Search(ctx context.Context, q query.Q, opts *SearchOptions) (*SearchResult, error)

	// List lists repositories. The query `q` can only contain
	// query.Repo atoms.
	List(ctx context.Context, q query.Q) (*RepoList, error)
	Stats() (*RepoStats, error)
	Close()
}

type RepoStats struct {
	Repos        []string
	Documents    int
	IndexBytes   int64
	ContentBytes int64
}

func (s *RepoStats) Add(o *RepoStats) {
	s.Repos = append(s.Repos, o.Repos...)
	s.IndexBytes += o.IndexBytes
	s.Documents += o.Documents
	s.ContentBytes += o.ContentBytes
}

type SearchOptions struct {
	// Return the whole file.
	Whole bool

	// Maximum number of matches: skip all processing an index
	// shard after we found this many non-overlapping matches.
	ShardMaxMatchCount int

	// Maximum number of matches: stop looking for more matches
	// once we have this many matches across shards.
	TotalMaxMatchCount int

	// Maximum number of important matches: skip processing
	// shard after we found this many important matches.
	ShardMaxImportantMatch int

	// Maximum number of important matches across shards.
	TotalMaxImportantMatch int

	// Abort the search after this much time has passed.
	MaxWallTime time.Duration
}

func (s *SearchOptions) String() string {
	return fmt.Sprintf("%#v", s)
}
