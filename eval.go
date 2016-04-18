package zoekt

import (
	"fmt"
	"log"
)

var _ = log.Println

// An expression tree coupled with matches
type matchTree interface {
	// returns whether this matches, and if we are sure.
	matches(known map[matchTree]bool) (match bool, sure bool)
	String() string
}

type andMatchTree struct {
	children []matchTree
}

type orMatchTree struct {
	children []matchTree
}

type notMatchTree struct {
	child matchTree
}

type substrMatchTree struct {
	query     *SubstringQuery
	current   []*candidateMatch
	caseMatch *bool
	contMatch *bool
	cands     []*candidateMatch
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
	return fmt.Sprintf("substr(%s, %v)", t.query, t.current)
}

func collectPositiveSubstrings(t matchTree, f func(*substrMatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			collectPositiveSubstrings(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			collectPositiveSubstrings(ch, f)
		}
	case *notMatchTree:
	case *substrMatchTree:
		f(s)
	}
}
func visitMatches(t matchTree, known map[matchTree]bool,
	f func(*substrMatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			if known[ch] {
				visitMatches(ch, known, f)
			}
		}
	case *orMatchTree:
		for _, ch := range s.children {
			if known[ch] {
				visitMatches(ch, known, f)
			}
		}
	case *notMatchTree:
		// don't collect into negative trees.
	case *substrMatchTree:
		f(s)
	}
}

func (p *contentProvider) evalContentMatches(s *substrMatchTree) {
	pruned := s.current[:0]
	for _, m := range s.current {
		if p.matchContent(m) {
			pruned = append(pruned, m)
		}
	}
	s.current = pruned
	s.contMatch = new(bool)
	*s.contMatch = (len(pruned) > 0)
}

func (p *contentProvider) evalCaseMatches(s *substrMatchTree) {
	pruned := s.current[:0]
	for _, m := range s.current {
		if p.caseMatches(m) {
			pruned = append(pruned, m)
		}
	}
	s.current = pruned
	s.caseMatch = new(bool)
	*s.caseMatch = len(pruned) > 0
}

func (t *andMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	sure := true

	for _, ch := range t.children {
		v, ok := evalMatchTree(known, ch)
		if ok && !v {
			return false, true
		}
		if !ok {
			sure = false
		}
	}
	return true, sure
}

func (t *orMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	sure := true
	for _, ch := range t.children {
		v, ok := evalMatchTree(known, ch)
		if ok {
			if v {
				return true, true
			}
		} else {
			sure = false
		}
	}
	return false, sure
}

func evalMatchTree(known map[matchTree]bool, mt matchTree) (bool, bool) {
	if v, ok := known[mt]; ok {
		return v, true
	}

	v, ok := mt.matches(known)
	if ok {
		known[mt] = v
	}

	return v, ok
}

func (t *notMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	v, ok := evalMatchTree(known, t.child)
	return !v, ok
}

func (t *substrMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	if len(t.current) == 0 {
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

func (d *indexData) newMatchTree(q Query, sq map[*SubstringQuery]*substrMatchTree) (matchTree, error) {
	switch s := q.(type) {
	case *AndQuery:
		var r []matchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, sq)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &andMatchTree{r}, nil
	case *OrQuery:
		var r []matchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, sq)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &andMatchTree{r}, nil
	case *NotQuery:
		ct, err := d.newMatchTree(s.Child, sq)
		return &notMatchTree{
			child: ct,
		}, err
	case *SubstringQuery:
		iter, err := d.getDocIterator(s)
		if err != nil {
			return nil, err
		}
		st := &substrMatchTree{
			query: s,
			cands: iter.next(),
		}
		sq[s] = st
		return st, nil
	}
	panic("type")
	return nil, nil
}

