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

// +build !amd64

package zoekt

// toOriginal undoes case folding. The `out` argument should be at
// least 8 bytes extra space `end-start`.
func toOriginal(out []byte, in []byte, caseBits []byte, start, end int) []byte {
	return toOriginalPortable(out, in, caseBits, start, end)
}
