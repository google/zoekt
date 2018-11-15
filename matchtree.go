// Copyright 2018 Google Inc. All rights reserved.
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
	"log"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/zoekt/matchtree"
	"github.com/google/zoekt/query"
)

type docMatchTree struct {
	// mutable
	docs    []uint32
	current []uint32
}

type regexpMatchTree struct {
	regexp *regexp.Regexp

	fileName bool

	// mutable
	reEvaluated bool
	found       []*candidateMatch

	// nextDoc, prepare.
	matchtree.All
}

type substrMatchTree struct {
	matchIterator

	query         *query.Substring
	caseSensitive bool
	fileName      bool

	// mutable
	current       []*candidateMatch
	contEvaluated bool
}

type branchQueryMatchTree struct {
	fileMasks []uint64
	mask      uint64

	// mutable
	firstDone bool
	docID     uint32
}

// all prepare methods

func (t *docMatchTree) Prepare(doc uint32) {
	for len(t.docs) > 0 && t.docs[0] < doc {
		t.docs = t.docs[1:]
	}
	i := 0
	for ; i < len(t.docs) && t.docs[i] == doc; i++ {
	}

	t.current = t.docs[:i]
	t.docs = t.docs[i:]
}

func (t *regexpMatchTree) Prepare(doc uint32) {
	t.found = t.found[:0]
	t.reEvaluated = false
	t.All.Prepare(doc)
}

func (t *substrMatchTree) Prepare(nextDoc uint32) {
	t.matchIterator.Prepare(nextDoc)
	t.current = t.matchIterator.candidates()
	t.contEvaluated = false
}

func (t *branchQueryMatchTree) Prepare(doc uint32) {
	t.firstDone = true
	t.docID = doc
}

// nextDoc

func (t *docMatchTree) NextDoc() uint32 {
	if len(t.docs) == 0 {
		return maxUInt32
	}
	return t.docs[0]
}

func (t *branchQueryMatchTree) NextDoc() uint32 {
	var start uint32
	if t.firstDone {
		start = t.docID + 1
	}

	for i := start; i < uint32(len(t.fileMasks)); i++ {
		if (t.mask & t.fileMasks[i]) != 0 {
			return i
		}
	}
	return maxUInt32
}

// all String methods

func (t *docMatchTree) String() string {
	return fmt.Sprintf("docs%v", t.docs)
}

func (t *regexpMatchTree) String() string {
	return fmt.Sprintf("re(%s)", t.regexp)
}

func (t *substrMatchTree) String() string {
	f := ""
	if t.fileName {
		f = "f"
	}

	return fmt.Sprintf("%ssubstr(%q, %v, %v)", f, t.query.Pattern, t.current, t.matchIterator)
}

func (t *branchQueryMatchTree) String() string {
	return fmt.Sprintf("branch(%x)", t.mask)
}

// all matches() methods.

func (t *docMatchTree) Matches(cp matchtree.ContentProvider, cost int, known map[matchtree.MatchTree]bool) (bool, bool) {
	return len(t.current) > 0, true
}

func (t *branchQueryMatchTree) Matches(cp matchtree.ContentProvider, cost int, known map[matchtree.MatchTree]bool) (bool, bool) {
	return t.fileMasks[t.docID]&t.mask != 0, true
}

func (t *regexpMatchTree) Matches(cp matchtree.ContentProvider, cost int, known map[matchtree.MatchTree]bool) (bool, bool) {
	if cost < matchtree.CostRegexp {
		return false, false
	}

	idxs := t.regexp.FindAllIndex(cp.Data(t.fileName), -1)
	t.found = make([]*candidateMatch, 0, len(idxs))
	for _, idx := range idxs {
		t.found = append(t.found, &candidateMatch{
			byteOffset:  uint32(idx[0]),
			byteMatchSz: uint32(idx[1] - idx[0]),
			fileName:    t.fileName,
		})
	}

	return len(t.found) > 0, true
}

