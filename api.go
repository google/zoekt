package codesearch

import (
	"time"
)

type FileMatch struct {
	// Ranking; the lower, the better.
	Rank    int
	Name    string
	Matches []Match
}

type Match struct {
	Line    string
	LineNum int
	LineOff int

	Offset      uint32
	MatchLength int
	FileName    bool
}

type Stats struct {
	NgramMatches int
	FilesLoaded  int
	FileCount    int
	MatchCount   int
	Duration     time.Duration
}

func (s *Stats) Add(o Stats) {
	s.NgramMatches += o.NgramMatches
	s.FilesLoaded += o.FilesLoaded
	s.MatchCount += o.MatchCount
	s.FileCount += o.FileCount
}

type SearchResult struct {
	Stats
	Files []FileMatch
}

type Searcher interface {
	Search(query Query) (*SearchResult, error)
	Close() error
}
