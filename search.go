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

package codesearch

import (
	"log"
)

var _ = log.Println

type searchInput struct {
	pat string

	first []uint32
	last  []uint32
	ends  []uint32
}

type candidateMatch struct {
	file   uint32
	offset uint32
}

func (s *searchInput) search() []candidateMatch {
	fileIdx := 0
	diff := uint32(len(s.pat) - NGRAM)

	var candidates []candidateMatch
	for {
		if len(s.first) == 0 || len(s.last) == 0 {
			break
		}
		p1 := s.first[0]
		p2 := s.last[0]

		for fileIdx < len(s.ends) && s.ends[fileIdx] <= p1 {
			fileIdx++
		}

		if p1+diff < p2 {
			s.first = s.first[1:]
		} else if p1+diff > p2 {
			s.last = s.last[1:]
		} else {
			s.first = s.first[1:]
			s.last = s.last[1:]

			if p1+uint32(len(s.pat)) >= s.ends[fileIdx] {
				continue
			}

			fileStart := uint32(0)
			if fileIdx > 0 {
				fileStart += s.ends[fileIdx-1]
			}
			candidates = append(candidates,
				candidateMatch{
					uint32(fileIdx),
					p1 - fileStart,
				})
		}
	}
	return candidates
}
