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
	"log"
	"regexp"

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
	runeOffsets      []uint32

	// offsets of file contents. Includes end of last file.
	boundaries []uint32

	fileEnds []uint32

	fileNameContent     []byte
	fileNameIndex       []uint32
	fileNameNgrams      map[ngram][]uint32
	fileNameRuneOffsets []uint32

	fileBranchMasks []uint32

	// mask (power of 2) => name
	branchNames map[uint]string

	// name => mask (power of 2)
	branchIDs map[string]uint

	metaData     IndexMetadata
	repoMetaData Repository

	subRepos     []uint32
	subRepoPaths []string

	repoListEntry RepoListEntry
}

func (d *indexData) String() string {
	return fmt.Sprintf("shard(%s)", d.file.Name())
}

func (d *indexData) memoryUse() int {
	sz := 0
	for _, a := range [][]uint32{
		d.newlinesIndex, d.docSectionsIndex,
		d.fileEnds, d.fileNameIndex, d.fileBranchMasks,
		d.runeOffsets, d.fileNameRuneOffsets,
	} {
		sz += 4 * len(a)
	}
	sz += 12 * len(d.ngrams)
	for _, v := range d.fileNameNgrams {
		sz += 4*len(v) + 4
	}
	return sz
}

func (data *indexData) getDocIterator(q query.Q) (docIterator, error) {
	switch s := q.(type) {
	case *query.Substring:
		if len(s.Pattern) < ngramSize {
			if !s.FileName {
				return nil, fmt.Errorf("pattern %q less than %d bytes", s.Pattern, ngramSize)
			}

			return data.getBruteForceFileNameDocIterator(s), nil
		}

		return data.getNgramDocIterator(s)

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

	if len(data.fileNameIndex) == 0 {
		return &bruteForceIter{cands}
	}

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
			runeOffset:    0,
			byteOffset:    0,
			byteMatchSz:   uint32(len(name)),
		})
	}
	return &bruteForceIter{cands}
}

func (data *indexData) getBruteForceFileNameDocIterator(query *query.Substring) docIterator {
	quoted := regexp.QuoteMeta(query.Pattern)
	if !query.CaseSensitive {
		quoted = "(?i)" + quoted
	}

	lowerPat := toLower([]byte(query.Pattern))

	re := regexp.MustCompile(quoted)

	fileID := 0
	startName := data.fileNameIndex[fileID]
	endName := data.fileNameIndex[fileID+1]

	var cands []*candidateMatch
	for _, match := range re.FindAllIndex(data.fileNameContent, -1) {
		start := uint32(match[0])
		end := uint32(match[1])

		for endName < end {
			fileID++
			startName = data.fileNameIndex[fileID]
			endName = data.fileNameIndex[fileID+1]
		}

		if start < startName {
			// straddles a filename boundary.
			continue
		}

		cands = append(cands, &candidateMatch{
			caseSensitive: query.CaseSensitive,
			fileName:      true,
			substrBytes:   []byte(query.Pattern),
			substrLowered: lowerPat,
			file:          uint32(fileID),
			// TODO - should also populate runeOffset?
			byteOffset:  uint32(start - startName),
			byteMatchSz: uint32(end - start),
		})
	}

	return &bruteForceIter{cands}
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

func (data *indexData) ngramFrequency(ng ngram, filename bool) uint32 {
	if filename {
		return uint32(len(data.fileNameNgrams[ng]))
	}

	return data.ngrams[ng].sz
}

func (data *indexData) getNgramDocIterator(query *query.Substring) (docIterator, error) {
	input := &ngramDocIterator{
		query: query,
	}

	if query.FileName {
		input.ends = data.fileNameIndex[1:]
	} else {
		input.ends = data.fileEnds
	}

	str := query.Pattern

	// Find the 2 least common ngrams from the string.
	ngramOffs := splitNGrams([]byte(query.Pattern))
	var frequencies []uint32
	for _, o := range ngramOffs {
		var freq uint32
		if query.CaseSensitive {
			freq = data.ngramFrequency(o.ngram, query.FileName)
		} else {
			for _, v := range generateCaseNgrams(o.ngram) {
				freq += data.ngramFrequency(v, query.FileName)
			}
		}

		if freq == 0 {
			return input, nil
		}

		frequencies = append(frequencies, freq)
	}

	firstI := minarg(frequencies)
	frequencies[firstI] = maxUInt32
	lastI := minarg(frequencies)
	if firstI > lastI {
		lastI, firstI = firstI, lastI
	}

	firstNG := ngramOffs[firstI].ngram
	lastNG := ngramOffs[lastI].ngram
	input.distance = lastI - firstI
	input.leftPad = firstI
	input.rightPad = uint32(len(str)-ngramSize) - lastI

	postings, err := data.readPostings(firstNG, query.CaseSensitive, query.FileName)
	if err != nil {
		return nil, err
	}

	input.first = postings
	if firstI != lastI {
		postings, err = data.readPostings(lastNG, query.CaseSensitive, query.FileName)
		if err != nil {
			return nil, err
		}
		input.last = postings
	} else {
		input.last = input.first
	}

	input.ng1 = firstNG
	input.ng2 = lastNG
	if lastI-firstI <= ngramSize && input.leftPad == 0 && input.rightPad == 0 {
		input._coversContent = true
	}
	return input, nil
}

func (d *indexData) readPostings(ng ngram, caseSensitive, fileName bool) ([]uint32, error) {
	variants := []ngram{ng}
	if !caseSensitive {
		variants = generateCaseNgrams(ng)
	}

	// TODO - generate less garbage.
	postings := make([][]uint32, 0, len(variants))
	for _, v := range variants {
		if fileName {
			postings = append(postings, d.fileNameNgrams[v])
			continue
		}

		sec := d.ngrams[v]
		blob, err := d.readSectionBlob(sec)
		if err != nil {
			return nil, err
		}
		ps := fromDeltas(blob, nil)
		if len(ps) > 0 {
			postings = append(postings, ps)
		}
	}

	result := mergeUint32(postings)
	return result, nil
}

func mergeUint32(in [][]uint32) []uint32 {
	sz := 0
	for _, i := range in {
		sz += len(i)
	}
	out := make([]uint32, 0, sz)
	for len(in) > 0 {
		minVal := uint32(maxUInt32)
		for _, n := range in {
			if len(n) > 0 && n[0] < minVal {
				minVal = n[0]
			}
		}

		next := in[:0]
		for _, n := range in {
			if len(n) == 0 {
				continue
			}
			if n[0] == minVal {
				out = append(out, minVal)
				n = n[1:]
			}
			if len(n) > 0 {
				next = append(next, n)
			}
		}

		in = next
	}

	return out
}

func (d *indexData) fileName(i uint32) []byte {
	return d.fileNameContent[d.fileNameIndex[i]:d.fileNameIndex[i+1]]
}

func (s *indexData) Close() {
	s.file.Close()
}
