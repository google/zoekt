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

type shardWatcher struct {
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
	fi, err := f.Stat()
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

	return &searchShard{
		mtime:    fi.ModTime(),
		Searcher: s,
	}, nil
}

func (s *shardWatcher) String() string {
	return fmt.Sprintf("shardWatcher(%s)", s.dir)
}

func (s *shardWatcher) scan() error {
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

func (s *shardWatcher) rlock() {
	s.throttle <- struct{}{}
}

// getShards returns the currently loaded shards. The shards must be
// accessed under a rlock call.
func (s *shardWatcher) getShards() []Searcher {
	var res []Searcher
	for _, sh := range s.shards {
		res = append(res, sh)
	}
	return res
}

func (s *shardWatcher) runlock() {
	<-s.throttle
}

func (s *shardWatcher) lock() {
	n := cap(s.throttle)
	for n > 0 {
		s.throttle <- struct{}{}
		n--
	}
}

func (s *shardWatcher) unlock() {
	n := cap(s.throttle)
	for n > 0 {
		<-s.throttle
		n--
	}
}

func (s *shardWatcher) replace(key string, shard *searchShard) {
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

func (s *shardWatcher) watch() error {
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
	ss := shardWatcher{
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

	return &shardedSearcher{&ss}, nil
}

// Close closes references to open files. It may be called only once.
func (ss *shardWatcher) Close() {
	close(ss.quit)
	ss.lock()
	defer ss.unlock()
	for _, s := range ss.shards {
		s.Close()
	}
}

type shardLoader interface {
	Close()
	getShards() []Searcher
	rlock()
	runlock()
	String() string
}

type shardedSearcher struct {
	shardLoader
}

func (ss *shardedSearcher) Stats() (*RepoStats, error) {
	var r RepoStats
	ss.rlock()
	defer ss.runlock()
	for _, s := range ss.shardLoader.getShards() {
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
		RepoURLs:      map[string]string{},
		LineFragments: map[string]string{},
	}

	// This critical section is large, but we don't want to deal with
	// searches on shards that have just been closed.
	ss.shardLoader.rlock()
	aggregate.Wait = time.Now().Sub(start)
	start = time.Now()

	// TODO - allow for canceling the query.
	shards := ss.getShards()
	all := make(chan res, len(shards))

	var childCtx context.Context
	var cancel context.CancelFunc
	if opts.MaxWallTime == 0 {
		childCtx, cancel = context.WithCancel(ctx)
	} else {
		childCtx, cancel = context.WithTimeout(ctx, opts.MaxWallTime)
	}

	defer cancel()

	for _, s := range shards {
		go func(s Searcher) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("crashed shard: %s: %s", s.String(), r)

					var r SearchResult
					r.Stats.Crashes = 1
					all <- res{&r, nil}
				}
			}()
			ms, err := s.Search(childCtx, pat, opts)
			all <- res{ms, err}
		}(s)
	}

	for range shards {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		aggregate.Files = append(aggregate.Files, r.sr.Files...)
		aggregate.Stats.Add(r.sr.Stats)

		if len(r.sr.Files) > 0 {
			for k, v := range r.sr.RepoURLs {
				aggregate.RepoURLs[k] = v
			}
			for k, v := range r.sr.LineFragments {
				aggregate.LineFragments[k] = v
			}
		}

		if cancel != nil && aggregate.Stats.MatchCount > opts.TotalMaxMatchCount {
			cancel()
			cancel = nil
		}
	}
	ss.runlock()

	sortFilesByScore(aggregate.Files)
	aggregate.Duration = time.Now().Sub(start)
	return &aggregate, nil
}

func (ss *shardedSearcher) List(ctx context.Context, r query.Q) (*RepoList, error) {
	type res struct {
		rl  *RepoList
		err error
	}

	ss.rlock()
	shards := ss.getShards()
	shardCount := len(shards)
	all := make(chan res, shardCount)
	for _, s := range shards {
		go func(s Searcher) {
			defer func() {
				if r := recover(); r != nil {
					all <- res{
						&RepoList{Crashes: 1}, nil,
					}
				}
			}()
			ms, err := s.List(ctx, r)
			all <- res{ms, err}
		}(s)
	}

	crashes := 0
	uniq := map[string]*Repository{}
	for i := 0; i < shardCount; i++ {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		crashes += r.rl.Crashes
		for _, r := range r.rl.Repos {
			uniq[r.Name] = r
		}
	}
	ss.runlock()

	var names []string
	for n := range uniq {
		names = append(names, n)
	}
	sort.Strings(names)

	var aggregate []*Repository
	for _, k := range names {
		aggregate = append(aggregate, uniq[k])
	}
	return &RepoList{
		Repos:   aggregate,
		Crashes: crashes,
	}, nil
}
