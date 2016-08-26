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

	// offset of the content
	offset uint32
}

func (e *searchableString) end() uint32 {
	return e.offset + uint32(len(e.data))
}

func newSearchableString(data []byte, startOff uint32, postings map[ngram][]uint32) *searchableString {
	dest := searchableString{
		offset: startOff,
		data:   data,
	}
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

	// Branch names; first is the default.
	branches []string

	// Branch versions. Length matches branches.
	versions []string

	// The repository name
	repoName string

	// The repository URL.
	repoURL string

	// The repository URL.
	fileURLTemplate string

	// The URL fragment for line numbers.
	lineFragmentTemplate string

	// The URL template for a branch.
	commitURLTemplate string
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
	}
}

func (b *IndexBuilder) SetName(nm string) {
	b.repoName = nm
}

// SetURLTemplates sets the repository URL templates for linking back to
// files. The fileURL has access to {{Branch}}, {{Path}} and fragment
// to {{LineNumber}}.
func (b *IndexBuilder) SetURLTemplates(repoURL, commitURL, fileURL, fragment string) error {
	for _, t := range []string{commitURL, fileURL, fragment} {
		if _, err := template.New("").Parse(t); err != nil {
			return err
		}
	}
	b.repoURL = repoURL
	b.commitURLTemplate = commitURL
	b.fileURLTemplate = fileURL
	b.lineFragmentTemplate = fragment
	return nil
}

type DocumentSection struct {
	Start, End uint32
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
func (m docSectionSlice) Less(i, j int) bool { return m[i].Start < m[j].Start }

// AddFile is a convenience wrapper for Add
func (b *IndexBuilder) AddFile(name string, content []byte) {
	b.Add(Document{Name: name, Content: content})
}

// addBranch adds a branch (if new) and returns its index.
func (b *IndexBuilder) addBranch(br, v string) int {
	for i, n := range b.branches {
		if n == br {
			return i
		}
	}
	b.branches = append(b.branches, br)
	b.versions = append(b.versions, v)
	return len(b.branches) - 1
}

// AddBranch registers a branch name.  The first is assumed to be the
// default.
func (b *IndexBuilder) AddBranch(br, version string) error {
	idx := b.addBranch(br, version)
	if idx >= 32 {
		return fmt.Errorf("branch %q: branch counts are limited to 32")
	}
	return nil
}

const maxTrigramCount = 20000

// Add a file which only occurs in certain branches.
func (b *IndexBuilder) Add(doc Document) error {
	if len(doc.Content) >= ngramSize {
		trigrams := map[ngram]struct{}{}
		for i := ngramSize; i < len(doc.Content); i++ {
			trigrams[bytesToNGram(doc.Content[i-3:i])] = struct{}{}

			if len(trigrams) > maxTrigramCount {
				// probably not text.
				return nil
			}
		}
	}

	sort.Sort(docSectionSlice(doc.Symbols))

	var last DocumentSection
	for i, s := range doc.Symbols {
		if i > 0 {
			if last.End > s.Start {
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
		mask |= uint32(1 << uint(b.addBranch(br, "")))
	}

	b.branchMasks = append(b.branchMasks, mask)
	return nil
}
