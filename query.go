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
	"reflect"
	"strings"
)

var _ = log.Println

// Query is a representation for a possibly hierarchical search query.
type Query interface {
	String() string
}

// SubstringQuery is the most basic query: a query for a substring.
type SubstringQuery struct {
	Pattern       string
	CaseSensitive bool
	Negate        bool
	FileName      bool
}

func (q *SubstringQuery) String() string {
	s := ""
	if q.Negate {
		s = "-"
	}

	t := "sub"
	if q.FileName {
		t = "file"
	}

	s += fmt.Sprintf("%sstr:%q", t, q.Pattern)
	if q.CaseSensitive {
		s = "case_" + s
	}
	return s
}

// OrQuery is matched when any of its children is matched.
type OrQuery struct {
	Children []Query
}

func (q *OrQuery) String() string {
	var sub []string
	for _, ch := range q.Children {
		sub = append(sub, ch.String())
	}
	return fmt.Sprintf("(or %s)", strings.Join(sub, " "))
}

// NotQuery inverts the meaning of its child.
type NotQuery struct {
	Child Query
}

func (q *NotQuery) String() string {
	return fmt.Sprintf("(not %s)", q.Child)
}

// AndQuery is matched when all its children are.
type AndQuery struct {
	Children []Query
}

func (q *AndQuery) String() string {
	var sub []string
	for _, ch := range q.Children {
		sub = append(sub, ch.String())
	}
	return fmt.Sprintf("(and %s)", strings.Join(sub, " "))
}

type andQuery struct {
	atoms []*SubstringQuery
}

func (q *andQuery) String() string {
	var qs []string
	for _, a := range q.atoms {
		qs = append(qs, a.String())
	}
	return fmt.Sprintf("(AND %s)", strings.Join(qs, " "))
}

type orQuery struct {
	ands []*andQuery
}

func (q *orQuery) String() string {
	var qs []string
	for _, a := range q.ands {
		qs = append(qs, a.String())
	}
	return fmt.Sprintf("(OR %s)", strings.Join(qs, " "))
}

func queryChildren(q Query) []Query {
	switch s := q.(type) {
	case *AndQuery:
		return s.Children
	case *OrQuery:
		return s.Children
	}
	return nil
}

func flattenAndOr(children []Query, typ Query) ([]Query, bool) {
	var flat []Query
	changed := false
	for _, ch := range children {
		ch, subChanged := flatten(ch)
		changed = changed || subChanged
		if reflect.TypeOf(ch) == reflect.TypeOf(typ) {
			changed = true
			subChildren := queryChildren(ch)
			if subChildren != nil {
				flat = append(flat, subChildren...)
			}
		} else {
			flat = append(flat, ch)
		}
	}

	return flat, changed
}

// (and (and x y) z) => (and x y z) , the same for "or"
func flatten(q Query) (Query, bool) {
	switch s := q.(type) {
	case *AndQuery:
		if len(s.Children) == 1 {
			return s.Children[0], true
		}
		flatChildren, changed := flattenAndOr(s.Children, s)
		return &AndQuery{flatChildren}, changed
	case *OrQuery:
		if len(s.Children) == 1 {
			return s.Children[0], true
		}
		flatChildren, changed := flattenAndOr(s.Children, s)
		return &OrQuery{flatChildren}, changed
	default:
		return q, false
	}
}

func negate(q Query) Query {
	switch s := q.(type) {
	case *NotQuery:
		return s.Child
	case *AndQuery:
		return &OrQuery{mapQuery(s.Children, negate)}
	case *OrQuery:
		return &AndQuery{mapQuery(s.Children, negate)}
	case *SubstringQuery:
		sub := *s
		sub.Negate = !sub.Negate
		return &sub
	default:
		panic("q")
	}
}

func mapQuery(qs []Query, f func(Query) Query) []Query {
	var neg []Query
	for _, sub := range qs {
		neg = append(neg, f(sub))
	}
	return neg
}

func pushDownNegations(q Query) Query {
	switch s := q.(type) {
	case *AndQuery:
		return &AndQuery{mapQuery(s.Children, pushDownNegations)}
	case *OrQuery:
		return &OrQuery{mapQuery(s.Children, pushDownNegations)}
	case *NotQuery:
		return negate(s.Child)
	default:
		return q
	}
}

func standardizeAnd(q Query) (*andQuery, bool) {
	switch s := q.(type) {
	case *AndQuery:
		var r andQuery
		for _, ch := range s.Children {
			atom, ok := ch.(*SubstringQuery)
			if !ok {
				return nil, false
			}
			r.atoms = append(r.atoms, atom)
		}
		return &r, true
	case *SubstringQuery:
		return &andQuery{atoms: []*SubstringQuery{s}}, true
	}
	return nil, false
}

func standardizeOr(q Query) (*orQuery, bool) {
	switch s := q.(type) {
	case *AndQuery:
		andQ, ok := standardizeAnd(s)
		if ok {
			return &orQuery{
				ands: []*andQuery{andQ},
			}, true
		}
		return nil, false
	case *OrQuery:
		var r orQuery
		for _, ch := range s.Children {
			and, ok := standardizeAnd(ch)
			if !ok {
				return nil, false
			}
			r.ands = append(r.ands, and)
		}

		return &r, true
	case *SubstringQuery:
		and, _ := standardizeAnd(s)
		return &orQuery{
			ands: []*andQuery{and},
		}, true
	}

	return nil, false
}

func simplify(q Query) Query {
	q = pushDownNegations(q)
	for {
		var changed bool
		q, changed = flatten(q)
		if !changed {
			break
		}
	}

	return q
}

func standardize(q Query) (*orQuery, error) {
	q = simplify(q)
	orQ, ok := standardizeOr(q)
	if !ok {
		return nil, fmt.Errorf("cannot standardize %s", q)
	}
	return orQ, nil
}
