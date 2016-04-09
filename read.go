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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var _ = log.Println

type reader struct {
	r   ReadSeekCloser
	err error
}

func (r *reader) readSection(s *section) {
	s.off = r.U32()
	s.sz = r.U32()
}

func (r *reader) U32() uint32 {
	if r.err != nil {
		return 0
	}
	var b [4]byte
	_, r.err = r.r.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}

func (r *reader) readTOC(toc *indexTOC) {
	if r.err != nil {
		return
	}

	r.r.Seek(-8, 2)
	var tocSection section
	r.readSection(&tocSection)
	_, r.err = r.r.Seek(int64(tocSection.off), 0)
	for _, s := range toc.sections() {
		r.readSection(s)
	}
}

type ngramText []byte

func (t ngramText) get(i int) []byte {
	return t[i*NGRAM : (i+1)*NGRAM]
}
func (t ngramText) length() int {
	return len(t) / NGRAM
}

// indexData holds the pattern independent data that we have to have
// in memory to search.
type indexData struct {
	ngramText        ngramText
	ngramFrequencies []uint32
	postingIndex     []uint32
	newlinesIndex    []uint32
	caseBitsIndex    []uint32

	// offsets of file contents. Includes end of last file.
	boundaries []uint32

	fileEnds  []uint32
	fileNames []string
}

func (d *indexData) findNgramIdx(ngram string) (uint32, bool) {
	asBytes := []byte(ngram)
	idx := sort.Search(d.ngramText.length(), func(j int) bool {
		return bytes.Compare(d.ngramText.get(j), asBytes) >= 0
	})
	if idx == d.ngramText.length() {
		return 0, false
	}
	if bytes.Compare(asBytes, d.ngramText.get(idx)) != 0 {
		return 0, false
	}
	return uint32(idx), true
}

func (r *reader) readSectionBlob(sec section) []byte {
	d := make([]byte, sec.sz)
	r.r.Seek(int64(sec.off), 0)
	_, r.err = r.r.Read(d)
	return d
}

func (r *reader) readSectionU32(sec section) []uint32 {
	blob := r.readSectionBlob(sec)
	arr := make([]uint32, 0, len(blob)/4)
	for len(blob) > 0 {
		arr = append(arr, binary.BigEndian.Uint32(blob))
		blob = blob[4:]
	}
	return arr
}

func (r *reader) readIndexData(toc *indexTOC) *indexData {
	if r.err != nil {
		return nil
	}

	textContent := r.readSectionBlob(toc.ngramText)
	d := indexData{
		ngramText:        ngramText(textContent),
		ngramFrequencies: r.readSectionU32(toc.ngramFrequencies),
		postingIndex:     r.readSectionU32(toc.postingsIndex),
		caseBitsIndex:    r.readSectionU32(toc.caseBitsIndex),
		boundaries:       r.readSectionU32(toc.contentBoundaries),
		newlinesIndex:    r.readSectionU32(toc.newlinesIndex),
	}

	d.boundaries = append(d.boundaries, d.boundaries[0]+toc.contents.sz)
	d.postingIndex = append(d.postingIndex, toc.postings.off+toc.postings.sz)
	d.fileEnds = make([]uint32, 0, len(d.boundaries))
	d.newlinesIndex = append(d.newlinesIndex, toc.newlines.off+toc.newlines.sz)
	d.caseBitsIndex = append(d.caseBitsIndex, toc.caseBits.off+toc.caseBits.sz)
	for _, b := range d.boundaries[1:] {
		d.fileEnds = append(d.fileEnds, b-d.boundaries[0])
	}

	fnBlob := r.readSectionBlob(toc.names)
	fnIndex := r.readSectionU32(toc.nameIndex)
	for i, n := range fnIndex {
		end := toc.names.sz
		if i < len(fnIndex)-1 {
			end = fnIndex[i+1] - fnIndex[0]
		}
		n -= fnIndex[0]
		d.fileNames = append(d.fileNames, string(fnBlob[n:end]))
	}
	return &d
}

func (r *reader) readContents(d *indexData, i uint32) []byte {
	return r.readSectionBlob(section{
		off: d.boundaries[i],
		sz:  d.boundaries[i+1] - d.boundaries[i],
	})
}

func (r *reader) readCaseBits(d *indexData, i uint32) []byte {
	return r.readSectionBlob(section{
		off: d.caseBitsIndex[i],
		sz:  d.caseBitsIndex[i+1] - d.caseBitsIndex[i],
	})
}

func (r *reader) readNewlines(d *indexData, i uint32) []uint32 {
	blob := r.readSectionBlob(section{
		off: d.newlinesIndex[i],
		sz:  d.newlinesIndex[i+1] - d.newlinesIndex[i],
	})
	last := -1

	var res []uint32
	for len(blob) > 0 {
		delta, m := binary.Uvarint(blob)
		next := int(delta) + last
		res = append(res, uint32(next))
		last = next
		blob = blob[m:]
	}

	return res
}

func (r *reader) readPostingData(d *indexData, idx uint32) ([]uint32, error) {
	sec := section{
		off: d.postingIndex[idx],
		sz:  d.postingIndex[idx+1] - d.postingIndex[idx],
	}

	data := r.readSectionBlob(sec)
	if r.err != nil {
		return nil, r.err
	}
	var ps []uint32
	var last uint32
	for len(data) > 0 {
		delta, m := binary.Uvarint(data)
		offset := last + uint32(delta)
		last = offset
		data = data[m:]
		ps = append(ps, offset)
	}
	return ps, nil
}

