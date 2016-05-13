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
	"os"
	"syscall"
)

type mmapedIndexFile struct {
	f    *os.File
	data []byte
}

func (f *mmapedIndexFile) Read(off, sz uint32) ([]byte, error) {
	return f.data[off : off+sz], nil
}

func (f *mmapedIndexFile) Size() (uint32, error) {
	fi, err := f.f.Stat()
	if err != nil {
		return 0, err
	}

	sz := fi.Size()

	if sz >= maxUInt32 {
		return 0, fmt.Errorf("overflow")
	}

	return uint32(sz), nil
}

func (f *mmapedIndexFile) Close() {
	f.f.Close()
}

func NewIndexFile(f *os.File) (IndexFile, error) {
	r := &mmapedIndexFile{f: f}

	rounded, err := r.Size()
	if err != nil {
		return nil, err
	}
	rounded = (rounded + 4095) &^ 4095
	r.data, err = syscall.Mmap(int(f.Fd()), 0, int(rounded), syscall.PROT_READ, syscall.MAP_SHARED)
	return r, err
}