func (t *substrMatchTree) Matches(cp matchtree.ContentProvider, cost int, known map[matchtree.MatchTree]bool) (bool, bool) {
	if len(t.current) == 0 {
		return false, true
	}

	if t.fileName && cost < matchtree.CostMemory {
		return false, false
	}

	if !t.fileName && cost < matchtree.CostContent {
		return false, false
	}

	pruned := t.current[:0]
	for _, m := range t.current {
		if m.byteOffset == 0 && m.runeOffset > 0 {
			m.byteOffset = cp.(*contentProvider).findOffset(m.fileName, m.runeOffset)
		}
		if m.matchContent(cp.Data(m.fileName)) {
			pruned = append(pruned, m)
		}
	}
	t.current = pruned

	return len(t.current) > 0, true
}

func (d *indexData) newMatchTree(q query.Q) (matchtree.MatchTree, error) {
	atom := func(q query.Q) (matchtree.MatchTree, error) {
		switch s := q.(type) {
		case *query.Regexp:
			subQ := query.RegexpToQuery(s.Regexp, ngramSize)
			subQ = query.Map(subQ, func(q query.Q) query.Q {
				if sub, ok := q.(*query.Substring); ok {
					sub.FileName = s.FileName
					sub.CaseSensitive = s.CaseSensitive
				}
				return q
			})

			subMT, err := d.newMatchTree(subQ)
			if err != nil {
				return nil, err
			}

			prefix := ""
			if !s.CaseSensitive {
				prefix = "(?i)"
			}

			tr := &regexpMatchTree{
				regexp:   regexp.MustCompile(prefix + s.Regexp.String()),
				fileName: s.FileName,
			}

			return matchtree.And(tr, &matchtree.NoVisit{subMT}), nil

		case *query.Substring:
			return d.newSubstringMatchTree(s)

		case *query.Branch:
			mask := uint64(0)
			if s.Pattern == "HEAD" {
				mask = 1
			} else {
				for nm, m := range d.branchIDs {
					if strings.Contains(nm, s.Pattern) {
						mask |= uint64(m)
					}
				}
			}
			return &branchQueryMatchTree{
				mask:      mask,
				fileMasks: d.fileBranchMasks,
			}, nil
		case *query.Language:
			code, ok := d.metaData.LanguageMap[s.Language]
			if !ok {
				return &matchtree.None{"lang"}, nil
			}
			docs := make([]uint32, 0, len(d.languages))
			for d, l := range d.languages {
				if l == code {
					docs = append(docs, uint32(d))
				}
			}
			return &docMatchTree{
				docs: docs,
			}, nil

		case *query.Symbol:
			mt, err := d.newSubstringMatchTree(s.Atom)
			if err != nil {
				return nil, err
			}

			if _, ok := mt.(*regexpMatchTree); ok {
				return nil, fmt.Errorf("regexps and short queries not implemented for symbol search")
			}
			subMT, ok := mt.(*substrMatchTree)
			if !ok {
				return nil, fmt.Errorf("found %T inside query.Symbol", mt)
			}

			subMT.matchIterator = d.newTrimByDocSectionIter(s.Atom, subMT.matchIterator)
			return subMT, nil
		}
		log.Panicf("type %T", q)
		return nil, nil
	}

	return matchtree.NewMatchTree(q, atom)
}

func (d *indexData) newSubstringMatchTree(s *query.Substring) (matchtree.MatchTree, error) {
	st := &substrMatchTree{
		query:         s,
		caseSensitive: s.CaseSensitive,
		fileName:      s.FileName,
	}

	if utf8.RuneCountInString(s.Pattern) < ngramSize {
		prefix := ""
		if !s.CaseSensitive {
			prefix = "(?i)"
		}
		t := &regexpMatchTree{
			regexp:   regexp.MustCompile(prefix + regexp.QuoteMeta(s.Pattern)),
			fileName: s.FileName,
		}
		return t, nil
	}

	result, err := d.iterateNgrams(s)
	if err != nil {
		return nil, err
	}
	st.matchIterator = result
	return st, nil
}