func (r *reader) readSearch(data *indexData, query *Query) (*searchInput, error) {
	str := strings.ToLower(query.Pattern) // UTF-8
	if len(str) < NGRAM {
		return nil, fmt.Errorf("patter must be at least %d bytes", NGRAM)
	}

	input := &searchInput{
		pat: str,
	}

	firstIdx, ok := data.findNgramIdx(str[:NGRAM])
	if !ok {
		return input, nil
	}
	lastIdx, ok := data.findNgramIdx(str[len(str)-NGRAM:])
	if !ok {
		return input, nil
	}

	var err error
	input.first, err = r.readPostingData(data, firstIdx)
	if err != nil {
		return nil, err
	}
	input.last, err = r.readPostingData(data, lastIdx)
	if err != nil {
		return nil, err
	}
	input.ends = data.fileEnds
	return input, nil
}

type Searcher interface {
	Search(query *Query) ([]Match, error)
	Close() error
}

type searcher struct {
	reader    reader
	indexData *indexData
}

func (s *searcher) Close() error {
	return s.reader.r.Close()
}

type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

func NewSearcher(r ReadSeekCloser) (Searcher, error) {
	s := &searcher{
		reader: reader{r: r},
	}
	var toc indexTOC
	s.reader.readTOC(&toc)
	s.indexData = s.reader.readIndexData(&toc)
	if s.reader.err != nil {
		return nil, s.reader.err
	}
	return s, nil
}

type Match struct {
	// Ranking; the lower, the better.
	Rank    int
	Line    string
	LineNum int
	LineOff int

	Name        string
	Offset      uint32
	MatchLength int
}

type Query struct {
	Pattern       string
	CaseSensitive bool
}

func (s *searcher) Search(pat *Query) ([]Match, error) {
	input, err := s.reader.readSearch(s.indexData, pat)
	if err != nil {
		return nil, err
	}
	cands := input.search()

	asBytes := []byte(pat.Pattern)
	patLen := len(pat.Pattern)

	// Find bitmasks for case sensitive search

	var patternCaseBits, patternCaseMask [][]byte

	if pat.CaseSensitive {
		patternCaseMask, patternCaseBits = findCaseMasks(asBytes)
	}
	asBytes = toLower(asBytes)

	var matches []Match
	lastFile := uint32(0xFFFFFFFF)
	var content []byte
	var caseBits []byte
	var newlines []uint32
	for _, c := range cands {
		if lastFile != c.file {
			caseBits = s.reader.readCaseBits(s.indexData, c.file)
		}

		if pat.CaseSensitive {
			startExtend := c.offset % 8
			patEnd := c.offset + uint32(patLen)
			endExtend := (8 - (patEnd % 8)) % 8

			start := c.offset - startExtend
			end := c.offset + uint32(patLen) + endExtend

			fileBits := append([]byte{}, caseBits[start/8:end/8]...)
			mask := patternCaseMask[startExtend]
			bits := patternCaseBits[startExtend]

			diff := false
			for i := range fileBits {
				if fileBits[i]&mask[i] != bits[i] {
					diff = true
					break
				}
			}
			if diff {
				continue
			}
		}

		if lastFile != c.file {
			content = s.reader.readContents(s.indexData, c.file)
			newlines = s.reader.readNewlines(s.indexData, c.file)
			lastFile = c.file
		}

		if bytes.Compare(content[c.offset:c.offset+uint32(patLen)], asBytes) == 0 {
			idx := sort.Search(len(newlines), func(n int) bool {
				return newlines[n] >= c.offset
			})

			end := uint32(len(content))
			if idx < len(newlines) {
				end = newlines[idx]
			}

			start := 0
			if idx > 0 {
				start = int(newlines[idx-1] + 1)
			}

			matches = append(matches, Match{
				Rank:   int(c.file),
				Offset: c.offset,
				Line: string(toOriginal(
					content, caseBits, start, int(end))),
				LineNum:     idx + 1,
				LineOff:     int(c.offset) - start,
				Name:        s.indexData.fileNames[c.file],
				MatchLength: patLen,
			})
		}
	}

	return matches, nil
}

type shardedSearcher struct {
	searchers []Searcher
}

func NewShardedSearcher(indexGlob string) (Searcher, error) {
	fs, err := filepath.Glob(indexGlob)
	if err != nil {
		return nil, err
	}

	if len(fs) == 0 {
		return nil, fmt.Errorf("glob %q does not match anything.", indexGlob)
	}

	ss := shardedSearcher{}

	for _, fn := range fs {
		f, err := os.Open(fn)
		if err != nil {
			return nil, err
		}

		s, err := NewSearcher(f)
		if err != nil {
			return nil, fmt.Errorf("NewSearcher(%s): %v", f, err)
		}
		ss.searchers = append(ss.searchers, s)
	}

	return &ss, nil
}

type matchSlice []Match

func (m matchSlice) Len() int           { return len(m) }
func (m matchSlice) Less(i, j int) bool { return m[i].Rank < m[j].Rank }
func (m matchSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

func (ss *shardedSearcher) Close() error {
	for _, s := range ss.searchers {
		s.Close()
	}
	return nil
}

func (ss *shardedSearcher) Search(pat *Query) ([]Match, error) {
	type res struct {
		m   []Match
		err error
	}
	all := make(chan res, len(ss.searchers))
	for _, s := range ss.searchers {
		go func(s Searcher) {
			ms, err := s.Search(pat)
			all <- res{ms, err}
		}(s)
	}

	var aggregate []Match
	for _ = range ss.searchers {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		aggregate = append(aggregate, r.m...)
	}
	sort.Sort((matchSlice)(aggregate))
	return aggregate, nil
}
