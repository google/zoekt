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
	"path/filepath"
	"runtime/pprof"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
)

const CONTEXT = 20

func displayMatches(files []zoekt.FileMatch, pat string) {
	for _, f := range files {
		for _, m := range f.Matches {
			fmt.Printf("%s:%d:%s\n", f.Name, m.LineNum, m.Line)
		}
	}
}

func main() {
	index := flag.String("index_dir",
		filepath.Join(os.Getenv("HOME"), ".zoekt"), "search for index files in `directory`")
	cpuProfile := flag.String("cpu_profile", "", "write cpu profile to `file`")
	verbose := flag.Bool("v", false, "print some background data")

	flag.Usage = func() {
		name := os.Args[0]
		fmt.Fprintf(os.Stderr, "Usage:\n\n  %s [option] QUERY\n"+
			`for example\n\n  %s 'byte file:java -file:test'`+"\n\n", name, name)
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

	searcher, err := zoekt.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	query, err := query.Parse(pat)
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		log.Println("query:", query)
	}

	var sOpts zoekt.SearchOptions
	sres, err := searcher.Search(query, &sOpts)
	if *cpuProfile != "" {
		// If profiling, do it another time so we measure with
		// warm caches.
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.Println("Displaying matches...")
		pprof.StartCPUProfile(f)
		for i := 0; i < 10; i++ {
			sres, err = searcher.Search(query, &sOpts)
		}
		pprof.StopCPUProfile()
	}

	if err != nil {
		log.Fatal(err)
	}

	displayMatches(sres.Files, pat)
	if *verbose {
		log.Printf("stats: %#v", sres.Stats)
	}
}
