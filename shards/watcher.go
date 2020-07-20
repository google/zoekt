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
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/fsnotify/fsnotify"
)

type shardLoader interface {
	// Load a new file. Should be safe for concurrent calls.
	load(filename string)
	drop(filename string)
}

type shardWatcher struct {
	dir        string
	timestamps map[string]time.Time
	loader     shardLoader
	quit       chan<- struct{}
}

func (sw *shardWatcher) Close() error {
	if sw.quit != nil {
		close(sw.quit)
		sw.quit = nil
	}
	return nil
}

func NewDirectoryWatcher(dir string, loader shardLoader) (io.Closer, error) {
	quitter := make(chan struct{}, 1)
	sw := &shardWatcher{
		dir:        dir,
		timestamps: map[string]time.Time{},
		loader:     loader,
		quit:       quitter,
	}
	if err := sw.scan(); err != nil {
		return nil, err
	}

	if err := sw.watch(quitter); err != nil {
		return nil, err
	}

	return sw, nil
}

func (s *shardWatcher) String() string {
	return fmt.Sprintf("shardWatcher(%s)", s.dir)
}

func (s *shardWatcher) scan() error {
	fs, err := filepath.Glob(filepath.Join(s.dir, "*.zoekt"))
	if err != nil {
		return err
	}

	if len(s.timestamps) == 0 && len(fs) == 0 {
		return fmt.Errorf("directory %s is empty", s.dir)
	}

	ts := map[string]time.Time{}
	for _, fn := range fs {
		fi, err := os.Lstat(fn)
		if err != nil {
			continue
		}

		ts[fn] = fi.ModTime()
	}

	var toLoad []string
	for k, mtime := range ts {
		if t, ok := s.timestamps[k]; !ok || t != mtime {
			toLoad = append(toLoad, k)
			s.timestamps[k] = mtime
		}
	}

	var toDrop []string
	// Unload deleted shards.
	for k := range s.timestamps {
		if _, ok := ts[k]; !ok {
			toDrop = append(toDrop, k)
			delete(s.timestamps, k)
		}
	}

	if len(toDrop) > 0 {
		log.Printf("unloading %d shards", len(toDrop))
	}
	for _, t := range toDrop {
		log.Printf("unloading: %s", t)
		s.loader.drop(t)
	}

	if len(toLoad) == 0 {
		return nil
	}

	log.Printf("loading %d shards", len(toLoad))

	// Limit amount of concurrent shard loads.
	throttle := make(chan struct{}, runtime.GOMAXPROCS(0))
	lastProgress := time.Now()
	for i, t := range toLoad {
		// If taking a while to start-up occasionally give a progress message
		if time.Since(lastProgress) > 10*time.Second {
			log.Printf("still need to load %d shards...", len(toLoad)-i)
			lastProgress = time.Now()
		}

		throttle <- struct{}{}
		go func(k string) {
			s.loader.load(k)
			<-throttle
		}(t)
	}
	for i := 0; i < cap(throttle); i++ {
		throttle <- struct{}{}
	}

	return nil
}

func (s *shardWatcher) watch(quitter <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(s.dir); err != nil {
		return err
	}

	// intermediate signal channel so if there are multiple watcher.Events we
	// only call scan once.
	signal := make(chan struct{}, 1)

	go func() {
		for {
			select {
			case <-watcher.Events:
				select {
				case signal <- struct{}{}:
				default:
				}
			case err := <-watcher.Errors:
				// Ignore ErrEventOverflow since we rely on the presence of events so
				// safe to ignore.
				if err != nil && err != fsnotify.ErrEventOverflow {
					log.Println("watcher error:", err)
				}
			case <-quitter:
				watcher.Close()
				close(signal)
				return
			}
		}
	}()

	go func() {
		for range signal {
			s.scan()
		}
	}()

	return nil
}
