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
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"github.com/google/zoekt/build"
)

type fileAggregator struct {
	sizeMax int64
	sink    chan string
}

func (a *fileAggregator) add(path string, info os.FileInfo, err error) error {
	sz := info.Size()
	if sz > a.sizeMax || !info.Mode().IsRegular() {
		return nil
	}

	a.sink <- path
	return nil
}

func main() {
	var cpuProfile = flag.String("cpu_profile", "", "write cpu profile to file")
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")

	indexTemplate := flag.String("index",
		"{{.Home}}/.csindex/{{.Base}}.{{.FP}}.{{.Shard}}",
		"template for index file to use.")

	flag.Parse()

	opts := build.Options{
		Parallelism:      *parallelism,
		SizeMax:          *sizeMax,
		ShardMax:         *shardLimit,
		FileNameTemplate: *indexTemplate,
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	for _, arg := range flag.Args() {
		if err := indexArg(arg, opts); err != nil {
			log.Fatal(err)
		}
	}
}

func indexArg(arg string, opts build.Options) error {
	dir, err := filepath.Abs(filepath.Clean(arg))
	if err != nil {
		return err
	}

	opts.RepoName = filepath.Base(dir)
	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

	comm := make(chan string, 100)
	agg := fileAggregator{
		sink:    comm,
		sizeMax: int64(opts.SizeMax),
	}

	go func() {
		if err := filepath.Walk(dir, agg.add); err != nil {
			log.Fatal(err)
		}
		close(comm)
	}()

	for f := range comm {
		content, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}

		f = strings.TrimPrefix(f, dir+"/")
		builder.AddFile(f, content)
	}

	return builder.Finish()
}
