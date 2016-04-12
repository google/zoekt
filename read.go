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
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var _ = log.Println

func (r *reader) readTOC(toc *indexTOC) {
	if r.err != nil {
		return
	}

	r.r.Seek(-8, 2)
	var tocSection simpleSection
	tocSection.read(r)
	_, r.err = r.r.Seek(int64(tocSection.off), 0)
	for _, s := range toc.sections() {
		s.read(r)
	}
}

// indexData holds the pattern independent data that we have to have
// in memory to search.
type indexData struct {
	ngrams map[ngram]simpleSection

	postingsIndex []uint32
	newlinesIndex []uint32
	caseBitsIndex []uint32

	// offsets of file contents. Includes end of last file.
	boundaries []uint32

	fileEnds []uint32

	fileNameContent       []byte
	fileNameCaseBits      []byte
	fileNameCaseBitsIndex []uint32
	fileNameIndex         []uint32
	fileNameNgrams        map[ngram][]uint32
}

func (d *indexData) fileName(i uint32) string {
	return string(d.fileNameContent[d.fileNameIndex[i]:d.fileNameIndex[i+1]])

}
func (r *reader) readSectionBlob(sec simpleSection) []byte {
	d := make([]byte, sec.sz)
	r.r.Seek(int64(sec.off), 0)
	_, r.err = r.r.Read(d)
	return d
}

func (r *reader) readSectionU32(sec simpleSection) []uint32 {
	if sec.sz%4 != 0 {
		log.Panic("barf", sec.sz)
	}
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

	toc.fileContents.content.readIndex(r)
	toc.fileContents.caseBits.readIndex(r)
	toc.postings.readIndex(r)
	toc.newlines.readIndex(r)

	toc.namePostings.readIndex(r)
	toc.fileNames.content.readIndex(r)
	toc.fileNames.caseBits.readIndex(r)

	d := indexData{
		postingsIndex:  toc.postings.absoluteIndex(),
		caseBitsIndex:  toc.fileContents.caseBits.absoluteIndex(),
		boundaries:     toc.fileContents.content.absoluteIndex(),
		newlinesIndex:  toc.newlines.absoluteIndex(),
		ngrams:         map[ngram]simpleSection{},
		fileNameNgrams: map[ngram][]uint32{},
	}

	textContent := r.readSectionBlob(toc.ngramText)
	for i := 0; i < len(textContent); i += NGRAM {
		j := i / NGRAM
		d.ngrams[bytesToNGram(textContent[i:i+NGRAM])] = simpleSection{
			d.postingsIndex[j],
			d.postingsIndex[j+1] - d.postingsIndex[j],
		}
	}

	d.fileEnds = toc.fileContents.content.relativeIndex()[1:]

	d.fileNameContent = r.readSectionBlob(toc.fileNames.content.data)
	d.fileNameCaseBits = r.readSectionBlob(toc.fileNames.caseBits.data)
	d.fileNameCaseBitsIndex = toc.fileNames.caseBits.relativeIndex()
	d.fileNameIndex = toc.fileNames.content.relativeIndex()

	nameNgramText := r.readSectionBlob(toc.nameNgramText)
	fileNamePostingsData := r.readSectionBlob(toc.namePostings.data)
	fileNamePostingsIndex := toc.namePostings.relativeIndex()
	for i := 0; i < len(nameNgramText); i += NGRAM {
		j := i / NGRAM
		off := fileNamePostingsIndex[j]
		end := fileNamePostingsIndex[j+1]
		d.fileNameNgrams[bytesToNGram(nameNgramText[i:i+NGRAM])] = fromDeltas(fileNamePostingsData[off:end])
	}

	return &d
}

func (r *reader) readContents(d *indexData, i uint32) []byte {
	return r.readSectionBlob(simpleSection{
		off: d.boundaries[i],
		sz:  d.boundaries[i+1] - d.boundaries[i],
	})
}

func (r *reader) readCaseBits(d *indexData, i uint32) []byte {
	return r.readSectionBlob(simpleSection{
		off: d.caseBitsIndex[i],
		sz:  d.caseBitsIndex[i+1] - d.caseBitsIndex[i],
	})
}

func (r *reader) readNewlines(d *indexData, i uint32) []uint32 {
	blob := r.readSectionBlob(simpleSection{
		off: d.newlinesIndex[i],
		sz:  d.newlinesIndex[i+1] - d.newlinesIndex[i],
	})

	return fromDeltas(blob)
}

func (r *reader) getDocIterator(data *indexData, query *SubstringQuery) (*docIterator, error) {
	if len(query.Pattern) < NGRAM {
		return nil, fmt.Errorf("pattern %q less than %d bytes", query.Pattern, NGRAM)
	}

	if query.FileName {
		return r.getFileNameDocIterator(data, query), nil
	}
	return r.getContentDocIterator(data, query)
}

func (r *reader) getFileNameDocIterator(data *indexData, query *SubstringQuery) *docIterator {
	str := strings.ToLower(query.Pattern) // TODO - UTF-8
	di := &docIterator{
		query:  query,
		patLen: uint32(len(str)),
		ends:   data.fileNameIndex[1:],
		first:  data.fileNameNgrams[stringToNGram(str[:NGRAM])],
		last:   data.fileNameNgrams[stringToNGram(str[len(str)-NGRAM:])],
	}

	return di
}

func (r *reader) getContentDocIterator(data *indexData, query *SubstringQuery) (*docIterator, error) {
	str := strings.ToLower(query.Pattern) // TODO - UTF-8
	input := &docIterator{
		query:  query,
		patLen: uint32(len(str)),
		ends:   data.fileEnds,
	}
	first, ok := data.ngrams[stringToNGram(str[:NGRAM])]
	if !ok {
		return input, nil
	}

	last, ok := data.ngrams[stringToNGram(str[len(str)-NGRAM:])]
	if !ok {
		return input, nil
	}

	input.first = fromDeltas(r.readSectionBlob(first))
	if r.err != nil {
		return nil, r.err
	}
	input.last = fromDeltas(r.readSectionBlob(last))
	if r.err != nil {
		return nil, r.err
	}
	return input, nil
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

func (s *searcher) Search(query Query) (*SearchResult, error) {
	orQ, err := standardize(query)
	if err != nil {
		return nil, err
	}

	if len(orQ.ands) != 1 {
		return nil, fmt.Errorf("not implemented: OrQuery")
	}

	andQ := orQ.ands[0]
	return s.andSearch(andQ)
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

type matchSlice []FileMatch

func (m matchSlice) Len() int           { return len(m) }
func (m matchSlice) Less(i, j int) bool { return m[i].Rank < m[j].Rank }
func (m matchSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

func (ss *shardedSearcher) Close() error {
	for _, s := range ss.searchers {
		s.Close()
	}
	return nil
}

func (ss *shardedSearcher) Search(pat Query) (*SearchResult, error) {
	start := time.Now()
	type res struct {
		sr  *SearchResult
		err error
	}
	all := make(chan res, len(ss.searchers))
	for _, s := range ss.searchers {
		go func(s Searcher) {
			ms, err := s.Search(pat)
			all <- res{ms, err}
		}(s)
	}

	var aggregate SearchResult
	for _ = range ss.searchers {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		aggregate.Files = append(aggregate.Files, r.sr.Files...)
		aggregate.Stats.Add(r.sr.Stats)
	}
	sort.Sort((matchSlice)(aggregate.Files))
	aggregate.Duration = time.Now().Sub(start)
	return &aggregate, nil
}
