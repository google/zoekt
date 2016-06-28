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
	"runtime"
	"sort"
	"time"

	"golang.org/x/net/context"

	"github.com/fsnotify/fsnotify"
	"github.com/google/zoekt/query"
)

type searchShard struct {
	Searcher
	mtime time.Time
}

type shardedSearcher struct {
	dir string

	// Limit the number of parallel queries. Since searching is
	// CPU bound, we can't do better than #CPU queries in
	// parallel.  If we do so, we just create more memory
	// pressure.
	throttle chan struct{}

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

	s.lock()
	var toLoad []string
	for k, mtime := range ts {
		if s.shards[k] == nil || s.shards[k].mtime != mtime {
			toLoad = append(toLoad, k)
		}
	}

	var toDrop []string
	// Unload deleted shards.
	for k := range s.shards {
		if _, ok := ts[k]; !ok {
			toDrop = append(toDrop, k)
		}
	}
	s.unlock()

	for _, t := range toDrop {
		log.Printf("unloading: %s", t)
		s.replace(t, nil)
	}

	for _, t := range toLoad {
		shard, err := loadShard(filepath.Join(s.dir, t))
		log.Printf("reloading: %s, err %v ", t, err)
		if err != nil {
			continue
		}
		s.replace(t, shard)
	}

	return nil
}

func (s *shardedSearcher) lock() {
	n := cap(s.throttle)
	for n > 0 {
		s.throttle <- struct{}{}
		n--
	}
}

func (s *shardedSearcher) unlock() {
	n := cap(s.throttle)
	for n > 0 {
		<-s.throttle
		n--
	}
}

func (s *shardedSearcher) replace(key string, shard *searchShard) {
	s.lock()
	defer s.unlock()
	old := s.shards[key]
	if old != nil {
		old.Close()
	}
	if shard != nil {
		s.shards[key] = shard
	} else {
		delete(s.shards, key)
	}
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
				if err != nil {
					log.Println("watcher error:", err)
				}
			case <-s.quit:
				watcher.Close()
				return
			}
		}
	}()
	return nil
}

// NewShardedSearcher returns a searcher instance that loads all
// shards corresponding to a glob into memory.
func NewShardedSearcher(dir string) (Searcher, error) {
	ss := shardedSearcher{
		dir:      dir,
		shards:   make(map[string]*searchShard),
		quit:     make(chan struct{}, 1),
		throttle: make(chan struct{}, runtime.NumCPU()),
	}

	if err := ss.scan(); err != nil {
		return nil, err
	}

	if err := ss.watch(); err != nil {
		return nil, err
	}

	return &ss, nil
}

// Close closes references to open files. It may be called only once.
func (ss *shardedSearcher) Close() {
	close(ss.quit)
	ss.lock()
	defer ss.unlock()
	for _, s := range ss.shards {
		s.Close()
	}
}

func (ss *shardedSearcher) Stats() (*RepoStats, error) {
	var r RepoStats
	for _, s := range ss.shards {
		s, err := s.Stats()
		if err != nil {
			return nil, err
		}
		r.Add(s)
	}
	return &r, nil
}

func (ss *shardedSearcher) Search(ctx context.Context, pat query.Q, opts *SearchOptions) (*SearchResult, error) {
	start := time.Now()
	type res struct {
		sr  *SearchResult
		err error
	}

	aggregate := SearchResult{
		RepoURLs: map[string]string{},
	}

	// This critical section is large, but we don't want to deal with
	// searches on a shards that have just been closed.
	ss.throttle <- struct{}{}
	aggregate.Wait = time.Now().Sub(start)
	start = time.Now()

	// TODO - allow for canceling the query.

	shardCount := len(ss.shards)
	all := make(chan res, shardCount)
	childCtx, cancel := context.WithCancel(ctx)
	for _, s := range ss.shards {
		go func(s Searcher) {
			ms, err := s.Search(childCtx, pat, opts)
			all <- res{ms, err}
		}(s)
	}
	<-ss.throttle

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

		if cancel != nil && aggregate.Stats.MatchCount > opts.TotalMaxMatchCount {
			cancel()
			cancel = nil
		}
	}

	sortFilesByScore(aggregate.Files)
	aggregate.Duration = time.Now().Sub(start)
	return &aggregate, nil
}

func (ss *shardedSearcher) List(ctx context.Context, r query.Q) (*RepoList, error) {
	type res struct {
		rl  *RepoList
		err error
	}

	ss.throttle <- struct{}{}
	shardCount := len(ss.shards)
	all := make(chan res, shardCount)
	for _, s := range ss.shards {
		go func(s Searcher) {
			ms, err := s.List(ctx, r)
			all <- res{ms, err}
		}(s)
	}
	<-ss.throttle

	uniq := map[string]struct{}{}
	for i := 0; i < shardCount; i++ {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		for _, r := range r.rl.Repos {
			uniq[r] = struct{}{}
		}
	}
	var aggregate []string
	for k := range uniq {
		aggregate = append(aggregate, k)
	}

	sort.Strings(aggregate)
	return &RepoList{aggregate}, nil
}
