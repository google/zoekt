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
	"encoding/json"
	"fmt"
	"log"
	"sort"
)

// reader is a stateful file
type reader struct {
	r   IndexFile
	off uint32
}

func (r *reader) seek(off uint32) {
	r.off = off
}

func (r *reader) U32() (uint32, error) {
	b, err := r.r.Read(r.off, 4)
	r.off += 4
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

var _ = log.Println

func (r *reader) readTOC(toc *indexTOC) error {
	sz, err := r.r.Size()
	if err != nil {
		return err
	}
	r.off = sz - 8

	var tocSection simpleSection
	if err := tocSection.read(r); err != nil {
		return err
	}

	r.seek(tocSection.off)

	sectionCount, err := r.U32()
	if err != nil {
		return err
	}

	secs := toc.sections()

	if len(secs) != int(sectionCount) {
		return fmt.Errorf("section count mismatch: got %d want %d", len(secs), sectionCount)
	}

	for _, s := range toc.sections() {
		if err := s.read(r); err != nil {
			return err
		}
	}
	return nil
}

func (r *indexData) readSectionBlob(sec simpleSection) ([]byte, error) {
	return r.file.Read(sec.off, sec.sz)
}

func (r *indexData) readSectionU32(sec simpleSection) ([]uint32, error) {
	return readSectionU32(r.file, sec)
}

func readSectionU32(f IndexFile, sec simpleSection) ([]uint32, error) {
	if sec.sz%4 != 0 {
		return nil, fmt.Errorf("barf: section size %% 4 != 0: sz %d ", sec.sz)
	}
	blob, err := f.Read(sec.off, sec.sz)
	if err != nil {
		return nil, err
	}
	arr := make([]uint32, 0, len(blob)/4)
	for len(blob) > 0 {
		arr = append(arr, binary.BigEndian.Uint32(blob))
		blob = blob[4:]
	}
	return arr, nil
}

func (r *reader) readIndexData(toc *indexTOC) (*indexData, error) {
	d := indexData{
		file:           r.r,
		ngrams:         map[ngram]simpleSection{},
		fileNameNgrams: map[ngram][]uint32{},
		branchIDs:      map[string]uint{},
		branchNames:    map[uint]string{},
	}
	blob, err := d.readSectionBlob(toc.unaryData)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(blob, &d.unaryData); err != nil {
		return nil, err
	}

	if d.unaryData.IndexFormatVersion != IndexFormatVersion {
		return nil, fmt.Errorf("file is v%d, want v%d", d.unaryData.IndexFormatVersion, IndexFormatVersion)
	}

	d.boundaries = toc.fileContents.absoluteIndex()
	d.newlinesIndex = toc.newlines.absoluteIndex()
	d.docSectionsIndex = toc.fileSections.absoluteIndex()

	textContent, err := d.readSectionBlob(toc.ngramText)
	if err != nil {
		return nil, err
	}
	postingsIndex := toc.postings.absoluteIndex()

	for i := 0; i < len(textContent); i += ngramSize {
		j := i / ngramSize
		d.ngrams[bytesToNGram(textContent[i:i+ngramSize])] = simpleSection{
			postingsIndex[j],
			postingsIndex[j+1] - postingsIndex[j],
		}
	}

	if r := toc.fileContents.relativeIndex(); len(r) > 0 {
		d.fileEnds = r[1:]
	}
	d.fileBranchMasks, err = d.readSectionU32(toc.branchMasks)
	if err != nil {
		return nil, err
	}

	d.fileNameContent, err = d.readSectionBlob(toc.fileNames.data)
	if err != nil {
		return nil, err
	}

	d.fileNameIndex = toc.fileNames.relativeIndex()

	nameNgramText, err := d.readSectionBlob(toc.nameNgramText)
	if err != nil {
		return nil, err
	}

	fileNamePostingsData, err := d.readSectionBlob(toc.namePostings.data)
	if err != nil {
		return nil, err
	}

	fileNamePostingsIndex := toc.namePostings.relativeIndex()
	for i := 0; i < len(nameNgramText); i += ngramSize {
		j := i / ngramSize
		off := fileNamePostingsIndex[j]
		end := fileNamePostingsIndex[j+1]
		ngram := bytesToNGram(nameNgramText[i : i+ngramSize])
		d.fileNameNgrams[ngram] = fromDeltas(fileNamePostingsData[off:end], nil)
	}

	for j, br := range d.unaryData.Repository.Branches {
		id := uint(1) << uint(j)
		d.branchIDs[br.Name] = id
		d.branchNames[id] = br.Name
	}

	if blob, err := d.readSectionBlob(toc.subRepos); err != nil {
		return nil, err
	} else {
		d.subRepos = fromSizedDeltas(blob, nil)
	}

	var keys []string
	for k := range d.unaryData.SubRepoMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	d.subRepoPaths = keys

	return &d, nil
}

func (d *indexData) readContents(i uint32) ([]byte, error) {
	return d.readSectionBlob(simpleSection{
		off: d.boundaries[i],
		sz:  d.boundaries[i+1] - d.boundaries[i],
	})
}

func (d *indexData) readNewlines(i uint32, buf []uint32) ([]uint32, error) {
	blob, err := d.readSectionBlob(simpleSection{
		off: d.newlinesIndex[i],
		sz:  d.newlinesIndex[i+1] - d.newlinesIndex[i],
	})
	if err != nil {
		return nil, err
	}

	return fromSizedDeltas(blob, buf), nil
}

func (d *indexData) readDocSections(i uint32) ([]DocumentSection, error) {
	blob, err := d.readSectionBlob(simpleSection{
		off: d.docSectionsIndex[i],
		sz:  d.docSectionsIndex[i+1] - d.docSectionsIndex[i],
	})
	if err != nil {
		return nil, err
	}

	return unmarshalDocSections(blob), nil
}

// IndexFile is a file suitable for concurrent read access. For performance
// reasons, it allows a mmap'd implementation.
type IndexFile interface {
	Read(off uint32, sz uint32) ([]byte, error)
	Size() (uint32, error)
	Close()
	Name() string
}

// NewSearcher creates a Searcher for a single index file.
func NewSearcher(r IndexFile) (Searcher, error) {
	rd := &reader{r: r}

	var toc indexTOC
	if err := rd.readTOC(&toc); err != nil {
		return nil, err
	}
	indexData, err := rd.readIndexData(&toc)
	if err != nil {
		return nil, err
	}
	indexData.file = r
	return indexData, nil
}
