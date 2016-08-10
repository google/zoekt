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
	"log"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/context"

	"github.com/google/zoekt/query"
)

var _ = log.Println

// An expression tree coupled with matches
type matchTree interface {
	// returns whether this matches, and if we are sure.
	matches(known map[matchTree]bool) (match bool, sure bool)

	// clears any per-document state of the matchTree, and
	// prepares for evaluating the given doc. The argument is
	// strictly increasing over time.
	prepare(nextDoc uint32)
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

type regexpMatchTree struct {
	query  *query.Regexp
	regexp *regexp.Regexp

	child    matchTree
	fileName bool

	// mutable
	reEvaluated bool
	found       []*candidateMatch
}

type substrMatchTree struct {
	query         *query.Substring
	cands         []*candidateMatch
	coversContent bool
	caseSensitive bool
	fileName      bool

	// mutable
	current       []*candidateMatch
	contEvaluated bool
}

type branchQueryMatchTree struct {
	fileMasks []uint32
	mask      uint32

	// mutable
	docID uint32
}

// prepare
func (t *andMatchTree) prepare(doc uint32) {
	for _, c := range t.children {
		c.prepare(doc)
	}
}

func (t *regexpMatchTree) prepare(doc uint32) {
	t.found = t.found[:0]
	t.reEvaluated = false
	t.child.prepare(doc)
}

func (t *orMatchTree) prepare(doc uint32) {
	for _, c := range t.children {
		c.prepare(doc)
	}
}

func (t *notMatchTree) prepare(doc uint32) {
	t.child.prepare(doc)
}

func (t *substrMatchTree) prepare(nextDoc uint32) {
	for len(t.cands) > 0 && t.cands[0].file < nextDoc {
		t.cands = t.cands[1:]
	}

	i := 0
	for ; i < len(t.cands) && t.cands[i].file == nextDoc; i++ {
	}
	t.current = t.cands[:i]
	t.cands = t.cands[i:]
	t.contEvaluated = false
}

func (t *branchQueryMatchTree) prepare(doc uint32) {
	t.docID = doc
}

// String.

func (t *andMatchTree) String() string {
	return fmt.Sprintf("and%v", t.children)
}

func (t *regexpMatchTree) String() string {
	return fmt.Sprintf("re(%s,%s)", t.regexp, t.child)
}

func (t *orMatchTree) String() string {
	return fmt.Sprintf("or%v", t.children)
}

func (t *notMatchTree) String() string {
	return fmt.Sprintf("not(%v)", t.child)
}

func (t *substrMatchTree) String() string {
	f := ""
	if t.fileName {
		f = "f"
	}

	return fmt.Sprintf("%ssubstr(%q,%v)", f, t.query.Pattern, t.current)
}

func (t *branchQueryMatchTree) String() string {
	return fmt.Sprintf("branch(%x)", t.mask)
}

func collectAtoms(t matchTree, f func(matchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			collectAtoms(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			collectAtoms(ch, f)
		}
	default:
		f(t)
	}
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
	case *regexpMatchTree:
		collectPositiveSubstrings(s.child, f)
	case *notMatchTree:
	case *substrMatchTree:
		f(s)
	}
}

func collectRegexps(t matchTree, f func(*regexpMatchTree)) {
	switch s := t.(type) {
	case *andMatchTree:
		for _, ch := range s.children {
			collectRegexps(ch, f)
		}
	case *orMatchTree:
		for _, ch := range s.children {
			collectRegexps(ch, f)
		}
	case *notMatchTree:
		collectRegexps(s.child, f)
	case *regexpMatchTree:
		f(s)
	}
}

func visitMatches(t matchTree, known map[matchTree]bool, f func(matchTree)) {
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
	default:
		f(s)
	}
}

func visitSubtreeMatches(t matchTree, known map[matchTree]bool, f func(*substrMatchTree)) {
	visitMatches(t, known, func(mt matchTree) {
		st, ok := mt.(*substrMatchTree)
		if ok {
			f(st)
		}
	})
}

func visitRegexMatches(t matchTree, known map[matchTree]bool, f func(*regexpMatchTree)) {
	visitMatches(t, known, func(mt matchTree) {
		st, ok := mt.(*regexpMatchTree)
		if ok {
			f(st)
		}
	})
}

