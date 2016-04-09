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
	"os"
	"strings"

	"github.com/hanwen/codesearch"
)

const CONTEXT = 20

func displayMatches(matches []codesearch.Match, pat string) {
	for _, m := range matches {
		fmt.Printf("%s:%d:%s\n", m.Name, m.LineNum, m.Line)
	}
}

func main() {
	index := flag.String("index", ".csindex.*", "index file glob to use")
	caseSensitive := flag.Bool("case", false, "case sensitive search by default ")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n\n  %s [option] PATTERN\n"+
			"\nIf PATTERN has uppercase characters, the search is case sensitive.\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}

	flag.Parse()
	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Pattern is missing.\n")
		flag.Usage()
		os.Exit(2)
	}
	pat := flag.Arg(0)

	searcher, err := codesearch.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	q := &codesearch.SubstringQuery{
		Pattern:       pat,
		CaseSensitive: *caseSensitive || strings.ToLower(pat) != pat,
	}
	ms, err := searcher.Search(q)
	if err != nil {
		log.Fatal(err)
	}

	displayMatches(ms, pat)
}
