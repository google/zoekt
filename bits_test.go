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
	roundtrip := unmarshalDocSections(serialized)
	if !reflect.DeepEqual(in, roundtrip) {
		t.Errorf("got %v, want %v", roundtrip, in)
	}
}

func TestGenerateCaseNgrams(t *testing.T) {
	ng := stringToNGram("aB1")
	gotNG := generateCaseNgrams(ng)

	var got []string
	for _, n := range gotNG {
		got = append(got, string(ngramToBytes(n)))
	}

	want := []string{
		"aB1",
		"AB1",
		"ab1",
		"Ab1",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeUInt32(t *testing.T) {
	in := [][]uint32{
		{1, 7, 9},
		{5, 7},
	}
	got := mergeUint32(in)
	want := []uint32{1, 5, 7, 7, 9}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeepEqual: got %v want %v", got, want)
	}
}
