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
	"reflect"
	"testing"
)

func TestDeltas(t *testing.T) {
	in := []uint32{1, 72, 0xfff}
	out := toSizedDeltas(in)
	round := fromSizedDeltas(out, nil)
	if !reflect.DeepEqual(in, round) {
		t.Errorf("got %v, want %v", round, in)
	}
}
