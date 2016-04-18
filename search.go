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
			Line:        p.id.fileNameContent[p.id.fileNameIndex[p.idx]:p.id.fileNameIndex[p.idx+1]],
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

	finalMatch := Match{
		Offset:      m.offset,
		Line:        data,
		LineNum:     num,
		LineOff:     off,
		MatchLength: len(m.substrBytes),
	}
	finalMatch.Score = matchScore(&finalMatch)

	return finalMatch
}

const (
	// TODO - how to anchor this relative to rank?
	scorePartialWordMatch = 5000.0
	scoreWordMatch        = 50000.0
)

func matchScore(m *Match) float64 {
	startBoundary := m.LineOff == 0 || byteClass(m.Line[m.LineOff-1]) != byteClass(m.Line[m.LineOff])

	end := int(m.LineOff) + m.MatchLength
	endBoundary := end == len(m.Line) || byteClass(m.Line[end-1]) != byteClass(m.Line[end])

	if startBoundary && endBoundary {
		return scoreWordMatch
	} else if startBoundary || endBoundary {
		return scorePartialWordMatch
	}

	return 0.0
}

type matchScoreSlice []Match

func (m matchScoreSlice) Len() int           { return len(m) }
func (m matchScoreSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m matchScoreSlice) Less(i, j int) bool { return m[i].Score > m[j].Score }

type fileMatchSlice []FileMatch

func (m fileMatchSlice) Len() int           { return len(m) }
func (m fileMatchSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m fileMatchSlice) Less(i, j int) bool { return m[i].Score > m[j].Score }

func sortMatchesByScore(ms []Match) {
	sort.Sort(matchScoreSlice(ms))
}

func sortFilesByScore(ms []FileMatch) {
	sort.Sort(fileMatchSlice(ms))
}
