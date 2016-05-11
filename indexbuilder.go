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
	"html/template"
	"log"
	"sort"
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
	docSections [][]DocumentSection

	branchMasks []uint32

	// ngram => posting.
	contentPostings map[ngram][]uint32

	// like postings, but for filenames
	namePostings map[ngram][]uint32

	// Branch name => ID
	branches map[string]int

	// The repository name
	repoName string

	// The repository URL
	repoURL string
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

// SetRepoURL sets the repository URL template for linking back to
// files.
func (b *IndexBuilder) SetRepoURL(url string) error {
	_, err := template.New("url").Parse(url)
	if err != nil {
		return err
	}
	b.repoURL = url
	return nil
}

type DocumentSection struct {
	start, end uint32
}

// Document holds a document (file) to index.
type Document struct {
	Name     string
	Content  []byte
	Branches []string

	Symbols []DocumentSection
}

type docSectionSlice []DocumentSection

func (m docSectionSlice) Len() int           { return len(m) }
func (m docSectionSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m docSectionSlice) Less(i, j int) bool { return m[i].start < m[j].start }

// AddFile is a convenience wrapper for Add
func (b *IndexBuilder) AddFile(name string, content []byte) {
	b.Add(Document{Name: name, Content: content})
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
func (b *IndexBuilder) Add(doc Document) error {
	sort.Sort(docSectionSlice(doc.Symbols))

	var last DocumentSection
	for i, s := range doc.Symbols {
		if i > 0 {
			if last.end > s.start {
				return fmt.Errorf("sections overlap")
			}
		}
		last = s
	}

	b.files = append(b.files, newSearchableString(doc.Content, b.contentEnd, b.contentPostings))
	b.fileNames = append(b.fileNames, newSearchableString([]byte(doc.Name), b.nameEnd, b.namePostings))
	b.docSections = append(b.docSections, doc.Symbols)
	b.contentEnd += uint32(len(doc.Content))
	b.nameEnd += uint32(len(doc.Name))

	var mask uint32
	for _, br := range doc.Branches {
		mask |= uint32(b.addBranch(br))
	}

	b.branchMasks = append(b.branchMasks, mask)
	return nil
}
