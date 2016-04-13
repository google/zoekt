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
	"log"
	"sort"
)

var _ = log.Println

// All the matches for a given file.
type mergedCandidateMatch struct {
	fileID        uint32
	matches       map[*SubstringQuery][]*candidateMatch
	negateMatches map[*SubstringQuery][]*candidateMatch
}

func mergeCandidates(iters []*docIterator, stats *Stats) []mergedCandidateMatch {
	var cands [][]*candidateMatch
	for _, i := range iters {
		iterCands := i.next()
		if len(iterCands) > 0 {
			cands = append(cands, iterCands)
		}
		stats.NgramMatches += len(iterCands)
	}

	var merged []mergedCandidateMatch

	for len(cands) > 0 {
		var newCands [][]*candidateMatch
		var nextDoc uint32
		nextDoc = maxUInt32
		for _, ms := range cands {
			if ms[0].file < nextDoc {
				nextDoc = ms[0].file
			}
		}

		newCands = nil
		mc := mergedCandidateMatch{
			fileID:  nextDoc,
			matches: map[*SubstringQuery][]*candidateMatch{},
		}
		for _, ms := range cands {
			var sqMatches []*candidateMatch
			for len(ms) > 0 && ms[0].file == nextDoc {
				sqMatches = append(sqMatches, ms[0])
				ms = ms[1:]
			}
			if len(sqMatches) > 0 {
				mc.matches[sqMatches[0].query] = sqMatches
			}
			if len(ms) > 0 {
				newCands = append(newCands, ms[:])
			}
		}
		cands = newCands
		merged = append(merged, mc)
	}

	return merged
}

type dataProvider interface {
	caseBits() []byte
	contentBits() []byte
	newlines() []uint32
}

type contentProvider struct {
	reader *reader
	id     *indexData
	idx    uint32
	stats  *Stats
	cb     []byte
	data   []byte
	nl     []uint32

	matchesByQuery map[*SubstringQuery][]*candidateMatch
}

func (p *contentProvider) caseMatches(m *candidateMatch) bool {
	var cb []byte
	if m.query.FileName {
		cb = p.id.fileNameCaseBits[p.id.fileNameCaseBitsIndex[p.idx]:p.id.fileNameCaseBitsIndex[p.idx+1]]
	} else {
		if p.cb == nil {
			p.cb = p.reader.readCaseBits(p.id, p.idx)
		}
		cb = p.cb
	}
	return m.caseMatches(cb)
}

func (p *contentProvider) matchContent(m *candidateMatch) bool {
	var content []byte
	if m.query.FileName {
		content = p.id.fileNameContent[p.id.fileNameIndex[p.idx]:p.id.fileNameIndex[p.idx+1]]
	} else {
		if p.data == nil {
			p.data = p.reader.readContents(p.id, p.idx)
			p.stats.FilesLoaded++
		}
		content = p.data
	}
	return m.matchContent(content)
}

func (p *contentProvider) fillMatch(m *candidateMatch) Match {
	if m.query.FileName {
		return Match{
			Offset:      m.offset,
			Line:        string(p.id.fileNameContent[p.id.fileNameIndex[p.idx]:p.id.fileNameIndex[p.idx+1]]),
			LineOff:     int(m.offset),
			MatchLength: len(m.substrBytes),
			FileName:    true,
		}
	}

	if p.nl == nil {
		p.nl = p.reader.readNewlines(p.id, p.idx)
	}
	if p.data == nil {
		p.data = p.reader.readContents(p.id, p.idx)
		p.stats.FilesLoaded++
	}
	if p.cb == nil {
		p.cb = p.reader.readCaseBits(p.id, p.idx)
	}
	num, off, data := m.line(p.nl, p.data, p.cb)
	return Match{
		Offset:      m.offset,
		Line:        string(data),
		LineNum:     num,
		LineOff:     off,
		MatchLength: len(m.substrBytes),
	}
}

type matchOffsetSlice []Match

func (m matchOffsetSlice) Len() int           { return len(m) }
func (m matchOffsetSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m matchOffsetSlice) Less(i, j int) bool { return m[i].Offset <= m[j].Offset }

func sortMatches(ms []Match) {
	sort.Sort(matchOffsetSlice(ms))
}
