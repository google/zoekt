package zoekt

import (
	"fmt"
	"log"
)

var _ = log.Println

// An expression tree coupled with matches
type matchTree interface {
	// returns whether this matches, and if we are sure.
	matches() (match bool, sure bool)
	String() string
}

type andMatchTree struct {
	children []matchTree
}

func (t *andMatchTree) String() string {
	return fmt.Sprintf("and%v", t.children)
}

func (t *orMatchTree) String() string {
	return fmt.Sprintf("and%v", t.children)
}

func (t *notMatchTree) String() string {
	return fmt.Sprintf("not(%v)", t.child)
}

func (t *substrMatchTree) String() string {
	return fmt.Sprintf("substr(%s, %v)", t.query, t.cands)
}

func visit(t matchTree, f func(*substrMatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			visit(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			visit(ch, f)
		}
	case *notMatchTree:
		visit(s.child, f)
	case *substrMatchTree:
		f(s)
	}
}

func (p *contentProvider) evalContentMatches(s *substrMatchTree) {
	pruned := s.cands[:0]
	for _, m := range s.cands {
		if p.matchContent(m) {
			pruned = append(pruned, m)
		}
	}
	s.cands = pruned
	s.caseMatch = new(bool)
	*s.caseMatch = (len(pruned) > 0)
}

func (p *contentProvider) evalCaseMatches(s *substrMatchTree) {
	pruned := s.cands[:0]
	for _, m := range s.cands {
		if p.caseMatches(m) {
			pruned = append(pruned, m)
		}
	}
	s.cands = pruned
	s.contMatch = new(bool)
	*s.contMatch = len(pruned) > 0
}

func (t *andMatchTree) matches() (bool, bool) {
	sure := true

	for _, ch := range t.children {
		v, ok := ch.matches()
		if ok && !v {
			return false, true
		}
		if !ok {
			sure = false
		}
	}
	return true, sure
}

type orMatchTree struct {
	children []matchTree
}

func (t *orMatchTree) matches() (bool, bool) {
	sure := false
	res := false
	var newCh []matchTree
	for _, ch := range t.children {
		v, ok := t.matches()
		if ok {
			if v {
				res = res || true
				sure = true
			} else {
				continue
			}
		}

		newCh = append(newCh, ch)
	}
	t.children = newCh
	return false, sure
}

type notMatchTree struct {
	child matchTree
}

func (t *notMatchTree) matches() (bool, bool) {
	v, ok := t.child.matches()
	return !v, ok
}

type substrMatchTree struct {
	query     *SubstringQuery
	caseMatch *bool
	contMatch *bool
	cands     []*candidateMatch
}

func (t *substrMatchTree) matches() (bool, bool) {
	if len(t.cands) == 0 {
		return false, true
	}
	sure := true
	val := true
	if t.caseMatch != nil {
		val = *t.caseMatch && val
	} else {
		sure = false
	}
	if t.contMatch != nil {
		val = *t.contMatch && val
	} else {
		sure = false
	}

	return val, sure
}

func newMatchTree(q Query, mc *mergedCandidateMatch) matchTree {
	switch s := q.(type) {
	case *AndQuery:
		var r []matchTree
		for _, ch := range s.Children {
			r = append(r, newMatchTree(ch, mc))
		}
		return &andMatchTree{r}
	case *OrQuery:
		var r []matchTree
		for _, ch := range s.Children {
			r = append(r, newMatchTree(ch, mc))
		}
		return &andMatchTree{r}
	case *NotQuery:
		return &notMatchTree{
			child: newMatchTree(s.Child, mc),
		}
	case *SubstringQuery:
		return &substrMatchTree{
			query: s,
			cands: mc.matches[s],
		}
	}
	return nil
}

func (d *indexData) Search(q Query) (*SearchResult, error) {
	atoms := extractSubstringQueries(q)
	var res SearchResult
	var iters []*docIterator
	for _, atom := range atoms {
		// TODO - postingsCache
		i, err := d.getDocIterator(atom)
		if err != nil {
			return nil, err
		}
		iters = append(iters, i)
	}

	// TODO merge mergeCandidates and following loop.
	cands := mergeCandidates(iters, &res.Stats)

nextFileMatch:
	for _, c := range cands {
		// TODO - this creates a bunch of garbage that we
		// could do without.
		matchTree := newMatchTree(q, &c)

		cp := contentProvider{
			reader: d.reader,
			id:     d,
			idx:    c.fileID,
			stats:  &res.Stats,
		}

		visit(matchTree, cp.evalCaseMatches)
		if v, ok := matchTree.matches(); ok && !v {
			continue nextFileMatch
		}

		visit(matchTree, cp.evalContentMatches)
		if v, ok := matchTree.matches(); !ok {
			panic("did not decide")
		} else if !v {
			continue nextFileMatch
		}

		fMatch := FileMatch{
			Name: d.fileName(c.fileID),
			Rank: int(c.fileID),
		}

		foundContentMatch := false
		visit(matchTree, func(s *substrMatchTree) {
			for _, c := range s.cands {
				fMatch.Matches = append(fMatch.Matches, cp.fillMatch(c))
				if !c.query.FileName {
					foundContentMatch = true
				}
			}
		})

		if foundContentMatch {
			trimmed := fMatch.Matches[:0]
			for _, m := range fMatch.Matches {
				if !m.FileName {
					trimmed = append(trimmed, m)
				}
			}
			fMatch.Matches = trimmed
		}

		sortMatches(fMatch.Matches)
		res.Files = append(res.Files, fMatch)
		res.Stats.MatchCount += len(fMatch.Matches)
		res.Stats.FileCount++
	}

	return &res, nil
}

func extractSubstringQueries(q Query) []*SubstringQuery {
	var r []*SubstringQuery
	switch s := q.(type) {
	case *AndQuery:
		for _, ch := range s.Children {
			r = append(r, extractSubstringQueries(ch)...)
		}
	case *OrQuery:
		for _, ch := range s.Children {
			r = append(r, extractSubstringQueries(ch)...)
		}
	case *NotQuery:
		r = append(r, extractSubstringQueries(s.Child)...)
	case *SubstringQuery:
		r = append(r, s)
	}
	return r
}
