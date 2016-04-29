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
	"bytes"
	"fmt"
	"log"
	"regexp/syntax"
)

var _ = log.Printf

const ngramSize = 3

type SuggestQueryError struct {
	Message    string
	Suggestion string
}

func (e *SuggestQueryError) Error() string {
	return fmt.Sprintf("%s. Suggestion: %s", e.Message, e.Suggestion)
}

// parseStringLiteral parses a string literal, consumes the starting
// quote too.
func parseStringLiteral(in []byte) (lit []byte, n int, err error) {
	left := in[1:]
	found := false
	for len(left) > 0 {
		c := left[0]
		left = left[1:]
		switch c {
		case '"':
			found = true
			break
		case '\\':
			// TODO - other escape sequences.
			if len(left) == 0 {
				return nil, 0, fmt.Errorf("missing char after \\")
			}
			c = left[0]
			left = left[1:]

			lit = append(lit, c)
		default:
			lit = append(lit, c)
		}
	}
	if !found {
		return nil, 0, fmt.Errorf("unterminated quoted string")
	}
	return lit, len(in) - len(left), nil
}

var casePrefix = []byte("case:")
var filePrefix = []byte("file:")
var branchPrefix = []byte("branch:")
var regexPrefix = []byte("regex:")

type setCase string

func isSpace(c byte) bool {
	return c == ' ' || c == '\t'
}

// Consumes KEYWORD:<arg>, where arg may be quoted.
func consumeKeyword(in []byte, kw []byte) ([]byte, int, bool, error) {
	if !bytes.HasPrefix(in, kw) {
		return nil, 0, false, nil
	}

	var arg []byte
	var err error
	left := in
	left = left[len(kw):]
done:
	for len(left) > 0 {
		c := left[0]
		switch {
		case c == '"':
			var n int
			arg, n, err = parseStringLiteral(left)
			if err != nil {
				return nil, 0, true, err
			}

			left = left[n:]
			break
		case isSpace(c):
			break done
		default:
			arg = append(arg, c)
			left = left[1:]
		}
	}

	return arg, len(in) - len(left), true, nil
}

func tryConsumeCase(in []byte) (string, int, bool, error) {
	arg, n, ok, err := consumeKeyword(in, casePrefix)
	if err != nil || !ok {
		return "", 0, ok, err
	}

	switch string(arg) {
	case "yes":
	case "no":
	case "auto":
	default:
		return "", 0, false, fmt.Errorf("unknown case argument %q, want {yes,no,auto}", arg)
	}

	return string(arg), n, true, nil
}

func tryConsumeFile(in []byte) (string, int, bool, error) {
	arg, n, ok, err := consumeKeyword(in, filePrefix)
	return string(arg), n, ok, err
}

func tryConsumeBranch(in []byte) (string, int, bool, error) {
	arg, n, ok, err := consumeKeyword(in, branchPrefix)
	return string(arg), n, ok, err
}

func tryConsumeRegexp(in []byte) (string, int, bool, error) {
	arg, n, ok, err := consumeKeyword(in, regexPrefix)
	return string(arg), n, ok, err
}

func tryConsumeRepo(in []byte) (string, int, bool, error) {
	arg, n, ok, err := consumeKeyword(in, []byte("repo:"))
	return string(arg), n, ok, err
}

func Parse(qStr string) (Q, error) {
	b := []byte(qStr)

	qs, _, err := parseExprList(b)
	if err != nil {
		return nil, err
	}

	return Simplify(&And{qs}), nil
}

func parseExpr(in []byte) (Q, int, error) {
	b := in[:]
	var expr Q
	for len(b) > 0 && isSpace(b[0]) {
		b = b[1:]
	}

	tok, err := nextToken(b)
	if err != nil {
		return nil, 0, err
	}
	if tok == nil {
		return nil, 0, nil
	}
	b = b[len(tok.Input):]

	text := string(tok.Text)
	switch tok.Type {
	case tokCase:
		switch text {
		case "yes":
		case "no":
		case "auto":
		default:
			return nil, 0, fmt.Errorf("unknown case argument %q, want {yes,no,auto}", text)
		}
		expr = &Case{text}
	case tokRepo:
		expr = &Repo{Name: text}
	case tokBranch:
		expr = &Branch{Name: text}
	case tokText, tokRegex:
		q, err := regexpQuery(text, false)
		if err != nil {
			return nil, 0, err
		}
		expr = q
	case tokFile:
		q, err := regexpQuery(text, true)
		if err != nil {
			return nil, 0, err
		}
		expr = q

	case tokParenClose:
		// Caller must consume paren.
		expr = nil

	case tokParenOpen:
		qs, n, err := parseExprList(b)
		b = b[n:]
		pTok, err := nextToken(b)
		if err != nil {
			return nil, 0, err
		}
		if pTok == nil || pTok.Type != tokParenClose {
			return nil, 0, fmt.Errorf("missing close paren, token %v", pTok)
		}

		b = b[len(pTok.Input):]
		expr = &And{qs}
	case tokNegate:
		subQ, n, err := parseExpr(b)
		if err != nil {
			return nil, 0, err
		}
		b = b[n:]
		expr = &Not{subQ}
	}

	return expr, len(in) - len(b), nil
}

