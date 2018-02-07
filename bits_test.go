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

func TestNgram(t *testing.T) {
	in := "abc"
	n := stringToNGram(in)
	if n.String() != "abc" {
		t.Errorf("got %q, want %q", n, "abc")
	}
}

func TestDocSection(t *testing.T) {
	in := []DocumentSection{{1, 2}, {3, 4}}
	serialized := marshalDocSections(in)
	roundtrip := unmarshalDocSections(serialized, nil)
	if !reflect.DeepEqual(in, roundtrip) {
		t.Errorf("got %v, want %v", roundtrip, in)
	}
}

func TestGenerateCaseNgrams(t *testing.T) {
	ng := stringToNGram("aB1")
	gotNG := generateCaseNgrams(ng)

	got := map[string]bool{}
	for _, n := range gotNG {
		got[string(ngramToBytes(n))] = true
	}

	want := map[string]bool{
		"aB1": true,
		"AB1": true,
		"ab1": true,
		"Ab1": true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextFileIndex(t *testing.T) {
	for _, tc := range []struct {
		off, curFile uint32
		ends         []uint32
		want         uint32
	}{
		{maxUInt32, 0, []uint32{34}, 1},
		{9, 0, []uint32{34}, 0},
		{450, 0, []uint32{100, 200, 300, 400, 500, 600}, 4},
	} {
		got := nextFileIndex(tc.off, tc.curFile, tc.ends)
		if got != tc.want {
			t.Errorf("%v: got %d, want %d", tc, got, tc.want)
		}
	}
}
