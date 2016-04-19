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
	"io"
	"log"
)

var _ = log.Println

// writer is an io.Writer that keeps track of errors and offsets
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

// reader is a ReadSeekCloser that keeps track of errors
type reader struct {
	r   ReadSeekCloser
	err error
}

func (r *reader) U32() uint32 {
	if r.err != nil {
		return 0
	}
	var b [4]byte
	_, r.err = r.r.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}

func (s *simpleSection) start(w *writer) {
	s.off = w.Off()
}

func (s *simpleSection) end(w *writer) {
	s.sz = w.Off() - s.off
}

// section is a range of bytes in the index file.
type section interface {
	read(*reader)
	write(*writer)
}

// simpleSection is a simple range of bytes.
type simpleSection struct {
	off uint32
	sz  uint32
}

func (s *simpleSection) read(r *reader) {
	s.off = r.U32()
	s.sz = r.U32()
}

func (s *simpleSection) write(w *writer) {
	w.U32(s.off)
	w.U32(s.sz)
}

// compoundSection is a range of bytes containg a list of variable
// sized items.
type compoundSection struct {
	data simpleSection

	offsets []uint32
	index   simpleSection
}

func (s *compoundSection) start(w *writer) {
	s.data.start(w)
}

func (s *compoundSection) end(w *writer) {
	s.data.end(w)
	s.index.start(w)
	for _, o := range s.offsets {
		w.U32(o)
	}
	s.index.end(w)
}

func (s *compoundSection) addItem(w *writer, item []byte) {
	s.offsets = append(s.offsets, w.Off())
	w.Write(item)
}

func (s *compoundSection) write(w *writer) {
	s.data.write(w)
	s.index.write(w)
}

func (s *compoundSection) read(r *reader) {
	s.data.read(r)
	s.index.read(r)
	// cannot read items.
}

func (s *compoundSection) readIndex(r *reader) {
	s.offsets = r.readSectionU32(s.index)
}

// absoluteIndex returns the offsets of items, plus a final marking the end of the
// last item.
func (s *compoundSection) absoluteIndex() []uint32 {
	index := s.offsets
	index = append(index, s.data.off+s.data.sz)
	return index
}

// relativeIndex returns the relative offsets of the items (first
// element is 0), plus a final marking the end of the last item.
func (s *compoundSection) relativeIndex() []uint32 {
	ri := make([]uint32, 0, len(s.offsets)+1)
	for _, o := range s.offsets {
		ri = append(ri, o-s.offsets[0])
	}
	if len(s.offsets) > 0 {
		ri = append(ri, s.data.sz)
	}
	return ri
}

func (s *compoundSection) readBlob(r *reader, i uint32) []byte {
	return r.readSectionBlob(simpleSection{s.offsets[i], s.offsets[i+1] - s.offsets[i]})
}

type contentSection struct {
	content  compoundSection
	caseBits compoundSection
}

func (s *contentSection) readIndex(r *reader) {
	s.content.readIndex(r)
	s.caseBits.readIndex(r)
}

func (s *contentSection) writeStrings(w *writer, strs []*searchableString) {
	s.content.start(w)
	for _, f := range strs {
		s.content.addItem(w, f.data)
	}
	s.content.end(w)

	s.caseBits.start(w)
	for _, f := range strs {
		s.caseBits.addItem(w, f.caseBits)
	}
	s.caseBits.end(w)
}

func (s *contentSection) read(r *reader) {
	s.content.read(r)
	s.caseBits.read(r)
}

func (s *contentSection) write(w *writer) {
	s.content.write(w)
	s.caseBits.write(w)
}

func toDeltas(offsets []uint32) []byte {
	deltas := make([]byte, 0, len(offsets)*2)
	var last uint32
	for _, p := range offsets {
		delta := p - last
		last = p
		var enc [8]byte
		m := binary.PutUvarint(enc[:], uint64(delta))
		deltas = append(deltas, enc[:m]...)
	}
	return deltas
}

func fromDeltas(data []byte) []uint32 {
	var last uint32
	ps := make([]uint32, 0, len(data)/2)
	for len(data) > 0 {
		delta, m := binary.Uvarint(data)
		offset := last + uint32(delta)
		last = offset
		data = data[m:]
		ps = append(ps, offset)
	}
	return ps
}
