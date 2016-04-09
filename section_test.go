package codesearch

import (
	"reflect"
	"testing"
)

func TestDeltas(t *testing.T) {
	in := []uint32{1, 72, 0xfff}
	out := toDeltas(in)
	round := fromDeltas(out)
	if !reflect.DeepEqual(in, round) {
		t.Errorf("got %v, want %v", round, in)
	}
}
