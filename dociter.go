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
	"bytes"
	"fmt"
	"log"
	"sort"

	"github.com/google/zoekt/query"
)

var _ = log.Println

// candidateMatch is a candidate match for a substring.
type candidateMatch struct {
	caseSensitive bool
	fileName      bool

	substrBytes   []byte
	substrLowered []byte

	file    uint32
	offset  uint32
	matchSz uint32
}

func (m *candidateMatch) String() string {
	return fmt.Sprintf("%d:%d", m.file, m.offset)
}

func (m *candidateMatch) matchContent(content []byte) bool {
	if m.caseSensitive {
		comp := bytes.Compare(content[m.offset:m.offset+uint32(m.matchSz)], m.substrBytes) == 0
		return comp
	} else {
		// TODO(hanwen): do this without generating garbage.
		l := toLower(content[m.offset : m.offset+uint32(m.matchSz)])
		return bytes.Compare(l, m.substrLowered) == 0
	}
}

func (m *candidateMatch) line(newlines []uint32, fileSize uint32) (lineNum, lineStart, lineEnd int) {
	idx := sort.Search(len(newlines), func(n int) bool {
		return newlines[n] >= m.offset
	})

	end := int(fileSize)
	if idx < len(newlines) {
		end = int(newlines[idx])
	}

	start := 0
	if idx > 0 {
		start = int(newlines[idx-1] + 1)
	}

	return idx + 1, start, end
}

type docIterator interface {
	// TODO - reconsider this name? Or don't get all the
	// candidateMatch in one go?
	next() []*candidateMatch
	coversContent() bool
}

type ngramDocIterator struct {
	query *query.Substring

	leftPad  uint32
	rightPad uint32
	distance uint32
	first    []uint32
	last     []uint32

	fileIdx int
	ends    []uint32

	// The ngram matches cover the pattern, so no need to check
	// contents.
	_coversContent bool
}

func (s *ngramDocIterator) coversContent() bool {
	return s._coversContent
}

func (s *ngramDocIterator) next() []*candidateMatch {
	patBytes := []byte(s.query.Pattern)
	lowerPatBytes := toLower(patBytes)

	var candidates []*candidateMatch
	for {
		if len(s.first) == 0 || len(s.last) == 0 {
			break
		}
		p1 := s.first[0]
		p2 := s.last[0]

		for s.fileIdx < len(s.ends) && s.ends[s.fileIdx] <= p1 {
			s.fileIdx++
		}

		if p1+s.distance < p2 {
			s.first = s.first[1:]
		} else if p1+s.distance > p2 {
			s.last = s.last[1:]
		} else {
			s.first = s.first[1:]
			s.last = s.last[1:]

			var fileStart uint32
			if s.fileIdx > 0 {
				fileStart = s.ends[s.fileIdx-1]
			}
			if p1 < s.leftPad+fileStart || p1+s.distance+ngramSize+s.rightPad > s.ends[s.fileIdx] {
				continue
			}

			candidates = append(candidates,
				&candidateMatch{
					caseSensitive: s.query.CaseSensitive,
					fileName:      s.query.FileName,
					substrBytes:   patBytes,
					substrLowered: lowerPatBytes,
					matchSz:       uint32(len(lowerPatBytes)),
					file:          uint32(s.fileIdx),
					offset:        p1 - fileStart - s.leftPad,
				})
		}
	}

	return candidates
}
