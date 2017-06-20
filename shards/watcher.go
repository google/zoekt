// Copyright 2017 Google Inc. All rights reserved.
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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/zoekt"
)

type searchShard struct {
	zoekt.Searcher
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

	iFile, err := zoekt.NewIndexFile(f)
	if err != nil {
		return nil, err
	}
	s, err := zoekt.NewSearcher(iFile)
	if err != nil {
		iFile.Close()
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
func (s *shardWatcher) getShards() []zoekt.Searcher {
	var res []zoekt.Searcher
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
