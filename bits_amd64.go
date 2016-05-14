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

import "unsafe"

const diff = 'a' - 'A'

var diffLookupTable = [16][4]byte{}

func init() {
	for i := 0; i < 16; i++ {
		b := i
		var d [4]byte
		for j := uint(0); j < 4; j++ {
			if (b>>j)&0x1 != 0 {
				d[j] = diff
			}
		}
		diffLookupTable[byte(i)] = d
	}
}

// This probably also works on more nitpicky architectures (ARM) if we
// could be sure they allocate larger byte slices at 8-byte aligned addresses.
// This is about 1.6x faster than the portable version.

// The `out` argument should be at least 8 bytes extra space
// `end-start`.
func toOriginal(out []byte, in []byte, caseBits []byte, start, end int) []byte {
	rounded := start - (start % 8)

	i := rounded

	var diff [8]byte
	for ; i+8 <= end; i += 8 {
		b := caseBits[i/8]

		lwr := diffLookupTable[b&0xf]
		upr := diffLookupTable[(b>>4)&0xf]

		diff = [8]byte{lwr[0], lwr[1], lwr[2], lwr[3],
			upr[0], upr[1], upr[2], upr[3]}

		dstP := (*uint64)(unsafe.Pointer(&out[i-rounded]))
		inP := (*uint64)(unsafe.Pointer(&in[i]))
		difP := (*uint64)(unsafe.Pointer(&diff[0]))
		*dstP = *inP - *difP
	}

	var b byte
	for ; i < end; i++ {
		if i%8 == 0 {
			b = caseBits[i/8]
		}
		c := in[i]
		if b&0x1 != 0 {
			c = c - 'a' + 'A'
		}
		out[i-rounded] = c
		b >>= 1
	}
	return out[start-rounded : end-rounded]
}
