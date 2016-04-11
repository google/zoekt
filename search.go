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

package codesearch

import (
	"fmt"
	"log"
	"sort"
)

var _ = log.Println

// All the matches for a given file.
type mergedCandidateMatch struct {
	fileID        uint32
	matches       map[*SubstringQuery][]candidateMatch
	negateMatches map[*SubstringQuery][]candidateMatch
}

func mergeCandidates(iters []*docIterator, stats *Stats) []mergedCandidateMatch {
	var cands [][]candidateMatch
	var negateCands [][]candidateMatch
	for _, i := range iters {
		iterCands := i.next()
		if i.query.Negate {
			negateCands = append(negateCands, iterCands)
		} else {
			cands = append(cands, iterCands)
		}
		stats.NgramMatches += len(iterCands)
	}

	var merged []mergedCandidateMatch
	var nextDoc uint32

done:
	for {
		found := true
		var newCands [][]candidateMatch
		for _, ms := range cands {
			for len(ms) > 0 && ms[0].file < nextDoc {
				ms = ms[1:]
			}
			if len(ms) == 0 {
				break done
			}
			if ms[0].file > nextDoc {
				nextDoc = ms[0].file
				found = false
			}
			newCands = append(newCands, ms)
		}
		cands = newCands
		if !found {
			continue
		}

		newCands = nil
		for _, ms := range negateCands {
			for len(ms) > 0 && ms[0].file < nextDoc {
				ms = ms[1:]
			}
			if len(ms) > 0 {
				newCands = append(newCands, ms)
			}
		}
		negateCands = newCands

		newCands = nil
		mc := mergedCandidateMatch{
			fileID:        nextDoc,
			matches:       map[*SubstringQuery][]candidateMatch{},
			negateMatches: map[*SubstringQuery][]candidateMatch{},
		}
		for _, ms := range cands {
			var sqMatches []candidateMatch
			for len(ms) > 0 && ms[0].file == nextDoc {
				sqMatches = append(sqMatches, ms[0])
				ms = ms[1:]
			}

			mc.matches[sqMatches[0].query] = sqMatches
			newCands = append(newCands, ms[:])
		}
		cands = newCands

		newCands = nil
		for _, ms := range negateCands {
			var sqMatches []candidateMatch
			for len(ms) > 0 && ms[0].file == nextDoc {
				sqMatches = append(sqMatches, ms[0])
				ms = ms[1:]
			}
			if len(sqMatches) > 0 {
				mc.negateMatches[sqMatches[0].query] = sqMatches
			}

			if len(ms) > 0 {
				newCands = append(newCands, ms)
			}
		}
		negateCands = newCands

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

func (s *searcher) andSearch(andQ *andQuery) (*SearchResult, error) {
	foundPositive := false
	for _, atom := range andQ.atoms {
		if !atom.Negate {
			foundPositive = true
			break
		}
	}
	if !foundPositive {
		return nil, fmt.Errorf("must have a positive query atom in AND query.")
	}

	var res SearchResult
	var caseSensitive bool
	var iters []*docIterator

	for _, atom := range andQ.atoms {
		caseSensitive = caseSensitive || atom.CaseSensitive

		// TODO - postingsCache
		i, err := s.reader.getDocIterator(s.indexData, atom)
		if err != nil {
			return nil, err
		}
		iters = append(iters, i)
	}

	// TODO merge mergeCandidates and following loop.
	cands := mergeCandidates(iters, &res.Stats)

	cp := contentProvider{
		idx: uint32(0xFFFFFFFF),
	}

nextFileMatch:
	for _, c := range cands {
		if c.fileID != cp.idx {
			cp = contentProvider{
				reader: &s.reader,
				id:     s.indexData,
				idx:    c.fileID,
				stats:  &res.Stats,
			}
		}

		if caseSensitive {
			trimmed := map[*SubstringQuery][]candidateMatch{}
			for q, req := range c.matches {
				matching := []candidateMatch{}
				for _, m := range req {
					if cp.caseMatches(&m) {
						matching = append(matching, m)
					}
				}
				if len(matching) == 0 {
					continue nextFileMatch
				}
				trimmed[q] = matching
			}

			c.matches = trimmed

			trimmed = map[*SubstringQuery][]candidateMatch{}
			for q, req := range c.negateMatches {
				matching := []candidateMatch{}
				for _, m := range req {
					if cp.caseMatches(&m) {
						matching = append(matching, m)
					}
				}
				trimmed[q] = matching
			}

			c.negateMatches = trimmed
		}

		trimmed := map[*SubstringQuery][]candidateMatch{}
		for q, req := range c.matches {
			matching := []candidateMatch{}
			for _, m := range req {
				if cp.matchContent(&m) {
					matching = append(matching, m)
				}
			}
			if len(matching) == 0 {
				continue nextFileMatch
			}
			trimmed[q] = matching
		}
		c.matches = trimmed

		for _, neg := range c.negateMatches {
			for _, m := range neg {
				if cp.matchContent(&m) {
					continue nextFileMatch
				}
			}
		}

		fMatch := FileMatch{
			Name: s.indexData.fileNames[c.fileID],
			Rank: int(c.fileID),
		}

		// If we have content matches, drop the filename match.
		foundContentMatch := false
		for _, req := range c.matches {
			for _, m := range req {
				if !m.query.FileName {
					foundContentMatch = true
				}
			}
		}
		for _, req := range c.matches {
			for _, m := range req {
				if !m.query.FileName || !foundContentMatch {
					fMatch.Matches = append(fMatch.Matches, cp.fillMatch(&m))
				}
			}
		}

		sortMatches(fMatch.Matches)
		res.Files = append(res.Files, fMatch)
		res.Stats.MatchCount += len(fMatch.Matches)
		res.Stats.FileCount++
	}

	return &res, nil
}

type matchOffsetSlice []Match

func (m matchOffsetSlice) Len() int           { return len(m) }
func (m matchOffsetSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m matchOffsetSlice) Less(i, j int) bool { return m[i].Offset <= m[j].Offset }

func sortMatches(ms []Match) {
	sort.Sort(matchOffsetSlice(ms))
}
