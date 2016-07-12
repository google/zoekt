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
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	"github.com/google/zoekt/web"
)

const logFormat = "2006-01-02T15-04-05.999999999Z07"

func divertLogs(dir string, interval time.Duration) {
	t := time.NewTicker(interval)
	var last *os.File
	for {
		nm := filepath.Join(dir, fmt.Sprintf("zoekt-webserver.%s.%d.log", time.Now().Format(logFormat), os.Getpid()))
		fmt.Fprintf(os.Stderr, "writing logs to %s\n", nm)

		f, err := os.Create(nm)
		if err != nil {
			// There is not much we can do now.
			fmt.Fprintf(os.Stderr, "can't create output file %s: %v\n", nm, err)
			os.Exit(2)
		}

		log.SetOutput(f)
		last.Close()

		last = f

		<-t.C
	}
}

func main() {
	logDir := flag.String("log_dir", "", "If set, log to this directory rather than stderr.")
	logRefresh := flag.Duration("log_refresh", 24*time.Hour, "if using --log_dir, start writing a new file this often.")

	listen := flag.String("listen", ":6070", "address to listen on.")
	index := flag.String("index", build.DefaultDir, "index directory to use")
	print := flag.Bool("print", false, "local result URLs")
	flag.Parse()

	if *logDir != "" {
		if fi, err := os.Lstat(*logDir); err != nil || !fi.IsDir() {
			log.Fatal("%s is not a directory", *logDir)
		}
		// We could do fdup acrobatics to also redirect
		// stderr, but it is simpler and more portable for the
		// caller to divert stderr output if necessary.
		go divertLogs(*logDir, *logRefresh)
	}

	searcher, err := zoekt.NewShardedSearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	s := &web.Server{
		Searcher:   searcher,
		DidYouMean: web.DidYouMeanTemplate,
		RepoList:   web.RepoListTemplate,
		Result:     web.ResultTemplate,
		SearchBox:  web.SearchBoxTemplate,
	}
	if *print {
		s.Print = web.PrintTemplate
	}

	handler := web.NewMux(s)
	log.Printf("serving on %s", *listen)
	err = http.ListenAndServe(*listen, handler)
	log.Printf("ListenAndServe: %v", err)
}
