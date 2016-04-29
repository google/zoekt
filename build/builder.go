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
	"log"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"github.com/google/zoekt"
)

// Options sets options for the index building.
type Options struct {
	// FileNameTemplate is a template to use for output files.
	// Available substitution keys are Home ($HOME), Base (the
	// repo name), FP a repo fingerprint (path to repository) and
	// Shard (shard number)
	FileNameTemplate string

	// Maximum file size
	SizeMax int

	// Parallelism is the maximum number of shards to index in parallel
	Parallelism int

	// ShardMax sets the maximum corpus size for a single shard
	ShardMax int

	// RepoName is name of the repository.
	RepoName string
}

type entry struct {
	name     string
	content  []byte
	branches []string
}

// Builder manages (parallel) creation of uniformly sized shards.
type Builder struct {
	opts     Options
	throttle chan int
	name     *template.Template

	nextShardNum int
	todo         []entry
	size         int

	building sync.WaitGroup

	errMu      sync.Mutex
	buildError error
}

// NewBuilder creates a new Builder instance.
func NewBuilder(opt Options) (*Builder, error) {
	tpl, err := template.New("index").Parse(opt.FileNameTemplate)
	if err != nil {
		return nil, err
	}

	return &Builder{
		opts:     opt,
		name:     tpl,
		throttle: make(chan int, opt.Parallelism),
	}, nil
}

func (b *Builder) AddFile(name string, content []byte) {
	b.AddFileBranches(name, content, nil)
}

// TODO - this should be an Add(content []byte, FileOptions) where
// FileOptions contains filename, branches, offsets of interesting
// sections (symbols, comments etc.)

func (b *Builder) AddFileBranches(name string, content []byte, branches []string) {
	if len(content) > b.opts.SizeMax {
		return
	}
	if bytes.IndexByte(content, 0) != -1 {
		return
	}

	b.todo = append(b.todo, entry{name, content, branches})
	b.size += len(name) + len(content)

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

func (b *Builder) buildShard(todo []entry, nextShardNum int) error {
	name, err := shardName(b.name, b.opts.RepoName, nextShardNum)
	if err != nil {
		return err
	}
	shardBuilder := zoekt.NewIndexBuilder()
	shardBuilder.SetName(b.opts.RepoName)
	for _, t := range todo {
		shardBuilder.AddFileBranches(t.name, t.content, t.branches)
	}
	return writeShard(name, shardBuilder)
}

func shardName(tpl *template.Template, repo string, shardNum int) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	fp := stableHash(filepath.Dir(abs))

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, struct {
		Home, FP, Base, Shard string
	}{
		os.Getenv("HOME"), fp, filepath.Base(abs),
		fmt.Sprintf("%05d", shardNum),
	}); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func writeShard(fn string, b *zoekt.IndexBuilder) error {
	if err := os.MkdirAll(filepath.Dir(fn), 0700); err != nil {
		return err
	}

	// TODO - write to temp file and rename on close.  Use a
	// standard extension so the temp file is never recognized as
	// an index file.

	f, err := os.OpenFile(
		fn, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
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
	log.Printf("wrote %s: %d index bytes (overhead %3.1f)", fn, fi.Size(),
		float64(fi.Size())/float64(b.ContentSize()+1))
	return nil
}

func stableHash(in string) string {
	h := md5.New()
	h.Write([]byte(in))
	return fmt.Sprintf("%x", h.Sum(nil)[:6])
}
