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

package matchtree

import (
	"fmt"
	"log"

	"github.com/google/zoekt/query"
)

// ContentProvider is an abstraction to treat matches for names and content
// with the same code.
type ContentProvider interface {
	Data(fileName bool) []byte
}

// A DocIterator iterates over documents in order.
type DocIterator interface {
	// provide the next document where we can may find something
	// interesting.
	NextDoc() uint32

	// clears any per-document state of the docIterator, and
	// prepares for evaluating the given doc. The argument is
	// strictly increasing over time.
	Prepare(nextDoc uint32)
}

const CostConst = 0
const CostMemory = 1
const CostContent = 2
const CostRegexp = 3

const CostMin = CostConst
const CostMax = CostRegexp

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

type BruteForceMatchTree struct {
	// mutable
	firstDone bool
	docID     uint32
}

// NoMatchTree is a MatchTree that matches nothing.
type NoMatchTree struct {
	Why string
}

type AndMatchTree struct {
	Children []MatchTree
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
type NoVisitMatchTree struct {
	MatchTree
}

// all prepare methods

func (t *BruteForceMatchTree) Prepare(doc uint32) {
	t.docID = doc
	t.firstDone = true
}

func (t *NoMatchTree) Prepare(uint32) {}

func (t *AndMatchTree) Prepare(doc uint32) {
	for _, c := range t.Children {
		c.Prepare(doc)
	}
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

// nextDoc

func (t *BruteForceMatchTree) NextDoc() uint32 {
	if !t.firstDone {
		return 0
	}
	return t.docID + 1
}

func (t *NoMatchTree) NextDoc() uint32 {
	return maxUInt32
}

func (t *AndMatchTree) NextDoc() uint32 {
	var max uint32
	for _, c := range t.Children {
		m := c.NextDoc()
		if m > max {
			max = m
		}
	}
	return max
}

const maxUInt32 = 0xffffffff

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

// all String methods

func (t *BruteForceMatchTree) String() string {
	return "all"
}

func (t *NoMatchTree) String() string {
	return fmt.Sprintf("not(%q)", t.Why)
}

func (t *AndMatchTree) String() string {
	return fmt.Sprintf("and%v", t.Children)
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

// Visit the matchTree. Skips noVisitMatchTree
func VisitMatchTree(t MatchTree, f func(MatchTree)) {
	switch s := t.(type) {
	case *AndMatchTree:
		for _, ch := range s.Children {
			VisitMatchTree(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			VisitMatchTree(ch, f)
		}
	case *NoVisitMatchTree:
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
	case *AndMatchTree:
		for _, ch := range s.Children {
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
	case *NoVisitMatchTree:
		// don't collect into negative trees.
	case *fileNameMatchTree:
		// We will just gather the filename if we do not visit this tree.
	default:
		f(s)
	}
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

// all matches() methods.

func (t *BruteForceMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return true, true
}

func (t *NoMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return false, true
}

func (t *AndMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	sure := true

	for _, ch := range t.Children {
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

func (t *notMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	v, ok := EvalMatchTree(cp, cost, known, t.child)
	return !v, ok
}

func (t *fileNameMatchTree) Matches(cp ContentProvider, cost int, known map[MatchTree]bool) (bool, bool) {
	return EvalMatchTree(cp, cost, known, t.child)
}

func NewMatchTree(q query.Q, atom func(q query.Q) (MatchTree, error)) (MatchTree, error) {
	switch s := q.(type) {
	case *query.And:
		var r []MatchTree
		for _, ch := range s.Children {
			ct, err := NewMatchTree(ch, atom)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &AndMatchTree{r}, nil
	case *query.Or:
		var r []MatchTree
		for _, ch := range s.Children {
			ct, err := NewMatchTree(ch, atom)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &orMatchTree{r}, nil
	case *query.Not:
		ct, err := NewMatchTree(s.Child, atom)
		return &notMatchTree{
			child: ct,
		}, err

	case *query.Type:
		if s.Type != query.TypeFileName {
			break
		}

		ct, err := NewMatchTree(s.Child, atom)
		if err != nil {
			return nil, err
		}

		return &fileNameMatchTree{
			child: ct,
		}, nil

	case *query.Const:
		if s.Value {
			return &BruteForceMatchTree{}, nil
		} else {
			return &NoMatchTree{"const"}, nil
		}
	}

	ct, err := atom(q)
	if err != nil {
		return nil, err
	}
	if ct == nil {
		log.Panicf("type %T", q)
	}
	return ct, err
}
