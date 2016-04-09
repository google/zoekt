package codesearch

import (
	"log"
	"reflect"
	"testing"
)

var _ = log.Println

func TestBitFunctions(t *testing.T) {
	orig := []byte("abCDef")

	lowered, bits := splitCase(orig)
	if want := []byte{1<<2 | 1<<3}; !reflect.DeepEqual(bits, want) {
		t.Errorf("got bits %v, want %v", bits, want)
	}

	if want := "abcdef"; want != string(lowered) {
		t.Errorf("got lowercase %q, want %q", lowered, want)
	}

	roundtrip := toOriginal(lowered, bits, 1, 4)
	if want := orig[1:4]; !reflect.DeepEqual(roundtrip, want) {
		t.Errorf("got roundtrip %q, want %q", roundtrip, want)
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
	log.Println(ngramToBytes(0xf0e010))
	if n.String() != "abc" {
		t.Errorf("got %q, want %q", n, "abc")
	}
}
