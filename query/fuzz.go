// Copyright 2020 Google Inc. All rights reserved.
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

package query

// This file implements a fuzzer using go-fuzz.
// More information about go-fuzz can be found here:
// https://github.com/dvyukov/go-fuzz

// To run the fuzzer locally, follow these steps:
// 1) go get github.com/google/zoekt
// 2) go get -u github.com/dvyukov/go-fuzz/go-fuzz
// 3) go get -u github.com/dvyukov/go-fuzz/go-fuzz-build
// 4) cd into dir of fuzz.go
// 5) $GOPATH/bin/go-fuzz-build
// 6) $GOPATH/bin/go-fuzz

func Fuzz(data []byte) int {
	_, err := Parse(string(data))
	if err != nil {
		return 0
	}
	return 1
}
