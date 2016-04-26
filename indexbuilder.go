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
	"log"
)

var _ = log.Println

const ngramSize = 3

type searchableString struct {
	// lower cased data.
	data []byte

	// Bit vector describing where we found uppercase letters
	caseBits []byte

	// offset of the content
	offset uint32
}

func (e *searchableString) end() uint32 {
	return e.offset + uint32(len(e.data))
}

func newSearchableString(data []byte, startOff uint32, postings map[ngram][]uint32) *searchableString {
	dest := searchableString{
		offset: startOff,
	}
	dest.data, dest.caseBits = splitCase(data)
	for i := range dest.data {
		if i+ngramSize > len(dest.data) {
			break
		}
		ngram := bytesToNGram(dest.data[i : i+ngramSize])
		postings[ngram] = append(postings[ngram], startOff+uint32(i))
	}
	return &dest
}

// IndexBuilder builds a single index shard.
type IndexBuilder struct {
	contentEnd uint32
	nameEnd    uint32

	files       []*searchableString
	fileNames   []*searchableString
	branchMasks []uint32

	// ngram => posting.
	contentPostings map[ngram][]uint32

	// like postings, but for filenames
	namePostings map[ngram][]uint32

	// Branch name => ID
	branches map[string]int

	// The repository name
	repoName string
}

// ContentSize returns the number of content bytes so far ingested.
func (b *IndexBuilder) ContentSize() uint32 {
	// Add the name too so we don't skip building index if we have
	// lots of empty files.
	return b.contentEnd + b.nameEnd
}

// NewIndexBuilder creates a fresh IndexBuilder.
func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{
		contentPostings: make(map[ngram][]uint32),
		namePostings:    make(map[ngram][]uint32),
		branches:        make(map[string]int),
	}
}

func (b *IndexBuilder) SetName(nm string) {
	b.repoName = nm
}

// AddFile adds a file. This is the basic ordering for search results,
// so if possible the most important files should be added last.
func (b *IndexBuilder) AddFile(name string, content []byte) {
	b.AddFileBranches(name, content, nil)
}

func (b *IndexBuilder) addBranch(br string) int {
	id, ok := b.branches[br]
	if !ok {
		id = len(b.branches) + 1
		b.branches[br] = id
	}

	return id
}

// AddBranch registers a branch name.  The first is assumed to be the
// default.
func (b *IndexBuilder) AddBranch(branch string) {
	b.addBranch(branch)
}

// Add a file which only occurs in certain branches.
func (b *IndexBuilder) AddFileBranches(name string, content []byte, branches []string) {
	b.files = append(b.files, newSearchableString(content, b.contentEnd, b.contentPostings))
	b.fileNames = append(b.fileNames, newSearchableString([]byte(name), b.nameEnd, b.namePostings))
	b.contentEnd += uint32(len(content))
	b.nameEnd += uint32(len(name))

	var mask uint32
	for _, br := range branches {
		mask |= uint32(b.addBranch(br))
	}

	b.branchMasks = append(b.branchMasks, mask)
}
