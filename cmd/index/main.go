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
	"bytes"
	"crypto/md5"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"text/template"

	"github.com/hanwen/codesearch"
)

type fileAggregator struct {
	chunks   chan<- []string
	files    []string
	total    int64
	shardMax int64
	sizeMax  int64
}

func (a *fileAggregator) flush() {
	a.chunks <- a.files
	a.files = nil
	close(a.chunks)
}

func (a *fileAggregator) add(path string, info os.FileInfo, err error) error {
	sz := info.Size()
	if sz > a.sizeMax || !info.Mode().IsRegular() {
		return nil
	}

	a.files = append(a.files, path)
	a.total += sz

	if a.total > a.shardMax {
		a.chunks <- a.files
		a.files = nil
		a.total = 0
	}
	return nil
}

func main() {
	var cpuProfile = flag.String("cpuprofile", "", "write cpu profile to file")
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")

	index := flag.String("index",
		"{{.Home}}/.csindex/{{.Base}}.{{.FP}}.{{.Shard}}",
		"index file to use. First %x argument is repo ID, second is shard number.")

	flag.Parse()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	for _, arg := range flag.Args() {
		if err := indexArg(arg, *index, *parallelism, *sizeMax, *shardLimit); err != nil {
			log.Fatal(err)
		}
	}
}

func stableHash(in string) string {
	h := md5.New()
	h.Write([]byte(in))
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}

func indexArg(arg string, indexTemplate string, parallelism, sizeMax, shardLimit int) error {
	tpl, err := template.New("index").Parse(indexTemplate)
	if err != nil {
		return err
	}

	shardNum := 0

	chunks := make(chan []string, 10)
	agg := fileAggregator{
		chunks:   chunks,
		sizeMax:  int64(sizeMax),
		shardMax: int64(shardLimit),
	}

	abs, err := filepath.Abs(arg)
	if err != nil {
		return err
	}
	fp := stableHash(filepath.Dir(abs))

	go func() {
		if err := filepath.Walk(arg, agg.add); err != nil {
			log.Fatal(err)
		}
		agg.flush()
	}()

	var wg sync.WaitGroup
	errors := make(chan error, 10)
	throttle := make(chan int, parallelism)

	for names := range chunks {
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, struct {
			Home, FP, Base, Shard string
		}{
			os.Getenv("HOME"), fp, filepath.Base(abs),
			fmt.Sprintf("%05d", shardNum),
		}); err != nil {
			return err
		}

		fn := buf.String()

		if err := os.MkdirAll(filepath.Dir(fn), 0700); err != nil {
			return err
		}
		shardNum++
		wg.Add(1)
		go func(nm []string) {
			throttle <- 1
			errors <- buildShard(fn, nm)
			<-throttle
			wg.Done()
		}(names)
	}

	go func() {
		wg.Wait()
		close(errors)
	}()

	var lastErr error
	for err := range errors {
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func buildShard(shardName string, files []string) error {
	f, err := os.OpenFile(
		shardName, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	b := codesearch.NewIndexBuilder()
	total := 0
	for _, a := range files {
		c, err := ioutil.ReadFile(a)
		if bytes.IndexByte(c, 0) != -1 {
			// skip binary
			continue
		}
		total += len(c)
		if err != nil {
			log.Println(err)
		} else {
			b.AddFile(a, c)
		}
	}

	if err := b.Write(f); err != nil {
		log.Println("Write", err)
	}
	if err := f.Close(); err != nil {
		log.Println("Write", err)
	}
	log.Printf("%s: indexed %d bytes\n", shardName, total)

	return nil
}
