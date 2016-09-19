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

package build

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/zoekt"
	"github.com/google/zoekt/ctags"
)

func runCTags(bin string, sandboxBin string, inputs map[string][]byte) ([]*ctags.Entry, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	dir, err := ioutil.TempDir("", "ctags-input")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	args := []string{bin, "-n", "-f", "-"}

	fileCount := 0
	for n, c := range inputs {
		if len(c) == 0 {
			continue
		}

		full := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			return nil, err
		}
		err := ioutil.WriteFile(full, c, 0600)
		if err != nil {
			return nil, err
		}
		args = append(args, n)
		fileCount++
	}
	if fileCount == 0 {
		return nil, nil
	}

	if sandboxBin != "" {
		// ctags parses untrusted input and is written in C.
		// Run it in a sandbox as defense in depth.  The
		// namespace sandbox only works on Linux,
		// unfortunately.
		sandboxDir, err := ioutil.TempDir("", "")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(sandboxDir)

		sandboxArgs := []string{
			sandboxBin,
			"-t30", "-T30",
			"-D", "-S", sandboxDir, "-M", dir, "-m", "/input", "-W", "/input",
			// Make sure the binary is available in the sandbox.
			"-M", bin, "-m", "/ctags",
		}
		args[0] = "/ctags"
		for _, d := range []string{"/bin", "/lib", "/usr/bin", "/lib64"} {
			if _, err := os.Lstat(d); err == nil {
				sandboxArgs = append(sandboxArgs, "-M", d, "-m", d)
			}
		}

		sandboxArgs = append(sandboxArgs, "--")
		args = append(sandboxArgs, args...)
	} else {
		log.Println("WARNING: running ctags without sandboxing.")
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec(%s): %v, stderr: %s", cmd.Args, err, errBuf.String())
	}

	var entries []*ctags.Entry
	for _, l := range bytes.Split(out, []byte{'\n'}) {
		if len(l) == 0 {
			continue
		}
		e, err := ctags.Parse(string(l))
		if err != nil {
			return nil, err
		}

		if len(e.Sym) == 1 {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func runCTagsChunked(bin, sandboxBin string, in map[string][]byte) ([]*ctags.Entry, error) {
	var res []*ctags.Entry

	cur := map[string][]byte{}
	sz := 0
	for k, v := range in {
		cur[k] = v
		sz += len(k)

		// 100k seems reasonable.
		if sz > (100 << 10) {
			r, err := runCTags(bin, sandboxBin, cur)
			if err != nil {
				return nil, err
			}
			res = append(res, r...)

			cur = map[string][]byte{}
			sz = 0
		}
	}
	r, err := runCTags(bin, sandboxBin, cur)
	if err != nil {
		return nil, err
	}
	res = append(res, r...)
	return res, nil
}

func ctagsAddSymbols(todo []*zoekt.Document, bin, sandboxBin string) error {
	pathIndices := map[string]int{}
	contents := map[string][]byte{}
	for i, t := range todo {
		if t.Symbols != nil {
			continue
		}

		_, ok := pathIndices[t.Name]
		if ok {
			continue
		}

		pathIndices[t.Name] = i
		contents[t.Name] = t.Content
	}

	entries, err := runCTagsChunked(bin, sandboxBin, contents)
	if err != nil {
		return err
	}

	fileTags := map[string][]*ctags.Entry{}
	for _, e := range entries {
		fileTags[e.Path] = append(fileTags[e.Path], e)
	}

	for k, tags := range fileTags {
		symOffsets, err := tagsToSections(contents[k], tags)
		if err != nil {
			return err
		}
		todo[pathIndices[k]].Symbols = symOffsets
	}
	return nil
}

func tagsToSections(content []byte, tags []*ctags.Entry) ([]zoekt.DocumentSection, error) {
	nls := newLinesIndices(content)
	nls = append(nls, uint32(len(content)))
	var symOffsets []zoekt.DocumentSection
	for _, t := range tags {
		if t.Line <= 0 {
			// Observed this with a .JS file.
			continue
		}
		lineIdx := t.Line - 1
		if lineIdx >= len(nls) {
			log.Println("nls", nls)
			return nil, fmt.Errorf("linenum for entry out of range %v", t)
		}

		lineOff := uint32(0)
		if lineIdx > 0 {
			lineOff = nls[lineIdx-1] + 1
		}

		end := nls[lineIdx]
		line := content[lineOff:end]

		intraOff := bytes.Index(line, []byte(t.Sym))
		if intraOff < 0 {
			// for Go code, this is very common, since
			// ctags barfs on multi-line declarations
			// log.Printf("symbol %s not found in line
			// %q", t.Sym, line)
			continue
		}
		symOffsets = append(symOffsets, zoekt.DocumentSection{
			Start: lineOff + uint32(intraOff),
			End:   lineOff + uint32(intraOff) + uint32(len(t.Sym)),
		})
	}

	return symOffsets, nil
}

func newLinesIndices(in []byte) []uint32 {
	out := make([]uint32, 0, len(in)/30)
	for i, c := range in {
		if c == '\n' {
			out = append(out, uint32(i))
		}
	}
	return out
}
