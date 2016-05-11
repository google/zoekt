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
	"encoding/binary"
	"fmt"
	"log"
	"os"
)

// reader is a ReadSeekCloser that keeps track of errors
type reader struct {
	r   IndexFile
	off uint32
	err error
}

func (r *reader) seek(off uint32) {
	r.off = off
}

func (r *reader) U32() uint32 {
	if r.err != nil {
		return 0
	}

	var b []byte
	b, r.err = r.r.Read(r.off, 4)
	r.off += 4
	return binary.BigEndian.Uint32(b[:])
}

var _ = log.Println

func (r *reader) readTOC(toc *indexTOC) {
	if r.err != nil {
		return
	}

	var sz uint32
	sz, r.err = r.r.Size()
	r.off = sz - 8

	var tocSection simpleSection
	tocSection.read(r)

	r.seek(tocSection.off)

	sectionCount := r.U32()
	secs := toc.sections()
	if len(secs) != int(sectionCount) {
		r.err = fmt.Errorf("section count mismatch: got %d want %d", len(secs), sectionCount)
	}

	for _, s := range toc.sections() {
		s.read(r)
	}
}

func (r *indexData) readSectionBlob(sec simpleSection) []byte {
	if r.err != nil {
		return nil
	}

	var res []byte
	res, r.err = r.file.Read(sec.off, sec.sz)
	return res
}

func (r *indexData) readSectionU32(sec simpleSection) []uint32 {
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

type indexReader interface {
	readIndex(*indexData)
}

func (r *reader) readIndexData(toc *indexTOC) *indexData {
	if r.err != nil {
		return nil
	}

	d := indexData{
		file:           r.r,
		ngrams:         map[ngram]simpleSection{},
		fileNameNgrams: map[ngram][]uint32{},
		branchNames:    map[int]string{},
		branchIDs:      map[string]int{},
	}
	for _, sec := range toc.sections() {
		if ir, ok := sec.(indexReader); ok {
			ir.readIndex(&d)
		}
	}

	d.postingsIndex = toc.postings.absoluteIndex()
	d.caseBitsIndex = toc.fileContents.caseBits.absoluteIndex()
	d.boundaries = toc.fileContents.content.absoluteIndex()
	d.newlinesIndex = toc.newlines.absoluteIndex()
	d.docSectionsIndex = toc.fileSections.absoluteIndex()

	textContent := d.readSectionBlob(toc.ngramText)
	for i := 0; i < len(textContent); i += ngramSize {
		j := i / ngramSize
		d.ngrams[bytesToNGram(textContent[i:i+ngramSize])] = simpleSection{
			d.postingsIndex[j],
			d.postingsIndex[j+1] - d.postingsIndex[j],
		}
	}

	d.fileEnds = toc.fileContents.content.relativeIndex()[1:]
	d.fileBranchMasks = d.readSectionU32(toc.branchMasks)
	d.fileNameContent = d.readSectionBlob(toc.fileNames.content.data)
	d.fileNameCaseBits = d.readSectionBlob(toc.fileNames.caseBits.data)
	d.fileNameCaseBitsIndex = toc.fileNames.caseBits.relativeIndex()
	d.fileNameIndex = toc.fileNames.content.relativeIndex()
	d.repoName = string(d.readSectionBlob(toc.repoName))
	d.repoURL = string(d.readSectionBlob(toc.repoURL))
	nameNgramText := d.readSectionBlob(toc.nameNgramText)
	fileNamePostingsData := d.readSectionBlob(toc.namePostings.data)
	fileNamePostingsIndex := toc.namePostings.relativeIndex()
	for i := 0; i < len(nameNgramText); i += ngramSize {
		j := i / ngramSize
		off := fileNamePostingsIndex[j]
		end := fileNamePostingsIndex[j+1]
		d.fileNameNgrams[bytesToNGram(nameNgramText[i:i+ngramSize])] = fromDeltas(fileNamePostingsData[off:end])
	}

	branchNameContent := d.readSectionBlob(toc.branchNames.data)
	if branchNameIndex := toc.branchNames.relativeIndex(); len(branchNameIndex) > 0 {
		var last uint32
		for i, end := range branchNameIndex[1:] {
			n := string(branchNameContent[last:end])
			id := i + 1
			d.branchIDs[n] = id
			d.branchNames[id] = n
			last = end
		}
	}
	return &d
}

func (d *indexData) readContents(i uint32) []byte {
	return d.readSectionBlob(simpleSection{
		off: d.boundaries[i],
		sz:  d.boundaries[i+1] - d.boundaries[i],
	})
}

func (d *indexData) readCaseBits(i uint32) []byte {
	return d.readSectionBlob(simpleSection{
		off: d.caseBitsIndex[i],
		sz:  d.caseBitsIndex[i+1] - d.caseBitsIndex[i],
	})
}

func (d *indexData) readNewlines(i uint32) []uint32 {
	blob := d.readSectionBlob(simpleSection{
		off: d.newlinesIndex[i],
		sz:  d.newlinesIndex[i+1] - d.newlinesIndex[i],
	})

	return fromDeltas(blob)
}

func (d *indexData) readDocSections(i uint32) []DocumentSection {
	blob := d.readSectionBlob(simpleSection{
		off: d.docSectionsIndex[i],
		sz:  d.docSectionsIndex[i+1] - d.docSectionsIndex[i],
	})

	return unmarshalDocSections(blob)
}

// IndexFile is a file suitable for concurrent read access. For performance
// reasons, it allows a mmap'd implementation.
type IndexFile interface {
	Read(off uint32, sz uint32) ([]byte, error)
	Size() (uint32, error)
	Close()
}

// NewSearcher creates a Searcher for a single index file.
func NewSearcher(r IndexFile) (Searcher, error) {
	rd := &reader{r: r}

	var toc indexTOC
	rd.readTOC(&toc)
	indexData := rd.readIndexData(&toc)
	if rd.err != nil {
		return nil, rd.err
	}
	indexData.file = r
	return indexData, nil
}

// NewIndexFile wraps a os.File to be an IndexFile.
func NewIndexFile(f *os.File) IndexFile {
	return &indexFileFromOS{f}
}

type indexFileFromOS struct {
	f *os.File
}

func (f *indexFileFromOS) Read(off, sz uint32) ([]byte, error) {
	r := make([]byte, sz)
	_, err := f.f.ReadAt(r, int64(off))
	return r, err
}

func (f indexFileFromOS) Size() (uint32, error) {
	fi, err := f.f.Stat()
	if err != nil {
		return 0, err
	}

	sz := fi.Size()

	if sz >= maxUInt32 {
		return 0, fmt.Errorf("overflow")
	}

	return uint32(sz), nil
}

func (f indexFileFromOS) Close() {
	f.f.Close()
}
