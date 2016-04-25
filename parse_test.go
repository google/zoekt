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
	"reflect"
	"regexp/syntax"
	"testing"
)

func mustParseRE(s string) *syntax.Regexp {
	r, err := syntax.Parse(s, 0)
	if err != nil {
		panic(err)
	}

	return r
}

func TestParseQuery(t *testing.T) {
	type testcase struct {
		in     string
		out    Query
		hasErr bool
	}

	for _, c := range []testcase{
		{"sub-pixel", &SubstringQuery{Pattern: "sub-pixel"}, false},
		{"abc", &SubstringQuery{Pattern: "abc"}, false},
		{"\"abc bcd\"", &SubstringQuery{Pattern: "abc bcd"}, false},
		{"abc bcd", &AndQuery{[]Query{
			&SubstringQuery{Pattern: "abc"},
			&SubstringQuery{Pattern: "bcd"},
		}}, false},
		{"-abc", &NotQuery{&SubstringQuery{Pattern: "abc"}}, false},

		{"abccase:yes", &SubstringQuery{Pattern: "abccase:yes"}, false},
		{"file:abc", &SubstringQuery{Pattern: "abc", FileName: true}, false},
		{"branch:pqr", &BranchQuery{Name: "pqr"}, false},

		{"file:helpers.go byte", &AndQuery{[]Query{
			&SubstringQuery{Pattern: "helpers.go", FileName: true},
			&SubstringQuery{Pattern: "byte"},
		}}, false},

		{"regex:abc[p-q]", &RegexpQuery{mustParseRE("abc[p-q]")}, false},

		// case
		{"abc case:yes", &SubstringQuery{Pattern: "abc", CaseSensitive: true}, false},
		{"abc case:auto", &SubstringQuery{Pattern: "abc", CaseSensitive: false}, false},
		{"ABC case:auto", &SubstringQuery{Pattern: "ABC", CaseSensitive: true}, false},
		{"ABC case:\"auto\"", &SubstringQuery{Pattern: "ABC", CaseSensitive: true}, false},
		// errors.
		{"\"abc", nil, true},
		{"\"a\\", nil, true},
		{"case:foo", nil, true},
		{"", nil, true},
	} {
		q, err := Parse(c.in)
		if c.hasErr != (err != nil) {
			t.Errorf("Parse(%s): error %v", c.in, err)
		} else if q != nil {
			if !reflect.DeepEqual(q, c.out) {
				t.Errorf("Parse(%s): got %v want %v", c.in, q, c.out)
			}
		}
	}
}
