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

package query

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
type Regexp struct {
	Regexp   *syntax.Regexp
	FileName bool
}

func (q *Regexp) String() string {
	pref := ""
	if q.FileName {
		pref = "file_"
	}
	return fmt.Sprintf("%sregex:%q", pref, q.Regexp.String())
}

type Case struct {
	Flavor string
}

func (c *Case) String() string {
	return "case:" + c.Flavor
}

type Const struct {
	Value bool
}

func (q *Const) String() string {
	if q.Value {
		return "TRUE"
	}
	return "FALSE"
}

type Repo struct {
	Name string
}

func (q *Repo) String() string {
	return fmt.Sprintf("repo:%s", q.Name)
}

// Substring is the most basic query: a query for a substring.
type Substring struct {
	Pattern       string
	CaseSensitive bool
	FileName      bool
}

func (q *Substring) String() string {
	s := ""

	t := ""
	if q.FileName {
		t = "file_"
	}

	s += fmt.Sprintf("%ssubstr:%q", t, q.Pattern)
	if q.CaseSensitive {
		s = "case_" + s
	}
	return s
}

// Or is matched when any of its children is matched.
type Or struct {
	Children []Query
}

func (q *Or) String() string {
	var sub []string
	for _, ch := range q.Children {
		sub = append(sub, ch.String())
	}
	return fmt.Sprintf("(or %s)", strings.Join(sub, " "))
}

// Not inverts the meaning of its child.
type Not struct {
	Child Query
}

func (q *Not) String() string {
	return fmt.Sprintf("(not %s)", q.Child)
}

// And is matched when all its children are.
type And struct {
	Children []Query
}

func (q *And) String() string {
	var sub []string
	for _, ch := range q.Children {
		sub = append(sub, ch.String())
	}
	return fmt.Sprintf("(and %s)", strings.Join(sub, " "))
}

// Branch limits search to a specific branch.
type Branch struct {
	Name string
}

func (q *Branch) String() string {
	return fmt.Sprintf("branch:%q", q.Name)
}

func queryChildren(q Query) []Query {
	switch s := q.(type) {
	case *And:
		return s.Children
	case *Or:
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
	case *And:
		if len(s.Children) == 1 {
			return s.Children[0], true
		}
		flatChildren, changed := flattenAndOr(s.Children, s)
		return &And{flatChildren}, changed
	case *Or:
		if len(s.Children) == 1 {
			return s.Children[0], true
		}
		flatChildren, changed := flattenAndOr(s.Children, s)
		return &Or{flatChildren}, changed
	default:
		return q, false
	}
}

func mapQueryList(qs []Query, f func(Query) Query) []Query {
	var neg []Query
	for _, sub := range qs {
		neg = append(neg, f(sub))
	}
	return neg
}

func invertConst(q Query) Query {
	c, ok := q.(*Const)
	if ok {
		return &Const{!c.Value}
	}
	return q
}

func evalAndOrConstants(q Query, children []Query) Query {
	_, isAnd := q.(*And)

	children = mapQueryList(children, evalConstants)

	newCH := children[:0]
	for _, ch := range children {
		c, ok := ch.(*Const)
		if ok {
			if c.Value == isAnd {
				continue
			} else {
				return ch
			}
		}
		newCH = append(newCH, ch)
	}
	if len(newCH) == 0 {
		return &Const{isAnd}
	}
	if isAnd {
		return &And{newCH}
	}
	return &Or{newCH}
}

func evalConstants(q Query) Query {
	switch s := q.(type) {
	case *And:
		return evalAndOrConstants(q, s.Children)
	case *Or:
		return evalAndOrConstants(q, s.Children)
	case *Not:
		ch := evalConstants(s.Child)
		if _, ok := ch.(*Const); ok {
			return invertConst(ch)
		}
		return &Not{ch}
	}
	return q
}

func Simplify(q Query) Query {
	q = evalConstants(q)
	for {
		var changed bool
		q, changed = flatten(q)
		if !changed {
			break
		}
	}

	return q
}

// Map runs f over the q.
func Map(q Query, f func(q Query) Query) Query {
	switch s := q.(type) {
	case *And:
		return &And{Children: mapQueryList(s.Children, f)}
	case *Or:
		return &Or{Children: mapQueryList(s.Children, f)}
	case *Not:
		return &Not{Child: f(s.Child)}
	}
	return f(q)
}
