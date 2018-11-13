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

	"github.com/google/zoekt/query"
)

// A DocIterator iterates over documents in order.
type DocIterator interface {
	// provide the next document where we can may find something
	// interesting.
	NextDoc() uint32

	// clears any per-document state of the docIterator, and
	// prepares for evaluating the given doc. The argument is
	// strictly increasing over time.
	Prepare(nextDoc uint32)

	// collects statistics.
	UpdateStats(stats *Stats)
}

const costConst = 0
const costMemory = 1
const costContent = 2
const costRegexp = 3

const CostMin = costConst
const CostMax = costRegexp

// An expression tree coupled with matches. The matchtree has two
// functions:
//
// * it implements boolean combinations (and, or, not)
//
// * it implements shortcuts, where we skip documents (for example: if
// there are no trigram matches, we can be sure there are no substring
// matches). The matchtree iterates over the documents as they are
// ordered in the shard.
//
// The general process for a given (shard, query) is
//
// - construct MatchTree for the query
//
// - find all different leaf matchTrees (substring, regexp, etc.)
//
// in a loop:
//
//   - find next doc to process using nextDoc
//
//   - evaluate atoms (leaf expressions that match text)
//
//   - evaluate the tree using matches(), storing the result in map.
//
//   - if the complete tree returns (matches() == true) for the document,
//     collect all text matches by looking at leaf matchTrees
//
type MatchTree interface {
	DocIterator

	// returns whether this Matches, and if we are sure.
	Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (match bool, sure bool)
}

type docMatchTree struct {
	// mutable
	docs    []uint32
	current []uint32
}

type bruteForceMatchTree struct {
	// mutable
	firstDone bool
	docID     uint32
}

type andMatchTree struct {
	children []MatchTree
}

type orMatchTree struct {
	children []MatchTree
}

type notMatchTree struct {
	child MatchTree
}

type fileNameMatchTree struct {
	child MatchTree
}

// Don't visit this subtree for collecting matches.
type noVisitMatchTree struct {
	MatchTree
}

