// Copyright 2019 Google Inc. All rights reserved.
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
	"sort"
	"testing"
	"testing/quick"

	"github.com/google/go-cmp/cmp"
)

func TestCompressedPostingIterator_limit(t *testing.T) {
	f := func(nums, limits []uint32) bool {
		if len(nums) == 0 || len(limits) == 0 {
			return true
		}

		nums = sortedUnique(nums)
		sort.Slice(limits, func(i, j int) bool { return limits[i] < limits[j] })

		want := doHitIterator(&inMemoryIterator{postings: nums}, limits)

		it := newCompressedPostingIterator(toDeltas(nums), stringToNGram("abc"))
		got := doHitIterator(it, limits)
		if !reflect.DeepEqual(want, got) {
			t.Log(cmp.Diff(want, got))
			return false
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func doHitIterator(it hitIterator, limits []uint32) []uint32 {
	var nums []uint32
	for _, limit := range limits {
		it.next(limit)
		nums = append(nums, it.first())
	}
	return nums
}