func (d *indexData) Search(q Query) (*SearchResult, error) {
	var res SearchResult

	atoms := map[*SubstringQuery]*substrMatchTree{}
	mt, err := d.newMatchTree(q, atoms)
	if err != nil {
		return nil, err
	}

	for _, st := range atoms {
		res.Stats.NgramMatches += len(st.cands)
	}

	var positiveAtoms, fileAtoms []*substrMatchTree
	collectPositiveSubstrings(mt, func(sq *substrMatchTree) {
		positiveAtoms = append(positiveAtoms, sq)
	})
	for _, st := range atoms {
		if st.query.FileName {
			fileAtoms = append(fileAtoms, st)
		}
	}

nextFileMatch:
	for {
		var nextDoc uint32
		nextDoc = maxUInt32
		for _, st := range atoms {
			st.current = nil
			st.contMatch = nil
			st.caseMatch = nil
		}

		for _, st := range positiveAtoms {
			if len(st.cands) > 0 && st.cands[0].file < nextDoc {
				nextDoc = st.cands[0].file
			}
		}

		if nextDoc == maxUInt32 {
			break
		}

		res.Stats.FilesConsidered++
		for _, st := range atoms {
			for len(st.cands) > 0 && st.cands[0].file < nextDoc {
				st.cands = st.cands[1:]
			}

			i := 0
			for ; i < len(st.cands) && st.cands[i].file == nextDoc; i++ {
			}
			st.current = st.cands[:i]
			st.cands = st.cands[i:]
		}

		cp := contentProvider{
			reader: d.reader,
			id:     d,
			idx:    nextDoc,
			stats:  &res.Stats,
		}

		known := make(map[matchTree]bool)
		if v, ok := evalMatchTree(known, mt); ok && !v {
			continue nextFileMatch
		}

		// Files are cheap to match. Do them first.
		if len(fileAtoms) > 0 {
			for _, st := range fileAtoms {
				cp.evalCaseMatches(st)
				cp.evalContentMatches(st)
			}
			if v, ok := evalMatchTree(known, mt); ok && !v {
				continue nextFileMatch
			}
		}

		for _, st := range atoms {
			cp.evalCaseMatches(st)
		}

		if v, ok := evalMatchTree(known, mt); ok && !v {
			continue nextFileMatch
		}

		for _, st := range atoms {
			// TODO - this may evaluate too much.
			cp.evalContentMatches(st)
		}
		if v, ok := evalMatchTree(known, mt); !ok {
			panic("did not decide")
		} else if !v {
			continue nextFileMatch
		}

		fileMatch := FileMatch{
			Name: d.fileName(nextDoc),
			// Maintain ordering of input files. This
			// strictly dominates the in-file ordering of
			// the matches.
			Score: 10 * float64(nextDoc) / float64(len(d.boundaries)),
		}

		foundContentMatch := false
		visitMatches(mt, known, func(s *substrMatchTree) {
			for _, c := range s.current {
				fileMatch.Matches = append(fileMatch.Matches, cp.fillMatch(c))
				if !c.query.FileName {
					foundContentMatch = true
				}
			}
		})

		if foundContentMatch {
			trimmed := fileMatch.Matches[:0]
			for _, m := range fileMatch.Matches {
				if !m.FileName {
					trimmed = append(trimmed, m)
				}
			}
			fileMatch.Matches = trimmed
		}

		maxFileScore := 0.0
		for i := range fileMatch.Matches {
			if maxFileScore < fileMatch.Matches[i].Score {
				maxFileScore = fileMatch.Matches[i].Score
			}

			// Order by ordering in file.
			fileMatch.Matches[i].Score += 1.0 - (float64(i) / float64(len(fileMatch.Matches)))
		}
		fileMatch.Score += maxFileScore

		sortMatchesByScore(fileMatch.Matches)
		res.Files = append(res.Files, fileMatch)
		res.Stats.MatchCount += len(fileMatch.Matches)
		res.Stats.FileCount++
	}
	sortFilesByScore(res.Files)
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
