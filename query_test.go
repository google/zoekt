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
	"log"
	"reflect"
	"testing"
)

var _ = log.Println

func TestQueryString(t *testing.T) {
	q := &OrQuery{[]Query{
		&AndQuery{[]Query{
			&SubstringQuery{Pattern: "hoi"},
			&NotQuery{&SubstringQuery{Pattern: "hai"}},
		}}}}
	got := q.String()
	want := `(or (and substr:"hoi" (not substr:"hai")))`

	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestSimplify(t *testing.T) {
	type testcase struct {
		in   Query
		want Query
	}

	cases := []testcase{
		{
			in: &OrQuery{[]Query{
				&OrQuery{[]Query{
					&AndQuery{[]Query{
						&SubstringQuery{Pattern: "hoi"},
						&NotQuery{&SubstringQuery{Pattern: "hai"}},
					}},
					&OrQuery{[]Query{
						&SubstringQuery{Pattern: "zip"},
						&SubstringQuery{Pattern: "zap"},
					}},
				}}}},
			want: &OrQuery{[]Query{
				&AndQuery{[]Query{
					&SubstringQuery{Pattern: "hoi"},
					&NotQuery{&SubstringQuery{Pattern: "hai"}},
				}},
				&SubstringQuery{Pattern: "zip"},
				&SubstringQuery{Pattern: "zap"}},
			}},
		{in: &AndQuery{}, want: &TrueQuery{}},
		{in: &OrQuery{}, want: &FalseQuery{}},
		{in: &AndQuery{[]Query{&TrueQuery{}, &FalseQuery{}}}, want: &FalseQuery{}},
		{in: &OrQuery{[]Query{&FalseQuery{}, &TrueQuery{}}}, want: &TrueQuery{}},
		{in: &NotQuery{&TrueQuery{}}, want: &FalseQuery{}},
	}

	for _, c := range cases {
		got := simplify(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
}
