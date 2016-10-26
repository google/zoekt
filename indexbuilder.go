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
	"path/filepath"
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
	subRepos    []uint32

	// ngram => posting.
	contentPostings map[ngram][]uint32

	// like postings, but for filenames
	namePostings map[ngram][]uint32

	// root repository
	repo Repository

	// subRepositories
	subRepoMap map[string]*Repository

	// name to index.
	subRepoIndices map[string]uint32
}

func (d *Repository) verify() error {
	for _, t := range []string{d.FileURLTemplate, d.LineFragmentTemplate, d.CommitURLTemplate} {
		if _, err := template.New("").Parse(t); err != nil {
			return err
		}
	}
	return nil
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
		subRepoMap:      map[string]*Repository{},
	}
}

// AddSubRepository adds repository metadata for a subrepository. The
// Branches field is ignored.
func (b *IndexBuilder) AddSubRepository(path string, desc *Repository) error {
	if len(b.files) > 0 {
		return fmt.Errorf("AddSubRepository called after adding files.")
	}
	branchEqual := len(b.repo.Branches) == len(desc.Branches)
	if branchEqual {
		for i, b := range b.repo.Branches {
			branchEqual = branchEqual && (b.Name == desc.Branches[i].Name)
		}
	}

	if !branchEqual {
		return fmt.Errorf("got subrepository branches %v, want main repository branches %v", b.repo.Branches, desc.Branches)
	}
	if err := desc.verify(); err != nil {
		return err
	}

	r := *desc
	b.subRepoMap[path] = &r
	return nil
}

// AddRepository adds repository metadata. The Branches field is
// ignored.
func (b *IndexBuilder) AddRepository(desc *Repository) error {
	if len(b.files) > 0 {
		return fmt.Errorf("AddSubRepository called after adding files.")
	}
	if err := desc.verify(); err != nil {
		return err
	}

	before := b.repo.Branches
	b.repo = *desc
	b.repo.Branches = before
	return nil
}

type DocumentSection struct {
	Start, End uint32
}

// Document holds a document (file) to index.
type Document struct {
	Name              string
	Content           []byte
	Branches          []string
	SubRepositoryPath string

	Symbols []DocumentSection
}

type docSectionSlice []DocumentSection

func (m docSectionSlice) Len() int           { return len(m) }
func (m docSectionSlice) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m docSectionSlice) Less(i, j int) bool { return m[i].Start < m[j].Start }

// AddFile is a convenience wrapper for Add
func (b *IndexBuilder) AddFile(name string, content []byte) error {
	return b.Add(Document{Name: name, Content: content})
}

// addBranch adds a branch (if new) and returns its index.
func (b *IndexBuilder) addBranch(br, v string) int {
	for i, b := range b.repo.Branches {
		if br == b.Name {
			return i
		}
	}
	b.repo.Branches = append(b.repo.Branches, RepositoryBranch{
		Name:    br,
		Version: v,
	})
	return len(b.repo.Branches) - 1
}

// AddBranch registers a branch name.  The first is assumed to be the
// default.
func (b *IndexBuilder) AddBranch(br, version string) error {
	if len(b.subRepoMap) > 0 {
		return fmt.Errorf("must add branches before sub repositories")
	}
	idx := b.addBranch(br, version)
	if idx >= 32 {
		return fmt.Errorf("branch %q: branch counts are limited to 32")
	}
	return nil
}

const maxTrigramCount = 20000
const maxLineSize = 1000

// IsText returns false if the given contents are probably not source texts.
func IsText(content []byte) bool {
	if len(content) < ngramSize {
		return true
	}

	trigrams := map[ngram]struct{}{}

	lineSize := 0
	for i := 0; i < len(content)-ngramSize; i++ {
		if content[i] == 0 {
			return false
		}
		if content[i] == '\n' {
			lineSize = 0
		} else {
			lineSize++
		}
		if lineSize > maxLineSize {
			return false
		}

		trigrams[bytesToNGram(content[i:i+ngramSize])] = struct{}{}

		if len(trigrams) > maxTrigramCount {
			// probably not text.
			return false
		}
	}
	return true
}

func (b *IndexBuilder) populateSubRepoIndices() {
	if b.subRepoIndices != nil {
		return
	}
	b.subRepoMap[""] = &b.repo
	var paths []string
	for k := range b.subRepoMap {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	b.subRepoIndices = make(map[string]uint32, len(paths))
	for i, p := range paths {
		b.subRepoIndices[p] = uint32(i)
	}
}

// Add a file which only occurs in certain branches. The document
// should be checked for sanity with IsText first.
func (b *IndexBuilder) Add(doc Document) error {
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

	b.populateSubRepoIndices()
	if doc.SubRepositoryPath != "" {
		rel, err := filepath.Rel(doc.SubRepositoryPath, doc.Name)
		if err != nil || rel == doc.Name {
			return fmt.Errorf("path %q must start subrepo path %q", doc.Name, doc.SubRepositoryPath)
		}
	}

	i, ok := b.subRepoIndices[doc.SubRepositoryPath]
	if !ok {
		return fmt.Errorf("unknown subrepo path %q", doc.SubRepositoryPath)
	}

	b.subRepos = append(b.subRepos, i)

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