func regexpQuery(text string, file bool) (Q, error) {
	var expr Q

	r, err := syntax.Parse(text, 0)
	if err != nil {
		return nil, err
	}

	if r.Op == syntax.OpLiteral {
		expr = &Substring{
			Pattern:  string(r.Rune),
			FileName: file,
		}
	} else {
		expr = &Regexp{
			Regexp:   r,
			FileName: file,
		}
	}

	return expr, nil
}

func parseExprList(in []byte) ([]Q, int, error) {
	b := in[:]
	var qs []Q
	for len(b) > 0 {
		for len(b) > 0 && isSpace(b[0]) {
			b = b[1:]
		}

		if tok, _ := nextToken(b); tok != nil && tok.Type == tokParenClose {
			break
		}

		q, n, err := parseExpr(b)
		if err != nil {
			return nil, 0, err
		}
		if q == nil {
			// eof or a ')'
			break
		}
		qs = append(qs, q)
		b = b[n:]
	}

	setCase := ""
	newQS := qs[:0]
	for _, q := range qs {
		if sc, ok := q.(*Case); ok {
			setCase = sc.Flavor
		} else {
			newQS = append(newQS, q)
		}
	}
	qs = newQS
	for _, q := range qs {
		if sq, ok := q.(*Substring); ok && setCase != "" {
			if len(sq.Pattern) < 3 {
				return nil, 0, &SuggestQueryError{
					fmt.Sprintf("pattern %q too short", sq.Pattern),
					fmt.Sprintf("%q", in),
				}
			}
			switch setCase {
			case "yes":
				sq.CaseSensitive = true
			case "no":
				sq.CaseSensitive = false
			case "auto":
				sq.CaseSensitive = (sq.Pattern != string(toLower([]byte(sq.Pattern))))
			}
		}
	}

	return qs, len(in) - len(b), nil
}

type token struct {
	Type  int
	Text  []byte
	Input []byte
}

func (t *token) String() string {
	return fmt.Sprintf("%s:%q", tokNames[t.Type], t.Text)
}

const (
	tokText       = 0
	tokFile       = 1
	tokRepo       = 2
	tokCase       = 3
	tokBranch     = 4
	tokParenOpen  = 5
	tokParenClose = 6
	tokError      = 7
	tokNegate     = 8
	tokRegex      = 9
)

var tokNames = map[int]string{
	tokText:       "Text",
	tokFile:       "File",
	tokRepo:       "Repo",
	tokCase:       "Case",
	tokBranch:     "Branch",
	tokParenOpen:  "ParenOpen",
	tokParenClose: "ParenClose",
	tokError:      "Error",
	tokNegate:     "Negate",
	tokRegex:      "Regex",
}

var prefixes = map[string]int{
	"file:":   tokFile,
	"f:":      tokFile,
	"repo:":   tokRepo,
	"case:":   tokCase,
	"branch:": tokBranch,
	"regex:":  tokRegex,
}

func (t *token) setType() {
	if len(t.Text) == 1 && t.Text[0] == '(' {
		t.Type = tokParenOpen
	}
	if len(t.Text) == 1 && t.Text[0] == ')' {
		t.Type = tokParenClose
	}

	for pref, typ := range prefixes {
		if !bytes.HasPrefix(t.Input, []byte(pref)) {
			continue
		}

		t.Text = t.Text[len(pref):]
		t.Type = typ
		break
	}
}

func nextToken(in []byte) (*token, error) {
	left := in[:]
	parenCount := 0
	var cur token
	if len(left) == 0 {
		return nil, nil
	}

	if left[0] == '-' {
		return &token{
			Type:  tokNegate,
			Text:  []byte{'-'},
			Input: in[:1],
		}, nil
	}

	foundSpace := false
	foundParenOpen := false

loop:
	for len(left) > 0 {
		c := left[0]
		switch c {
		case '(':
			foundParenOpen = true
			parenCount++
			cur.Text = append(cur.Text, c)
			left = left[1:]
		case ')':
			if foundParenOpen {
				cur.Text = append(cur.Text, c)
				left = left[1:]
				parenCount--
			} else if len(cur.Text) == 0 {
				cur.Text = []byte{')'}
				left = left[1:]
			} else {
				break loop
			}

		case '"':
			t, n, err := parseStringLiteral(left)
			if err != nil {
				return nil, err
			}
			cur.Text = append(cur.Text, t...)
			left = left[n:]
		case '\\':
			left = left[1:]
			if len(left) == 0 {
				return nil, fmt.Errorf("lone \\ at end")
			}
			c := left[0]
			cur.Text = append(cur.Text, '\\', c)
			left = left[1:]

		case ' ', '\n', '\t':
			if parenCount > 0 {
				foundSpace = true
			}
			break loop
		default:
			cur.Text = append(cur.Text, c)
			left = left[1:]
		}
	}

	if len(cur.Text) == 0 {
		return nil, nil
	}

	if foundSpace && cur.Text[0] == '(' {
		cur.Text = cur.Text[:1]
		cur.Input = in[:1]
	} else {
		cur.Input = in[:len(in)-len(left)]
	}
	cur.setType()
	return &cur, nil
}