func (p *contentProvider) evalContentMatches(s *substrMatchTree) {
	if !s.coversContent {
		pruned := s.current[:0]
		for _, m := range s.current {
			if p.matchContent(m) {
				pruned = append(pruned, m)
			}
		}
		s.current = pruned
	}
	s.contEvaluated = true
}

func (p *contentProvider) evalRegexpMatches(s *regexpMatchTree) {
	idxs := s.regexp.FindAllIndex(p.data(s.fileName), -1)
	for _, idx := range idxs {
		s.found = append(s.found, &candidateMatch{
			offset:   uint32(idx[0]),
			matchSz:  uint32(idx[1] - idx[0]),
			fileName: s.fileName,
		})
	}
	s.reEvaluated = true
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
	matches := false
	sure := true
	for _, ch := range t.children {
		v, ok := evalMatchTree(known, ch)
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

func (t *branchQueryMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	return t.fileMasks[t.docID]&t.mask != 0, true
}

func (t *regexpMatchTree) matches(known map[matchTree]bool) (bool, bool) {
	v, ok := evalMatchTree(known, t.child)
	if ok && !v {
		return false, true
	}

	if !t.reEvaluated {
		return false, false
	}

	return len(t.found) > 0, true
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

	sure := (t.coversContent || t.contEvaluated)
	return true, sure
}

func (d *indexData) newMatchTree(q query.Q, sq map[*substrMatchTree]struct{}) (matchTree, error) {
	switch s := q.(type) {
	case *query.Regexp:
		sz := ngramSize
		if s.FileName {
			sz = 1
		}
		subQ := query.RegexpToQuery(s.Regexp, sz)
		subQ = query.Map(subQ, func(q query.Q) query.Q {
			if sub, ok := q.(*query.Substring); ok {
				sub.FileName = s.FileName
				sub.CaseSensitive = s.CaseSensitive
			}
			return q
		})

		subMT, err := d.newMatchTree(subQ, sq)
		if err != nil {
			return nil, err
		}

		prefix := ""
		if !s.CaseSensitive {
			prefix = "(?i)"
		}

		tr := &regexpMatchTree{
			regexp:   regexp.MustCompile(prefix + s.Regexp.String()),
			child:    subMT,
			fileName: s.FileName,
		}
		return tr, nil
	case *query.And:
		var r []matchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, sq)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &andMatchTree{r}, nil
	case *query.Or:
		var r []matchTree
		for _, ch := range s.Children {
			ct, err := d.newMatchTree(ch, sq)
			if err != nil {
				return nil, err
			}
			r = append(r, ct)
		}
		return &orMatchTree{r}, nil
	case *query.Not:
		ct, err := d.newMatchTree(s.Child, sq)
		return &notMatchTree{
			child: ct,
		}, err
	case *query.Substring:
		iter, err := d.getDocIterator(s)
		if err != nil {
			return nil, err
		}
		st := &substrMatchTree{
			query:         s,
			caseSensitive: s.CaseSensitive,
			coversContent: iter.coversContent(),
			cands:         iter.next(),
		}
		sq[st] = struct{}{}
		return st, nil

	case *query.Branch:
		mask := uint32(0)
		for nm, m := range d.branchIDs {
			if strings.Contains(nm, s.Pattern) {
				mask |= uint32(m)
			}
		}

		return &branchQueryMatchTree{
			mask:      mask,
			fileMasks: d.fileBranchMasks,
		}, nil
	case *query.Const:
		if s.Value {
			iter := d.matchAllDocIterator()
			return &substrMatchTree{
				query:         &query.Substring{Pattern: "TRUE"},
				coversContent: true,
				caseSensitive: false,
				fileName:      true,
				cands:         iter.next(),
			}, nil
		} else {
			return &substrMatchTree{
				query: &query.Substring{Pattern: "FALSE"},
			}, nil
		}
	}
	log.Panicf("type %T", q)
	return nil, nil
}

func (d *indexData) simplify(in query.Q) query.Q {
	eval := query.Map(in, func(q query.Q) query.Q {
		if r, ok := q.(*query.Repo); ok {
			return &query.Const{strings.Contains(d.unaryData.RepoName, r.Pattern)}
		}
		return q
	})
	return query.Simplify(eval)
}

func (o *SearchOptions) SetDefaults() {
	if o.ShardMaxMatchCount == 0 {
		// We cap the total number of matches, so overly broad
		// searches don't crash the machine.
		o.ShardMaxMatchCount = 100000
	}
	if o.TotalMaxMatchCount == 0 {
		o.TotalMaxMatchCount = 10 * o.ShardMaxMatchCount
	}
	if o.ShardMaxImportantMatch == 0 {
		o.ShardMaxImportantMatch = 10
	}
	if o.TotalMaxImportantMatch == 0 {
		o.TotalMaxImportantMatch = 10 * o.ShardMaxImportantMatch
	}
}

func (d *indexData) Search(ctx context.Context, q query.Q, opts *SearchOptions) (*SearchResult, error) {
	copyOpts := *opts
	opts = &copyOpts
	opts.SetDefaults()
	importantMatchCount := 0

	var res SearchResult

	q = d.simplify(q)
	if c, ok := q.(*query.Const); ok && !c.Value {
		return &res, nil
	}

	atoms := map[*substrMatchTree]struct{}{}
	mt, err := d.newMatchTree(q, atoms)
	if err != nil {
		return nil, err
	}

	for st := range atoms {
		res.Stats.NgramMatches += len(st.cands)
	}

	var positiveAtoms, fileAtoms []*substrMatchTree
	collectPositiveSubstrings(mt, func(sq *substrMatchTree) {
		positiveAtoms = append(positiveAtoms, sq)
	})

	var regexpAtoms []*regexpMatchTree
	collectRegexps(mt, func(re *regexpMatchTree) {
		regexpAtoms = append(regexpAtoms, re)
	})

	for st := range atoms {
		if st.fileName {
			fileAtoms = append(fileAtoms, st)
		}
	}

	cp := contentProvider{
		id:    d,
		stats: &res.Stats,
	}

	totalAtomCount := 0
	collectAtoms(mt, func(t matchTree) { totalAtomCount++ })

	canceled := false

nextFileMatch:
	for {
		if !canceled {
			select {
			case <-ctx.Done():
				canceled = true
			default:
			}
		}

		var nextDoc uint32
		nextDoc = maxUInt32
		for _, st := range positiveAtoms {
			if len(st.cands) > 0 && st.cands[0].file < nextDoc {
				nextDoc = st.cands[0].file
			}
		}

		if nextDoc == maxUInt32 {
			break
		}
		res.Stats.FilesConsidered++
		mt.prepare(nextDoc)
		if canceled || res.Stats.MatchCount >= opts.ShardMaxMatchCount ||
			importantMatchCount >= opts.ShardMaxImportantMatch {
			res.Stats.FilesSkipped++
			continue
		}

		cp.setDocument(nextDoc)

		known := make(map[matchTree]bool)
		if v, ok := evalMatchTree(known, mt); ok && !v {
			continue nextFileMatch
		}

		// Files are cheap to match. Do them first.
		if len(fileAtoms) > 0 {
			for _, st := range fileAtoms {
				cp.evalContentMatches(st)
			}
			if v, ok := evalMatchTree(known, mt); ok && !v {
				continue nextFileMatch
			}
		}

		for st := range atoms {
			// TODO - this may evaluate too much.
			cp.evalContentMatches(st)
		}

		if len(regexpAtoms) > 0 {
			if v, ok := evalMatchTree(known, mt); ok && !v {
				continue nextFileMatch
			}

			for _, re := range regexpAtoms {
				cp.evalRegexpMatches(re)
			}
		}

		if v, ok := evalMatchTree(known, mt); !ok {
			log.Panicf("did not decide. Repo %s, doc %d, known %v",
				d.unaryData.RepoName, nextDoc, known)
		} else if !v {
			continue nextFileMatch
		}

		fileMatch := FileMatch{
			Repo: d.unaryData.RepoName,
			Name: string(d.fileName(nextDoc)),
			// Maintain ordering of input files. This
			// strictly dominates the in-file ordering of
			// the matches.
			Score: 10 * float64(nextDoc) / float64(len(d.boundaries)),
		}

		atomMatchCount := 0
		visitMatches(mt, known, func(mt matchTree) {
			atomMatchCount++
		})
		fileMatch.Score += float64(atomMatchCount) / float64(totalAtomCount) * scoreFactorAtomMatch
		finalCands := gatherMatches(mt, known)

		fileMatch.Matches = cp.fillMatches(finalCands)

		maxFileScore := 0.0
		for i := range fileMatch.Matches {
			if maxFileScore < fileMatch.Matches[i].Score {
				maxFileScore = fileMatch.Matches[i].Score

			}

			// Order by ordering in file.
			fileMatch.Matches[i].Score += 1.0 - (float64(i) / float64(len(fileMatch.Matches)))
		}
		fileMatch.Score += maxFileScore

		if fileMatch.Score > scoreImportantThreshold {
			importantMatchCount++
		}
		fileMatch.Branches = d.gatherBranches(nextDoc, mt, known)

		sortMatchesByScore(fileMatch.Matches)
		if opts.Whole {
			fileMatch.Content = cp.data(false)
		}

		res.Files = append(res.Files, fileMatch)
		res.Stats.MatchCount += len(fileMatch.Matches)
		res.Stats.FileCount++
	}
	sortFilesByScore(res.Files)
	res.RepoURLs = map[string]string{
		d.unaryData.RepoName: d.unaryData.RepoURL,
	}
	res.LineFragments = map[string]string{
		d.unaryData.RepoName: d.unaryData.RepoLineFragment,
	}
	return &res, nil
}

func extractSubstringQueries(q query.Q) []*query.Substring {
	var r []*query.Substring
	switch s := q.(type) {
	case *query.And:
		for _, ch := range s.Children {
			r = append(r, extractSubstringQueries(ch)...)
		}
	case *query.Or:
		for _, ch := range s.Children {
			r = append(r, extractSubstringQueries(ch)...)
		}
	case *query.Not:
		r = append(r, extractSubstringQueries(s.Child)...)
	case *query.Substring:
		r = append(r, s)
	}
	return r
}

type sortByOffsetSlice []*candidateMatch

func (m sortByOffsetSlice) Len() int      { return len(m) }
func (m sortByOffsetSlice) Swap(i, j int) { m[i], m[j] = m[j], m[i] }
func (m sortByOffsetSlice) Less(i, j int) bool {
	return m[i].offset < m[j].offset
}

// Gather matches from this document. This never returns a mixture of
// filename/content matches: if there are content matches, all
// filename matches are trimmed from the result. The matches are
// returned in document order and are non-overlapping.
func gatherMatches(mt matchTree, known map[matchTree]bool) []*candidateMatch {
	var cands []*candidateMatch
	visitMatches(mt, known, func(mt matchTree) {
		if smt, ok := mt.(*substrMatchTree); ok {
			cands = append(cands, smt.current...)
		}
		if rmt, ok := mt.(*regexpMatchTree); ok {
			cands = append(cands, rmt.found...)
		}
	})

	foundContentMatch := false
	for _, c := range cands {
		if !c.fileName {
			foundContentMatch = true
			break
		}
	}

	res := cands[:0]
	for _, c := range cands {
		if !foundContentMatch || !c.fileName {
			res = append(res, c)
		}
	}
	cands = res

	// Merge adjacent candidates. This guarantees that the matches
	// are non-overlapping.
	sort.Sort((sortByOffsetSlice)(cands))
	res = cands[:0]
	for i, c := range cands {
		if i == 0 {
			res = append(res, c)
			continue
		}
		last := res[len(res)-1]
		lastEnd := last.offset + last.matchSz
		end := c.offset + c.matchSz
		if lastEnd >= c.offset {
			if end > lastEnd {
				last.matchSz = end - last.offset
			}
			continue
		}

		res = append(res, c)
	}

	return res
}

func (d *indexData) gatherBranches(docID uint32, mt matchTree, known map[matchTree]bool) []string {
	foundBranchQuery := false
	var branches []string
	visitMatches(mt, known, func(mt matchTree) {
		bq, ok := mt.(*branchQueryMatchTree)
		if ok {
			foundBranchQuery = true
			branches = append(branches,
				d.branchNames[uint(bq.mask)])
		}
	})

	if !foundBranchQuery {
		mask := d.fileBranchMasks[docID]
		id := uint32(1)
		for mask != 0 {
			if mask&0x1 != 0 {
				branches = append(branches, d.branchNames[uint(id)])
			}
			id <<= 1
			mask >>= 1
		}
	}
	return branches
}

func (d *indexData) List(ctx context.Context, q query.Q) (*RepoList, error) {
	q = d.simplify(q)
	c, ok := q.(*query.Const)

	if !ok {
		return nil, fmt.Errorf("List should receive Repo-only query.")
	}

	l := &RepoList{}
	if c.Value {
		l.Repos = append(l.Repos, d.unaryData.RepoName)
	}
	return l, nil
}
