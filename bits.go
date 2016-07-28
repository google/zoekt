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

func toLower(in []byte) []byte {
	out := make([]byte, len(in))
	for i, c := range in {
		if c >= 'A' && c <= 'Z' {
			c = c - 'A' + 'a'
		}
		out[i] = c
	}
	return out
}

type ngram uint32

func bytesToNGram(b []byte) ngram {
	return ngram(uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]))
}

func stringToNGram(s string) ngram {
	return bytesToNGram([]byte(s))
}

func ngramToBytes(n ngram) []byte {
	return []byte{byte(n >> 16), byte(n >> 8), byte(n)}
}

func (n ngram) String() string {
	return string(ngramToBytes(n))
}

const (
	_classChar  = 0
	_classDigit = iota
	_classPunct = iota
	_classOther = iota
	_classSpace = iota
)

func byteClass(c byte) int {
	if (c >= 'a' && c <= 'z') || c >= 'A' && c <= 'Z' {
		return _classChar
	}
	if c >= '0' && c <= '9' {
		return _classDigit
	}

	switch c {
	case ' ', '\n':
		return _classSpace
	case '.', ',', ';', '"', '\'':
		return _classPunct
	default:
		return _classOther
	}
}

func marshalDocSections(secs []DocumentSection) []byte {
	ints := make([]uint32, 0, len(secs)*2)
	for _, s := range secs {
		ints = append(ints, uint32(s.Start), uint32(s.End))
	}

	return toDeltas(ints)
}

func unmarshalDocSections(in []byte) (secs []DocumentSection) {
	ints := fromDeltas(in, nil)
	res := make([]DocumentSection, 0, len(ints)/2)
	for len(ints) > 0 {
		res = append(res, DocumentSection{ints[0], ints[1]})
		ints = ints[2:]
	}
	return res
}
