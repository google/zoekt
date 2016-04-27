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
	"log"
	"reflect"
	"testing"
)

var _ = log.Println

func TestQueryString(t *testing.T) {
	q := &Or{[]Query{
		&And{[]Query{
			&Substring{Pattern: "hoi"},
			&Not{&Substring{Pattern: "hai"}},
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
			in: &Or{[]Query{
				&Or{[]Query{
					&And{[]Query{
						&Substring{Pattern: "hoi"},
						&Not{&Substring{Pattern: "hai"}},
					}},
					&Or{[]Query{
						&Substring{Pattern: "zip"},
						&Substring{Pattern: "zap"},
					}},
				}}}},
			want: &Or{[]Query{
				&And{[]Query{
					&Substring{Pattern: "hoi"},
					&Not{&Substring{Pattern: "hai"}},
				}},
				&Substring{Pattern: "zip"},
				&Substring{Pattern: "zap"}},
			}},
		{in: &And{}, want: &Const{true}},
		{in: &Or{}, want: &Const{false}},
		{in: &And{[]Query{&Const{true}, &Const{false}}}, want: &Const{false}},
		{in: &Or{[]Query{&Const{false}, &Const{true}}}, want: &Const{true}},
		{in: &Not{&Const{true}}, want: &Const{false}},
	}

	for _, c := range cases {
		got := Simplify(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
}
