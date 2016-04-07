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
	"bufio"
	"encoding/binary"
	"io"
	"log"
	"sort"
)

var _ = log.Println

type writer struct {
	err error
	w   io.Writer
	off uint32
}

func (w *writer) Write(b []byte) error {
	if w.err != nil {
		return w.err
	}

	var n int
	n, w.err = w.w.Write(b)
	w.off += uint32(n)
	return w.err
}

func (w *writer) Off() uint32 { return w.off }

func (w *writer) B(b byte) {
	s := []byte{b}
	w.Write(s)
}

func (w *writer) U32(n uint32) {
	var enc [4]byte
	binary.BigEndian.PutUint32(enc[:], n)
	w.Write(enc[:])
}

func (w *writer) Varint(n uint32) {
	var enc [8]byte
	m := binary.PutUvarint(enc[:], uint64(n))
	w.Write(enc[:m])
}

func (w *writer) startSection(s *section) {
	s.off = w.Off()
}

func (w *writer) endSection(s *section) {
	s.sz = w.Off() - s.off
}

type section struct {
	off uint32
	sz  uint32
}

func (w *writer) writeSection(s *section) {
	w.U32(s.off)
	w.U32(s.sz)
}

type indexTOC struct {
	contents          section
	contentBoundaries section
	newlines          section
	newlinesIndex     section
	ngramText         section
	ngramFrequencies  section
	postings          section
	postingsIndex     section
	names             section
	nameIndex         section
}

func (t *indexTOC) sections() []*section {
	return []*section{
		&t.contents,
		&t.contentBoundaries,
		&t.newlines,
		&t.newlinesIndex,
		&t.ngramText,
		&t.ngramFrequencies,
		&t.postings,
		&t.postingsIndex,
		&t.names,
		&t.nameIndex,
	}
}

func (w *writer) writeTOC(toc *indexTOC) {
	for _, s := range toc.sections() {
		w.writeSection(s)
	}
}

func (b *IndexBuilder) Write(out io.Writer) error {
	buffered := bufio.NewWriterSize(out, 1<<20)
	defer buffered.Flush()

	w := &writer{w: buffered}
	toc := indexTOC{}
	var items []uint32
	w.startSection(&toc.contents)
	for _, f := range b.files {
		items = append(items, w.Off())
		w.Write(f.content)
	}
	w.endSection(&toc.contents)

	w.startSection(&toc.contentBoundaries)
	for _, off := range items {
		w.U32(off)
	}
	w.endSection(&toc.contentBoundaries)

	w.startSection(&toc.newlines)
	items = items[:0]
	for _, f := range b.files {
		items = append(items, w.Off())
		last := -1
		for i, c := range f.content {
			if c == '\n' {
				w.Varint(uint32(i - last))
				last = i
			}
		}
	}
	w.endSection(&toc.newlines)

	w.startSection(&toc.newlinesIndex)
	for _, off := range items {
		w.U32(off)
	}
	w.endSection(&toc.newlinesIndex)

	var keys []string
	for k := range b.postings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w.startSection(&toc.ngramText)
	for _, k := range keys {
		w.Write([]byte(k))
	}
	w.endSection(&toc.ngramText)

	w.startSection(&toc.postings)
	items = items[:0]
	for _, k := range keys {
		var last uint32
		items = append(items, w.Off())
		for _, p := range b.postings[k] {
			delta := p - last
			w.Varint(delta)
			last = p
		}
	}
	w.endSection(&toc.postings)

	w.startSection(&toc.ngramFrequencies)
	for _, k := range keys {
		n := uint32(len(b.postings[k]))
		w.U32(n)
	}
	w.endSection(&toc.ngramFrequencies)

	w.startSection(&toc.postingsIndex)
	for _, off := range items {
		w.U32(off)
	}
	w.endSection(&toc.postingsIndex)

	w.startSection(&toc.names)
	items = items[:0]
	for _, f := range b.files {
		items = append(items, w.Off())
		w.Write([]byte(f.name))
	}
	w.endSection(&toc.names)

	w.startSection(&toc.nameIndex)
	for _, off := range items {
		w.U32(off)
	}
	w.endSection(&toc.nameIndex)

	var tocSection section
	w.startSection(&tocSection)
	w.writeTOC(&toc)

	w.endSection(&tocSection)
	w.writeSection(&tocSection)

	return w.err
}
