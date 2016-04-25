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
	"regexp/syntax"
	"strings"
)

var _ = log.Println

// Query is a representation for a possibly hierarchical search query.
type Query interface {
	String() string
}

// RegexpQuery is a query looking for regular expressions matches.
type RegexpQuery struct {
	Regexp *syntax.Regexp
}

func (q *RegexpQuery) String() string {
	return fmt.Sprintf("regex:%q", q.Regexp.String())
}

// SubstringQuery is the most basic query: a query for a substring.
type SubstringQuery struct {
	Pattern       string
	CaseSensitive bool
	FileName      bool
}

func (q *SubstringQuery) String() string {
	s := ""

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

// BranchQuery limits search to a specific branch.
type BranchQuery struct {
	Name string
}

func (q *BranchQuery) String() string {
	return fmt.Sprintf("branch:%q", q.Name)
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

func mapQuery(qs []Query, f func(Query) Query) []Query {
	var neg []Query
	for _, sub := range qs {
		neg = append(neg, f(sub))
	}
	return neg
}

func simplify(q Query) Query {
	for {
		var changed bool
		q, changed = flatten(q)
		if !changed {
			break
		}
	}

	return q
}
