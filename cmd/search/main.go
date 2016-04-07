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

package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/hanwen/codesearch"
)

// go1.4
func lastIndex(b string, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

const CONTEXT = 20

func displayMatches(matches []codesearch.Match, pat string) {
	for _, m := range matches {
		fmt.Printf("%s:%d:%s\n", m.Name, m.LineNum, m.Line)
	}
}

func main() {
	index := flag.String("index", ".csindex.*", "index file glob to use")
	flag.Parse()

	searcher, err := codesearch.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	if len(flag.Args()) == 0 {
		log.Fatal("needs argument")
	}
	pat := flag.Arg(0)
	ms, err := searcher.Search(pat)
	if err != nil {
		log.Fatal(err)
	}

	displayMatches(ms, pat)
}
