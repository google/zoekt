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
	"strings"

	"github.com/google/zoekt/query"
)

var _ = log.Println

// indexData holds the pattern-independent data that we have to have
// in memory to search. Most of the memory is taken up by the ngram =>
// offset index.
type indexData struct {
	file IndexFile

	ngrams map[ngram]simpleSection

	newlinesIndex    []uint32
	docSectionsIndex []uint32
	caseBitsIndex    []uint32

	// offsets of file contents. Includes end of last file.
	boundaries []uint32

	fileEnds []uint32

	fileNameContent       []byte
	fileNameCaseBits      []byte
	fileNameCaseBitsIndex []uint32
	fileNameIndex         []uint32
	fileNameNgrams        map[ngram][]uint32

	fileBranchMasks []uint32
	branchNames     map[uint]string
	branchIDs       map[string]uint

	unaryData indexUnaryData
}

type indexUnaryData struct {
	RepoName           string
	RepoURL            string
	RepoLineFragment   string
	IndexFormatVersion int
}

func (d *indexData) memoryUse() int {
	sz := 0
	for _, a := range [][]uint32{
		d.newlinesIndex, d.docSectionsIndex, d.caseBitsIndex,
		d.fileEnds, d.fileNameCaseBitsIndex, d.fileNameIndex, d.fileBranchMasks,
	} {
		sz += 4 * len(a)
	}
	sz += 12 * len(d.ngrams)
	for _, v := range d.fileNameNgrams {
		sz += 4*len(v) + 4
	}
	return sz
}

func (d *indexData) Stats() (*RepoStats, error) {
	last := d.boundaries[len(d.boundaries)-1]
	lastFN := d.fileNameIndex[len(d.fileNameIndex)-1]
	return &RepoStats{
		Repos:        []string{d.unaryData.RepoName},
		IndexBytes:   int64(d.memoryUse()),
		ContentBytes: int64(int(last) + int(lastFN)),
		Documents:    len(d.newlinesIndex) - 1,
	}, nil
}

func (data *indexData) getDocIterator(q query.Q) (docIterator, error) {
	switch s := q.(type) {

	case *query.Substring:
		if s.FileName {
			return data.getFileNameDocIterator(s), nil
		}
		if len(s.Pattern) < ngramSize {
			return nil, fmt.Errorf("pattern %q less than %d bytes", s.Pattern, ngramSize)
		}

		return data.getContentDocIterator(s)
	case *query.Const:
		if s.Value {
			return data.matchAllDocIterator(), nil
		}
		return &bruteForceIter{}, nil
	}

	log.Panicf("type %T", q)
	return nil, nil
}

type bruteForceIter struct {
	cands []*candidateMatch
}

func (i *bruteForceIter) next() []*candidateMatch {
	return i.cands
}
func (i *bruteForceIter) coversContent() bool {
	return true
}

func (data *indexData) matchAllDocIterator() docIterator {
	var cands []*candidateMatch
	var last uint32
	for i, off := range data.fileNameIndex[1:] {
		name := data.fileNameContent[last:off]
		last = off
		cands = append(cands, &candidateMatch{
			caseSensitive: false,
			fileName:      true,
			substrBytes:   name,
			substrLowered: name,
			file:          uint32(i),
			offset:        uint32(0),
			matchSz:       uint32(len(name)),
		})
	}
	return &bruteForceIter{cands}
}

func (data *indexData) getBruteForceFileNameDocIterator(query *query.Substring) docIterator {
	lowerStr := toLower([]byte(query.Pattern))
	last := uint32(0)

	var cands []*candidateMatch
	for i, off := range data.fileNameIndex[1:] {
		name := data.fileNameContent[last:off]
		last = off
		idx := bytes.Index(name, lowerStr)
		if idx == -1 {
			continue
		}
		cands = append(cands, &candidateMatch{
			caseSensitive: query.CaseSensitive,
			fileName:      true,
			substrBytes:   []byte(query.Pattern),
			substrLowered: lowerStr,
			file:          uint32(i),
			offset:        uint32(idx),
			matchSz:       uint32(len(lowerStr)),
		})
	}

	return &bruteForceIter{cands}
}

func (data *indexData) getFileNameDocIterator(query *query.Substring) docIterator {
	if len(query.Pattern) < ngramSize {
		return data.getBruteForceFileNameDocIterator(query)
	}
	str := strings.ToLower(query.Pattern) // TODO - UTF-8
	di := &ngramDocIterator{
		query:    query,
		distance: uint32(len(str)) - ngramSize,
		ends:     data.fileNameIndex[1:],
		first:    data.fileNameNgrams[stringToNGram(str[:ngramSize])],
		last:     data.fileNameNgrams[stringToNGram(str[len(str)-ngramSize:])],
	}

	return di
}

const maxUInt32 = 0xffffffff

func minarg(xs []uint32) uint32 {
	m := uint32(maxUInt32)
	j := len(xs)
	for i, x := range xs {
		if x <= m {
			m = x
			j = i
		}
	}
	return uint32(j)
}

func (data *indexData) getContentDocIterator(query *query.Substring) (docIterator, error) {
	str := strings.ToLower(query.Pattern) // TODO - UTF-8
	input := &ngramDocIterator{
		query: query,
		ends:  data.fileEnds,
	}

	// Find the 2 least common ngrams from the string.
	frequencies := make([]uint32, len(str)-ngramSize+1)
	for i := range frequencies {
		frequencies[i] = data.ngrams[stringToNGram(str[i:i+ngramSize])].sz
		if frequencies[i] == 0 {
			return input, nil
		}
	}

	firstI := minarg(frequencies)
	frequencies[firstI] = maxUInt32
	lastI := minarg(frequencies)
	if firstI > lastI {
		lastI, firstI = firstI, lastI
	}

	first := data.ngrams[stringToNGram(str[firstI:firstI+ngramSize])]
	last := data.ngrams[stringToNGram(str[lastI:lastI+ngramSize])]
	input.distance = lastI - firstI
	input.leftPad = firstI
	input.rightPad = uint32(len(str)-ngramSize) - lastI

	blob, err := data.readSectionBlob(first)
	if err != nil {
		return nil, err
	}
	input.first = fromDeltas(blob, nil)

	if firstI != lastI {
		blob, err = data.readSectionBlob(last)
		if err != nil {
			return nil, err
		}
		input.last = fromDeltas(blob, nil)
	} else {
		input.last = input.first
	}

	if lastI-firstI <= ngramSize && input.leftPad == 0 && input.rightPad == 0 {
		input._coversContent = true
	}
	return input, nil
}

func (d *indexData) fileName(i uint32) []byte {
	data := d.fileNameContent[d.fileNameIndex[i]:d.fileNameIndex[i+1]]
	cb := d.fileNameCaseBits[d.fileNameCaseBitsIndex[i]:d.fileNameCaseBitsIndex[i+1]]

	return toOriginal(make([]byte, len(data)+8), data, cb, 0, len(data))
}

func (s *indexData) Close() {
	s.file.Close()
}
