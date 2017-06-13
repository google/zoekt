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

package shards

import (
	"log"
	"runtime"
	"runtime/debug"
	"sort"
	"time"

	"golang.org/x/net/context"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
)

// NewShardedSearcher returns a searcher instance that loads all
// shards corresponding to a glob into memory.
func NewShardedSearcher(dir string) (zoekt.Searcher, error) {
	ss := newShardWatcher(dir)
	if err := ss.scan(); err != nil {
		return nil, err
	}

	if err := ss.watch(); err != nil {
		return nil, err
	}

	return &shardedSearcher{ss}, nil
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
	getShards() []zoekt.Searcher
	rlock()
	runlock()
	String() string
}

type shardedSearcher struct {
	shardLoader
}

func (ss *shardedSearcher) Search(ctx context.Context, pat query.Q, opts *zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	start := time.Now()
	type res struct {
		sr  *zoekt.SearchResult
		err error
	}

	aggregate := zoekt.SearchResult{
		RepoURLs:      map[string]string{},
		LineFragments: map[string]string{},
	}

	// This critical section is large, but we don't want to deal with
	// searches on shards that have just been closed.
	ss.shardLoader.rlock()
	defer ss.shardLoader.runlock()
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

	// For each query, throttle the number of parallel
	// actions. Since searching is mostly CPU bound, we limit the
	// number of parallel searches. This reduces the peak working
	// set, which hopefully stops https://cs.bazel.build from crashing
	// when looking for the string "com".
	throttle := make(chan int, 10*runtime.NumCPU())
	for _, s := range shards {
		go func(s zoekt.Searcher) {
			throttle <- 1
			defer func() {
				<-throttle
				if r := recover(); r != nil {
					log.Printf("crashed shard: %s: %s, %s", s.String(), r, debug.Stack())

					var r zoekt.SearchResult
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

	zoekt.SortFilesByScore(aggregate.Files)
	aggregate.Duration = time.Now().Sub(start)
	return &aggregate, nil
}

func (ss *shardedSearcher) List(ctx context.Context, r query.Q) (*zoekt.RepoList, error) {
	type res struct {
		rl  *zoekt.RepoList
		err error
	}

	ss.rlock()
	defer ss.runlock()

	shards := ss.getShards()
	shardCount := len(shards)
	all := make(chan res, shardCount)

	for _, s := range shards {
		go func(s zoekt.Searcher) {
			defer func() {
				if r := recover(); r != nil {
					all <- res{
						&zoekt.RepoList{Crashes: 1}, nil,
					}
				}
			}()
			ms, err := s.List(ctx, r)
			all <- res{ms, err}
		}(s)
	}

	crashes := 0
	uniq := map[string]*zoekt.RepoListEntry{}

	var names []string
	for i := 0; i < shardCount; i++ {
		r := <-all
		if r.err != nil {
			return nil, r.err
		}
		crashes += r.rl.Crashes
		for _, r := range r.rl.Repos {
			prev, ok := uniq[r.Repository.Name]
			if !ok {
				cp := *r
				uniq[r.Repository.Name] = &cp
				names = append(names, r.Repository.Name)
			} else {
				prev.Stats.Add(&r.Stats)
			}
		}
	}
	sort.Strings(names)

	aggregate := make([]*zoekt.RepoListEntry, 0, len(names))
	for _, k := range names {
		aggregate = append(aggregate, uniq[k])
	}
	return &zoekt.RepoList{
		Repos:   aggregate,
		Crashes: crashes,
	}, nil
}
