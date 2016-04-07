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
	"fmt"
	"log"
)

var _ = log.Println

const NGRAM = 3

type fileEntry struct {
	content []byte
	name    string
	offset  uint32
}

func (e *fileEntry) end() uint32 {
	return e.offset + uint32(len(e.content))
}

type IndexBuilder struct {
	contentEnd uint32
	files      []fileEntry

	// ngram => posting.
	postings map[string][]uint32
}

func (m *candidateMatch) String() string {
	return fmt.Sprintf("%d:%d", m.file, m.offset)
}

func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{postings: make(map[string][]uint32)}
}

func (b *IndexBuilder) AddFile(name string, content []byte) {
	off := b.contentEnd
	for i := range content {
		if i+NGRAM > len(content) {
			break
		}
		ngram := string(content[i : i+NGRAM])
		b.postings[ngram] = append(b.postings[ngram], off+uint32(i))
	}
	b.files = append(b.files,
		fileEntry{
			name:    name,
			content: content,
			offset:  b.contentEnd,
		})
	b.contentEnd += uint32(len(content))
}

func (b *IndexBuilder) search(str string) ([]candidateMatch, error) {
	if len(str) < NGRAM {
		return nil, fmt.Errorf("too short")
	}
	if len(b.files) == 0 {
		return nil, fmt.Errorf("no files")
	}

	first := str[:NGRAM]
	last := str[len(str)-NGRAM:]

	input := searchInput{
		first: b.postings[first],
		last:  b.postings[last],
		pat:   str,
	}

	for _, f := range b.files {
		input.ends = append(input.ends, f.end())
	}

	input.ends = append(input.ends, b.files[len(b.files)-1].end())

	return input.search(), nil
}
