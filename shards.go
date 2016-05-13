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

package zoekt

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/zoekt/query"
)

type searchShard struct {
	Searcher
	mtime time.Time
}

type shardedSearcher struct {
	dir    string
	mu     sync.Mutex
	shards map[string]*searchShard
	quit   chan struct{}
}

func loadShard(fn string) (*searchShard, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}

	iFile, err := NewIndexFile(f)
	if err != nil {
		return nil, err
	}
	s, err := NewSearcher(iFile)
	if err != nil {
		return nil, fmt.Errorf("NewSearcher(%s): %v", fn, err)
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &searchShard{
		mtime:    fi.ModTime(),
		Searcher: s,
	}, nil
}

func (s *shardedSearcher) scan() error {
	fs, err := filepath.Glob(filepath.Join(s.dir, "*.zoekt"))
	if err != nil {
		return err
	}

	if len(fs) == 0 {
		return fmt.Errorf("directory %s is empty", s.dir)
	}

	ts := map[string]time.Time{}
	for _, fn := range fs {
		key := filepath.Base(fn)
		fi, err := os.Lstat(fn)
		if err != nil {
			continue
		}

		ts[key] = fi.ModTime()
	}

	var todo []string
	s.mu.Lock()
	for k, mtime := range ts {
		if s.shards[k] == nil || s.shards[k].mtime != mtime {
			todo = append(todo, k)
		}
	}
	s.mu.Unlock()

	for _, t := range todo {
		shard, err := loadShard(filepath.Join(s.dir, t))
		log.Printf("reloading: %s, err %v ", t, err)
		if err != nil {
			continue
		}
		s.replace(t, shard)
	}

	// TODO - unload deleted shards?
	return nil
}

func (s *shardedSearcher) replace(key string, shard *searchShard) {
	s.mu.Lock()
	old := s.shards[key]
	if old != nil {
		old.Close()
	}
	s.shards[key] = shard
	s.mu.Unlock()
}

func (s *shardedSearcher) watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(s.dir); err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-watcher.Events:
				s.scan()
			case err := <-watcher.Errors:
				log.Println("watcher error:", err)
			case <-s.quit:
				watcher.Close()
				break
			}
		}
	}()
	return nil
}

// NewShardedSearcher returns a searcher instance that loads all
// shards corresponding to a glob into memory.
func NewShardedSearcher(dir string) (Searcher, error) {
	ss := shardedSearcher{
		dir:    dir,
		shards: make(map[string]*searchShard),
		quit:   make(chan struct{}, 1),
	}

	if err := ss.scan(); err != nil {
		return nil, err
	}

	if err := ss.watch(); err != nil {
		return nil, err
	}

	return &ss, nil
}

func (ss *shardedSearcher) Close() {
	if ss.quit == nil {
		return
	}

	close(ss.quit)
	ss.quit = nil
	ss.mu.Lock()
	for _, s := range ss.shards {
		s.Close()
	}
	ss.mu.Unlock()
}

func (ss *shardedSearcher) Search(pat query.Q) (*SearchResult, error) {
	start := time.Now()
	type res struct {
		sr  *SearchResult
		err error
	}

	// This critical section is large, but we don't want to deal with
	// searches on a shards that have just been closed.
	shardCount := len(ss.shards)
	ss.mu.Lock()
	all := make(chan res, shardCount)
	for _, s := range ss.shards {
		go func(s Searcher) {
			ms, err := s.Search(pat)
			all <- res{ms, err}
		}(s)
	}
	ss.mu.Unlock()

	aggregate := SearchResult{
		RepoURLs: map[string]string{},
	}
	for i := 0; i < shardCount; i++ {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		aggregate.Files = append(aggregate.Files, r.sr.Files...)
		aggregate.Stats.Add(r.sr.Stats)
		for k, v := range r.sr.RepoURLs {
			aggregate.RepoURLs[k] = v
		}
	}
	sortFilesByScore(aggregate.Files)
	aggregate.Duration = time.Now().Sub(start)
	return &aggregate, nil
}
