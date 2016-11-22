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
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"sort"
	"time"
)

// FormatVersion is a version number. It is increased every time the
// on-disk index format is changed.
// 5: subrepositories.
// 6: remove size prefix for posting varint list.
// 7: move subrepos into Repository struct.
// 8: move repoMetaData out of indexMetadata
// 9: use bigendian uint64 for trigrams.
const IndexFormatVersion = 8

// FeatureVersion is increased if a feature is added that requires reindexing data.
const FeatureVersion = 2

var _ = log.Println

type indexTOC struct {
	fileContents compoundSection
	fileNames    compoundSection
	fileSections compoundSection
	postings     compoundSection
	newlines     compoundSection
	ngramText    simpleSection

	branchMasks simpleSection
	subRepos    simpleSection

	nameNgramText simpleSection
	namePostings  compoundSection
	metaData      simpleSection
	repoMetaData  simpleSection
}

func (t *indexTOC) sections() []section {
	return []section{
		// This must be first, so it can be reliably read across
		// file format versions.
		&t.metaData,
		&t.repoMetaData,
		&t.fileContents,
		&t.fileNames,
		&t.fileSections,
		&t.newlines,
		&t.ngramText,
		&t.postings,
		&t.nameNgramText,
		&t.namePostings,
		&t.branchMasks,
		&t.subRepos,
	}
}

func (w *writer) writeTOC(toc *indexTOC) {
	secs := toc.sections()
	w.U32(uint32(len(secs)))
	for _, s := range secs {
		s.write(w)
	}
}

func (s *compoundSection) writeStrings(w *writer, strs []*searchableString) {
	s.start(w)
	for _, f := range strs {
		s.addItem(w, f.data)
	}
	s.end(w)
}

func (b *IndexBuilder) Write(out io.Writer) error {
	buffered := bufio.NewWriterSize(out, 1<<20)
	defer buffered.Flush()

	w := &writer{w: buffered}
	toc := indexTOC{}

	toc.fileContents.writeStrings(w, b.files)

	toc.newlines.start(w)
	for _, f := range b.files {
		toc.newlines.addItem(w, toSizedDeltas(newLinesIndices(f.data)))
	}
	toc.newlines.end(w)

	toc.branchMasks.start(w)
	for _, m := range b.branchMasks {
		w.U32(m)
	}
	toc.branchMasks.end(w)

	toc.fileSections.start(w)
	for _, s := range b.docSections {
		toc.fileSections.addItem(w, marshalDocSections(s))
	}
	toc.fileSections.end(w)

	var keys ngramSlice
	for k := range b.contentPostings {
		keys = append(keys, k)
	}
	sort.Sort(keys)

	toc.ngramText.start(w)
	for _, k := range keys {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(k))
		w.Write(buf[:])
	}
	toc.ngramText.end(w)

	toc.postings.start(w)
	for _, k := range keys {
		toc.postings.addItem(w, b.contentPostings[k])
	}
	toc.postings.end(w)

	// names.
	toc.fileNames.writeStrings(w, b.fileNames)

	keys = keys[:0]
	for k := range b.namePostings {
		keys = append(keys, k)
	}
	sort.Sort(keys)

	toc.nameNgramText.start(w)
	for _, k := range keys {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(k))
		w.Write(buf[:])
	}
	toc.nameNgramText.end(w)

	toc.namePostings.start(w)
	for _, k := range keys {
		toc.namePostings.addItem(w, b.namePostings[k])
	}
	toc.namePostings.end(w)

	toc.subRepos.start(w)
	w.Write(toSizedDeltas(b.subRepos))
	toc.subRepos.end(w)

	if err := b.writeJSON(&IndexMetadata{
		IndexFormatVersion:  IndexFormatVersion,
		IndexTime:           time.Now(),
		IndexFeatureVersion: FeatureVersion,
	}, &toc.metaData, w); err != nil {
		return err
	}
	if err := b.writeJSON(b.repo, &toc.repoMetaData, w); err != nil {
		return err
	}

	var tocSection simpleSection

	tocSection.start(w)
	w.writeTOC(&toc)
	tocSection.end(w)
	tocSection.write(w)
	return w.err
}

func (b *IndexBuilder) writeJSON(data interface{}, sec *simpleSection, w *writer) error {
	blob, err := json.Marshal(data)
	if err != nil {
		return err
	}
	sec.start(w)
	w.Write(blob)
	sec.end(w)
	return nil
}

func newLinesIndices(in []byte) []uint32 {
	out := make([]uint32, 0, len(in)/30)
	for i, c := range in {
		if c == '\n' {
			out = append(out, uint32(i))
		}
	}
	return out
}
