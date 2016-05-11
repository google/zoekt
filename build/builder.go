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

// package build implements a more convenient interface for building
// zoekt indices.
package build

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/zoekt"
)

var DefaultDir = filepath.Join(os.Getenv("HOME"), ".zoekt")

// Options sets options for the index building.
type Options struct {
	// IndexDir is a directory that holds *.zoekt index files.
	IndexDir string

	// Maximum file size
	SizeMax int

	// Parallelism is the maximum number of shards to index in parallel
	Parallelism int

	// ShardMax sets the maximum corpus size for a single shard
	ShardMax int

	// RepoName is name of the repository.
	RepoName string

	// RepoURL is the URL template for the repository.
	RepoURL string
}

// Builder manages (parallel) creation of uniformly sized shards.
type Builder struct {
	opts     Options
	throttle chan int

	nextShardNum int
	todo         []zoekt.Document
	size         int

	building sync.WaitGroup

	errMu      sync.Mutex
	buildError error
}

// NewBuilder creates a new Builder instance.
func NewBuilder(opt Options) (*Builder, error) {
	return &Builder{
		opts:     opt,
		throttle: make(chan int, opt.Parallelism),
	}, nil
}

func (b *Builder) AddFile(name string, content []byte) {
	b.Add(zoekt.Document{Name: name, Content: content})
}

func (b *Builder) Add(doc zoekt.Document) {
	if len(doc.Content) > b.opts.SizeMax {
		return
	}
	if bytes.IndexByte(doc.Content, 0) != -1 {
		return
	}

	b.todo = append(b.todo, doc)
	b.size += len(doc.Name) + len(doc.Content)

	if b.size > b.opts.ShardMax {
		b.flush()
	}
}

func (b *Builder) Finish() error {
	b.flush()
	b.building.Wait()
	return b.buildError
}

func (b *Builder) flush() {
	todo := b.todo
	b.todo = nil
	b.size = 0
	b.errMu.Lock()
	defer b.errMu.Unlock()
	if b.buildError != nil {
		return
	}

	if len(todo) == 0 {
		return
	}

	shard := b.nextShardNum
	b.nextShardNum++
	b.building.Add(1)
	go func() {
		b.throttle <- 1
		err := b.buildShard(todo, shard)
		<-b.throttle

		b.errMu.Lock()
		defer b.errMu.Unlock()
		if err != nil && b.buildError == nil {
			b.buildError = err
		}
		b.building.Done()
	}()
}

func (b *Builder) buildShard(todo []zoekt.Document, nextShardNum int) error {
	name, err := shardName(b.opts.IndexDir, b.opts.RepoName, nextShardNum)
	if err != nil {
		return err
	}
	shardBuilder := zoekt.NewIndexBuilder()
	shardBuilder.SetName(b.opts.RepoName)
	shardBuilder.SetRepoURL(b.opts.RepoURL)
	for _, t := range todo {
		shardBuilder.Add(t)
	}
	return writeShard(name, shardBuilder)
}

func shardName(dir string, repo string, shardNum int) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	fp := stableHash(filepath.Dir(abs))

	return filepath.Join(dir,
		fmt.Sprintf("%s.%s.%05d.zoekt", filepath.Base(abs), fp, shardNum)), nil
}

func writeShard(fn string, b *zoekt.IndexBuilder) error {
	dir := filepath.Dir(fn)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := ioutil.TempFile(dir, filepath.Base(fn))
	if err != nil {
		return err
	}

	defer f.Close()
	if err := b.Write(f); err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), fn); err != nil {
		return err
	}

	log.Printf("wrote %s: %d index bytes (overhead %3.1f)", fn, fi.Size(),
		float64(fi.Size())/float64(b.ContentSize()+1))
	return nil
}

func stableHash(in string) string {
	h := md5.New()
	h.Write([]byte(in))
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}