type regexpMatchTree struct {
	regexp *regexp.Regexp

	fileName bool

	// mutable
	reEvaluated bool
	found       []*candidateMatch

	// nextDoc, prepare.
	bruteForceMatchTree
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

func (t *noMatchTree) UpdateStats(s *Stats) {
}

func (t *bruteForceMatchTree) UpdateStats(s *Stats) {
}

func (t *docMatchTree) UpdateStats(s *Stats) {
}

func (t *andMatchTree) UpdateStats(s *Stats) {
	for _, c := range t.children {
		c.UpdateStats(s)
	}
}

func (t *orMatchTree) UpdateStats(s *Stats) {
	for _, c := range t.children {
		c.UpdateStats(s)
	}
}

func (t *notMatchTree) UpdateStats(s *Stats) {
	t.child.UpdateStats(s)
}

func (t *fileNameMatchTree) UpdateStats(s *Stats) {
	t.child.UpdateStats(s)
}

func (t *branchQueryMatchTree) UpdateStats(s *Stats) {
}

func (t *regexpMatchTree) UpdateStats(s *Stats) {
}

// all prepare methods

func (t *bruteForceMatchTree) Prepare(doc uint32) {
	t.docID = doc
	t.firstDone = true
}

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

func (t *andMatchTree) Prepare(doc uint32) {
	for _, c := range t.children {
		c.Prepare(doc)
	}
}

func (t *regexpMatchTree) Prepare(doc uint32) {
	t.found = t.found[:0]
	t.reEvaluated = false
	t.bruteForceMatchTree.Prepare(doc)
}

func (t *orMatchTree) Prepare(doc uint32) {
	for _, c := range t.children {
		c.Prepare(doc)
	}
}

func (t *notMatchTree) Prepare(doc uint32) {
	t.child.Prepare(doc)
}

func (t *fileNameMatchTree) Prepare(doc uint32) {
	t.child.Prepare(doc)
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

func (t *bruteForceMatchTree) NextDoc() uint32 {
	if !t.firstDone {
		return 0
	}
	return t.docID + 1
}

func (t *andMatchTree) NextDoc() uint32 {
	var max uint32
	for _, c := range t.children {
		m := c.NextDoc()
		if m > max {
			max = m
		}
	}
	return max
}

func (t *orMatchTree) NextDoc() uint32 {
	min := uint32(maxUInt32)
	for _, c := range t.children {
		m := c.NextDoc()
		if m < min {
			min = m
		}
	}
	return min
}

func (t *notMatchTree) NextDoc() uint32 {
	return 0
}

func (t *fileNameMatchTree) NextDoc() uint32 {
	return t.child.NextDoc()
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

func (t *bruteForceMatchTree) String() string {
	return "all"
}

func (t *docMatchTree) String() string {
	return fmt.Sprintf("docs%v", t.docs)
}

func (t *andMatchTree) String() string {
	return fmt.Sprintf("and%v", t.children)
}

func (t *regexpMatchTree) String() string {
	return fmt.Sprintf("re(%s)", t.regexp)
}

func (t *orMatchTree) String() string {
	return fmt.Sprintf("or%v", t.children)
}

func (t *notMatchTree) String() string {
	return fmt.Sprintf("not(%v)", t.child)
}

func (t *fileNameMatchTree) String() string {
	return fmt.Sprintf("f(%v)", t.child)
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

// Visit the matchTree. Skips noVisitMatchTree
func VisitMatchTree(t MatchTree, f func(MatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			VisitMatchTree(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			VisitMatchTree(ch, f)
		}
	case *noVisitMatchTree:
		VisitMatchTree(s.MatchTree, f)
	case *notMatchTree:
		VisitMatchTree(s.child, f)
	case *fileNameMatchTree:
		VisitMatchTree(s.child, f)
	default:
		f(t)
	}
}

func VisitMatches(t MatchTree, known map[MatchTree]bool, f func(MatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			if known[ch] {
				VisitMatches(ch, known, f)
			}
		}
	case *orMatchTree:
		for _, ch := range s.children {
			if known[ch] {
				VisitMatches(ch, known, f)
			}
		}
	case *notMatchTree:
	case *noVisitMatchTree:
		// don't collect into negative trees.
	case *fileNameMatchTree:
		// We will just gather the filename if we do not visit this tree.
	default:
		f(s)
	}
}

func visitSubtreeMatches(t MatchTree, known map[MatchTree]bool, f func(*substrMatchTree)) {
	VisitMatches(t, known, func(mt MatchTree) {
		st, ok := mt.(*substrMatchTree)
		if ok {
			f(st)
		}
	})
}

func visitRegexMatches(t MatchTree, known map[MatchTree]bool, f func(*regexpMatchTree)) {
	VisitMatches(t, known, func(mt MatchTree) {
		st, ok := mt.(*regexpMatchTree)
		if ok {
			f(st)
		}
	})
}

// all matches() methods.

func (t *docMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return len(t.current) > 0, true
}

func (t *bruteForceMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return true, true
}

func (t *andMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	sure := true

	for _, ch := range t.children {
		v, ok := EvalMatchTree(cp, cost, known, ch)
		if ok && !v {
			return false, true
		}
		if !ok {
			sure = false
		}
	}
	return true, sure
}

func (t *orMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	matches := false
	sure := true
	for _, ch := range t.children {
		v, ok := EvalMatchTree(cp, cost, known, ch)
		if ok {
			// we could short-circuit, but we want to use
			// the other possibilities as a ranking
			// signal.
			matches = matches || v
		} else {
			sure = false
		}
	}
	return matches, sure
}

func (t *branchQueryMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return t.fileMasks[t.docID]&t.mask != 0, true
}

func (t *regexpMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	if cost < costRegexp {
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

func EvalMatchTree(cp ContentProvider, cost int, known map[MatchTree]bool, mt MatchTree) (bool, bool) {
	if v, ok := known[mt]; ok {
		return v, true
	}

	v, ok := mt.Matches(cp, cost, known)
	if ok {
		known[mt] = v
	}

	return v, ok
}

func (t *notMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	v, ok := EvalMatchTree(cp, cost, known, t.child)
	return !v, ok
}

func (t *fileNameMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return EvalMatchTree(cp, cost, known, t.child)
}

func (t *substrMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	if len(t.current) == 0 {
		return false, true
	}

	if t.fileName && cost < costMemory {
		return false, false
	}

	if !t.fileName && cost < costContent {
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

func (d *indexData) newMatchTree(q query.Q, stats *Stats) (MatchTree, error) {
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

		subMT, err := d.newMatchTree(subQ, stats)
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

		return &andMatchTree{
			children: []MatchTree{
				tr, &noVisitMatchTree{subMT},
			},
		}, nil
	case *query.And:
		var r []MatchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, stats)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &andMatchTree{r}, nil
	case *query.Or:
		var r []MatchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, stats)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &orMatchTree{r}, nil
	case *query.Not:
		ct, err := d.newMatchTree(s.Child, stats)
		return &notMatchTree{
			child: ct,
		}, err

	case *query.Type:
		if s.Type != query.TypeFileName {
			break
		}

		ct, err := d.newMatchTree(s.Child, stats)
		if err != nil {
			return nil, err
		}

		return &fileNameMatchTree{
			child: ct,
		}, nil

	case *query.Substring:
		return d.newSubstringMatchTree(s, stats)

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
	case *query.Const:
		if s.Value {
			return &bruteForceMatchTree{}, nil
		} else {
			return &noMatchTree{"const"}, nil
		}
	case *query.Language:
		code, ok := d.metaData.LanguageMap[s.Language]
		if !ok {
			return &noMatchTree{"lang"}, nil
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
		mt, err := d.newSubstringMatchTree(s.Atom, stats)
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

func (d *indexData) newSubstringMatchTree(s *query.Substring, stats *Stats) (MatchTree, error) {
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
