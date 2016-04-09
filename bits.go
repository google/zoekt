package codesearch

import (
	"bytes"
	"log"
)

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

func toOriginal(in []byte, caseBits []byte, start, end int) []byte {
	out := make([]byte, end-start)

	rounded := start - (start % 8)
	var b byte
	for i := rounded; i < end; i++ {
		if i%8 == 0 {
			b = caseBits[i/8]
		}

		if i >= start {
			c := in[i]
			if b&0x1 != 0 {
				c = c - 'a' + 'A'
			}
			out[i-start] = c
		}
		b >>= 1
	}
	return out
}

func diffBits(a, b []byte) []byte {
	if len(a) != len(b) {
		log.Panic("lengths", len(a), len(b))
	}
	if len(a)%8 != 0 {
		panic("mod")
	}
	bits := make([]byte, len(a)/8)
	for i := 0; i < len(a); i += 8 {
		var limb byte
		for j := 0; j < 8; j++ {
			var diff byte
			if a[i+j] != b[i+j] {
				diff = 0x1
			}
			limb |= (diff << uint(j))
		}
		bits[i/8] = limb
	}
	return bits
}

func splitCase(content []byte) (lower []byte, caseBits []byte) {
	origLen := len(content)
	up := len(content) + (8-(len(content)%8))%8
	for len(content) < up {
		content = append(content, 0)
	}
	lowered := toLower(content)
	diff := diffBits(content, lowered)
	lowered = lowered[:origLen]
	return lowered, diff
}

// Generates bitvectors for case folding and accompanying bitmasks for
// all 8 different shifts of the pattern.
func findCaseMasks(pattern []byte) (mask [][]byte, bits [][]byte) {
	patLen := len(pattern)
	for i := 0; i < 8; i++ {
		orig := bytes.Repeat([]byte{0}, i)
		orig = append(orig, pattern...)

		lower := bytes.Repeat([]byte{0}, i)
		lower = append(lower, toLower(pattern)...)

		m1 := bytes.Repeat([]byte{0}, i)
		m2 := bytes.Repeat([]byte{0}, i)

		m1 = append(m1, bytes.Repeat([]byte{1}, patLen)...)
		m2 = append(m2, bytes.Repeat([]byte{2}, patLen)...)

		for _, s := range []*[]byte{&m1, &m2, &lower, &orig} {
			for len(*s)%8 != 0 {
				*s = append(*s, 0)
			}
		}

		mask = append(mask, diffBits(m1, m2))
		bits = append(bits, diffBits(orig, lower))
	}

	return mask, bits
}

type ngram uint32

func bytesToNGram(b []byte) ngram {
	return ngram(uint32(b[0]) << 16 | uint32(b[1]) << 8 | uint32(b[2]))
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
