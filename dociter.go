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
	"encoding/binary"
	"fmt"
	"sort"
)

// candidateMatch is a candidate match for a substring.
type candidateMatch struct {
	caseSensitive bool
	fileName      bool

	substrBytes   []byte
	substrLowered []byte

	file uint32

	// Offsets are relative to the start of the filename or file contents.
	runeOffset  uint32
	byteOffset  uint32
	byteMatchSz uint32
}

func (m *candidateMatch) String() string {
	return fmt.Sprintf("%d:%d", m.file, m.runeOffset)
}

func (m *candidateMatch) matchContent(content []byte) bool {
	if m.caseSensitive {
		comp := bytes.Compare(m.substrBytes, content[m.byteOffset:m.byteOffset+uint32(len(m.substrBytes))]) == 0
		return comp
	} else {
		// It is tempting to try a simple ASCII based
		// comparison if possible, but we need more
		// information. Simple ASCII chars have unicode upper
		// case variants (the ASCII 'k' has the Kelvin symbol
		// as upper case variant). We can only degrade to
		// ASCII if we are sure that both the corpus and the
		// query is ASCII only
		return caseFoldingEqualsRunes(m.substrLowered, content[m.byteOffset:])
	}
}

// line returns the line holding the match. If the match starts with
// the newline ending line M, we return M.  The line is characterized
// by its linenumber (base-1, byte index of line start, byte index of
// line end).  The line end is the index of a newline, or the filesize
// (if matching the last line of the file.)
func (m *candidateMatch) line(newlines []uint32, fileSize uint32) (lineNum, lineStart, lineEnd int) {
	idx := sort.Search(len(newlines), func(n int) bool {
		return newlines[n] >= m.byteOffset
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

type hitIterator interface {
	// Return the first hit, or maxUInt32 if none.
	first() uint32

	// Skip until past limit.
	next(limit uint32)

	// Return how many bytes were read.
	bytesRead() uint32
}

type distanceHitIterator struct {
	started  bool
	distance uint32
	i1       hitIterator
	i2       hitIterator
}

func (i *distanceHitIterator) String() string {
	return fmt.Sprintf("dist(%d, %v, %v)", i.distance, i.i1, i.i2)
}

func (i *distanceHitIterator) findNext() {
	for {
		var p1, p2 uint32
		p1 = i.i1.first()
		p2 = i.i2.first()
		if p1 == maxUInt32 || p2 == maxUInt32 {
			break
		}

		if p1+i.distance < p2 {
			i.i1.next(p2 - i.distance - 1)
		} else if p1+i.distance > p2 {
			i.i2.next(p1 + i.distance - 1)
		} else {
			break
		}
	}
}

func (i *distanceHitIterator) first() uint32 {
	if !i.started {
		i.findNext()
		i.started = true
	}
	return i.i1.first()
}

func (i *distanceHitIterator) bytesRead() uint32 {
	return i.i1.bytesRead() + i.i2.bytesRead()
}

func (i *distanceHitIterator) next(limit uint32) {
	i.i1.next(limit)
	i.i2.next(limit + i.distance)
	i.findNext()
}

func (d *indexData) newDistanceTrigramIter(ng1, ng2 ngram, dist uint32, caseSensitive, fileName bool) (hitIterator, error) {
	if dist == 0 {
		return nil, fmt.Errorf("d == 0")
	}

	i1, err := d.trigramHitIterator(ng1, caseSensitive, fileName)
	if err != nil {
		return nil, err
	}
	i2, err := d.trigramHitIterator(ng2, caseSensitive, fileName)
	if err != nil {
		return nil, err
	}
	return &distanceHitIterator{
		i1:       i1,
		i2:       i2,
		distance: dist,
	}, nil
}

func (d *indexData) trigramHitIterator(ng ngram, caseSensitive, fileName bool) (hitIterator, error) {
	variants := []ngram{ng}
	if !caseSensitive {
		variants = generateCaseNgrams(ng)
	}

	iters := make([]hitIterator, 0, len(variants))
	for _, v := range variants {
		if fileName {
			blob := d.fileNameNgrams[v]
			if len(blob) > 0 {
				iters = append(iters, &inMemoryIterator{
					d.fileNameNgrams[v],
					v,
				})
			}
			continue
		}

		sec := d.ngrams[v]
		blob, err := d.readSectionBlob(sec)
		if err != nil {
			return nil, err
		}
		if len(blob) > 0 {
			iters = append(iters, newCompressedPostingIterator(blob, v))
		}
	}

	if len(iters) == 1 {
		return iters[0], nil
	}
	return &mergingIterator{
		iters: iters,
	}, nil
}

type inMemoryIterator struct {
	postings []uint32
	what     ngram
}

func (i *inMemoryIterator) String() string {
	return fmt.Sprintf("mem(%s):%v", i.what, i.postings)
}

func (i *inMemoryIterator) first() uint32 {
	if len(i.postings) > 0 {
		return i.postings[0]
	}
	return maxUInt32
}

func (i *inMemoryIterator) bytesRead() uint32 {
	return 0
}

func (i *inMemoryIterator) next(limit uint32) {
	for len(i.postings) > 0 && i.postings[0] <= limit {
		i.postings = i.postings[1:]
	}
}

type compressedPostingIterator struct {
	blob, orig []byte
	_first     uint32
	what       ngram
}

func newCompressedPostingIterator(b []byte, w ngram) *compressedPostingIterator {
	d, sz := binary.Uvarint(b)
	return &compressedPostingIterator{
		_first: uint32(d),
		blob:   b[sz:],
		orig:   b,
		what:   w,
	}
}

func (i *compressedPostingIterator) String() string {
	return fmt.Sprintf("compressed(%s, %d, [%d bytes])", i.what, i._first, len(i.blob))
}

func (i *compressedPostingIterator) first() uint32 {
	return i._first
}

func (i *compressedPostingIterator) next(limit uint32) {
	if i._first <= limit && len(i.blob) == 0 {
		i._first = maxUInt32
		return
	}

	for i._first <= limit {
		delta, sz := binary.Uvarint(i.blob)
		i._first += uint32(delta)
		i.blob = i.blob[sz:]
	}
}

func (i *compressedPostingIterator) bytesRead() uint32 {
	return uint32(len(i.orig) - len(i.blob))
}

type mergingIterator struct {
	iters []hitIterator
}

func (i *mergingIterator) String() string {
	return fmt.Sprintf("merge:%v", i.iters)
}

func (i *mergingIterator) bytesRead() uint32 {
	var r uint32
	for _, j := range i.iters {
		r += j.bytesRead()
	}
	return r
}

func (i *mergingIterator) first() uint32 {
	r := uint32(maxUInt32)
	for _, j := range i.iters {
		f := j.first()
		if f < r {
			r = f
		}
	}

	return r
}

func (i *mergingIterator) next(limit uint32) {
	for _, j := range i.iters {
		j.next(limit)
	}
}

type ngramDocIterator struct {
	leftPad  uint32
	rightPad uint32

	iter hitIterator
	ends []uint32
}

func (i *ngramDocIterator) bytesRead() uint32 {
	return i.iter.bytesRead()
}

func (i *ngramDocIterator) candidates() []*candidateMatch {
	var candidates []*candidateMatch
	fileIdx := 0
	for {
		p1 := i.iter.first()
		if p1 == maxUInt32 {
			break
		}
		i.iter.next(p1)

		for fileIdx < len(i.ends) && i.ends[fileIdx] <= p1 {
			fileIdx++
		}

		var fileStart uint32
		if fileIdx > 0 {
			fileStart = i.ends[fileIdx-1]
		}

		if p1 < i.leftPad+fileStart || p1+i.rightPad > i.ends[fileIdx] {
			continue
		}

		candidates = append(candidates, &candidateMatch{
			file:       uint32(fileIdx),
			runeOffset: p1 - fileStart - i.leftPad,
		})
	}
	return candidates
}
