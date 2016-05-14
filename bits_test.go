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
	"strings"
	"testing"
)

var _ = log.Println

func TestToOriginal(t *testing.T) {
	in := "AbBcDeFggHiJkLLmNoPQ"

	out := make([]byte, len(in)+8)
	for i := 0; i < len(in); i++ {
		for j := i; j < len(in); j++ {
			orig := []byte(in)

			lowered, bits := splitCase(orig)
			roundtrip := toOriginal(out, lowered, bits, i, j)
			if want := orig[i:j]; !reflect.DeepEqual(roundtrip, want) {
				t.Fatalf("%d,%d: got roundtrip %q (%d), want %q (%d)", i, j, roundtrip, len(roundtrip), want, len(want))
			}
		}
	}
}

func TestBitFunctions(t *testing.T) {
	orig := []byte("abCDef")
	lowered, bits := splitCase(orig)
	if want := []byte{1<<2 | 1<<3}; !reflect.DeepEqual(bits, want) {
		t.Errorf("got bits %v, want %v", bits, want)
	}

	if want := strings.ToLower("abcdef"); want != string(lowered) {
		t.Errorf("got lowercase %q, want %q", lowered, want)
	}
}

func TestCaseMasks(t *testing.T) {
	m, b := findCaseMasks([]byte("aB"))

	if m[0][0] != (1 | 2) {
		t.Errorf("%v", m[0][0])
	}
	if b[0][0] != (0 | 2) {
		t.Errorf("b[0] %v", m[0][0])
	}

	if m[1][0] != (2 | 4) {
		t.Errorf("m[0]")
	}
	if b[1][0] != (0 | 4) {
		t.Errorf("b[1]")
	}
}

func TestNgram(t *testing.T) {
	in := "abc"
	n := stringToNGram(in)
	if n.String() != "abc" {
		t.Errorf("got %q, want %q", n, "abc")
	}
}

func benchmarkToOriginal(b *testing.B, sz int, portable bool) {
	b.StopTimer()
	line := `  if ((size == kSignedByte || size == kUnsignedByte) && !IsByteRegister(rl_src.reg)) {`
	pre := "xX\n"
	post := "\nbla"

	for i := 0; i < sz; i++ {
		line = line + line
	}
	content := []byte(pre + line + post)
	lwr, cb := splitCase(content)

	result := make([][]byte, 0, b.N)

	b.SetBytes(int64(len(line)))

	out := make([]byte, len(line)+20)
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		if portable {
			result = append(result, toOriginalPortable(out, lwr, cb, len(pre), len(line)+len(pre)))
		} else {
			result = append(result, toOriginal(out, lwr, cb, len(pre), len(line)+len(pre)))
		}
	}
}

func BenchmarkToOriginalPortableSmall(b *testing.B) {
	benchmarkToOriginal(b, 0, true)
}

func BenchmarkToOriginalSmall(b *testing.B) {
	benchmarkToOriginal(b, 0, false)
}

func BenchmarkToOriginalPortableBig(b *testing.B) {
	benchmarkToOriginal(b, 10, true)
}

func BenchmarkToOriginalBig(b *testing.B) {
	benchmarkToOriginal(b, 10, false)
}

func TestDocSection(t *testing.T) {
	in := []DocumentSection{{1, 2}, {3, 4}}
	serialized := marshalDocSections(in)
	roundtrip := unmarshalDocSections(serialized)
	if !reflect.DeepEqual(in, roundtrip) {
		t.Errorf("got %v, want %v", roundtrip, in)
	}
}
