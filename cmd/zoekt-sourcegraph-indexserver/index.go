package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
)

// IndexOptions are the options that Sourcegraph can set via it's search
// configuration endpoint.
type IndexOptions struct {
	// LargeFiles is a slice of glob patterns where matching file paths should
	// be indexed regardless of their size. The pattern syntax can be found
	// here: https://golang.org/pkg/path/filepath/#Match.
	LargeFiles []string

	// Symbols if true will make zoekt index the output of ctags.
	Symbols bool

	// Branches is a slice of branches to index.
	Branches []zoekt.RepositoryBranch
}

// indexArgs represents the arguments we pass to zoekt-archive-index
type indexArgs struct {
	IndexOptions

	// Root is the base URL for the Sourcegraph instance to index. Normally
	// http://sourcegraph-frontend-internal or http://localhost:3090.
	Root *url.URL

	// Name is the name of the repository.
	Name string

	// Incremental indicates to skip indexing if already indexed.
	Incremental bool

	// IndexDir is the index directory to store the shards.
	IndexDir string

	// Parallelism is the number of shards to compute in parallel.
	Parallelism int

	// FileLimit is the maximum size of a file
	FileLimit int

	// DownloadLimitMBPS is the maximum MB/s to use when downloading the
	// archive.
	DownloadLimitMBPS string
}

// BuildOptions returns a build.Options represented by indexArgs. Note: it
// doesn't set fields like repository/branch.
func (o *indexArgs) BuildOptions() *build.Options {
	return &build.Options{
		// It is important that this RepositoryDescription exactly matches
		// what the indexer we call will produce. This is to ensure that
		// IncrementalSkipIndexing returns true if nothing needs to be done.
		RepositoryDescription: zoekt.Repository{
			Name:     o.Name,
			Branches: o.Branches,
		},
		IndexDir:         o.IndexDir,
		Parallelism:      o.Parallelism,
		SizeMax:          o.FileLimit,
		LargeFiles:       o.LargeFiles,
		CTagsMustSucceed: o.Symbols,
		DisableCTags:     !o.Symbols,
	}
}

func (o *indexArgs) String() string {
	s := o.Name
	for i, b := range o.Branches {
		if i == 0 {
			s = fmt.Sprintf("%s@%s=%s", s, b.Name, b.Version)
		} else {
			s = fmt.Sprintf("%s,%s=%s", s, b.Name, b.Version)
		}
	}
	return s
}

func getIndexOptions(root *url.URL, repoName string) (IndexOptions, error) {
	u := root.ResolveReference(&url.URL{
		Path:     "/.internal/search/configuration",
		RawQuery: "repo=" + url.QueryEscape(repoName),
	})

	resp, err := client.Get(u.String())
	if err != nil {
		return IndexOptions{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		if err != nil {
			return IndexOptions{}, err
		}
		return IndexOptions{}, &url.Error{
			Op:  "Get",
			URL: u.String(),
			Err: fmt.Errorf("%s: %s", resp.Status, string(b)),
		}
	}

	var opts IndexOptions
	err = json.NewDecoder(resp.Body).Decode(&opts)
	if err != nil {
		return IndexOptions{}, fmt.Errorf("error decoding body: %w", err)
	}

	return opts, nil
}

func archiveIndex(o *indexArgs, runCmd func(*exec.Cmd) error) error {
	if len(o.Branches) != 1 {
		return fmt.Errorf("zoekt-archive-index only supports 1 branch, got %v", o.Branches)
	}

	commit := o.Branches[0].Version
	args := []string{
		"-name", o.Name,
		"-commit", commit,
		"-branch", o.Branches[0].Name,
	}

	// Even though we check for incremental in this process, we still pass it
	// in just in case we regress in how we check in process. We will still
	// notice thanks to metrics and increased load on gitserver.
	if o.Incremental {
		args = append(args, "-incremental")
	}

	if o.DownloadLimitMBPS != "" {
		args = append(args, "-download-limit-mbps", o.DownloadLimitMBPS)
	}

	args = append(args, o.BuildOptions().Args()...)

	args = append(args, o.Root.ResolveReference(&url.URL{Path: fmt.Sprintf("/.internal/git/%s/tar/%s", o.Name, commit)}).String())

	cmd := exec.Command("zoekt-archive-index", args...)
	// Prevent prompting
	cmd.Stdin = &bytes.Buffer{}
	return runCmd(cmd)
}

func gitIndex(o *indexArgs, runCmd func(*exec.Cmd) error) error {
	if len(o.Branches) == 0 {
		return errors.New("zoekt-git-index requires 1 or more branches")
	}

	gitDir, err := tmpGitDir(o.Name)
	if err != nil {
		return err
	}
	// We intentionally leave behind gitdir if indexing failed so we can
	// investigate. This is only during the experimental phase of indexing a
	// clone. So don't defer os.RemoveAll here

	cloneURL := o.Root.ResolveReference(&url.URL{Path: path.Join("/.internal/git", o.Name)}).String()
	cmd := exec.Command("git", "-c", "protocol.version=2", "clone", "--depth=1", "--bare", cloneURL, gitDir)
	cmd.Stdin = &bytes.Buffer{}
	if err := runCmd(cmd); err != nil {
		return err
	}

	cmd = exec.Command("git", "-C", gitDir, "config", "zoekt.name", o.Name)
	cmd.Stdin = &bytes.Buffer{}
	if err := runCmd(cmd); err != nil {
		return err
	}

	args := []string{
		"-submodules=false",
	}

	// Note: we ignore o.Commit, we just fetch/clone the branch. This means the
	// commit could be different (due to the branch being updated). This is not
	// an issue since this commit will be more up to date. Additionally we will
	// eventually converge thanks to the -incremental check. The only downside
	// is the log message for indexing may report we indexed a different commit
	// than we actually did.

	// Even though we check for incremental in this process, we still pass it
	// in just in case we regress in how we check in process. We will still
	// notice thanks to metrics and increased load on gitserver.
	if o.Incremental {
		args = append(args, "-incremental")
	}

	args = append(args, "-branches", o.Branches[0].Name)

	args = append(args, o.BuildOptions().Args()...)
	args = append(args, gitDir)

	cmd = exec.Command("zoekt-git-index", args...)
	cmd.Stdin = &bytes.Buffer{}
	if err := runCmd(cmd); err != nil {
		return err
	}

	// Do not return error, since we have successfully indexed. Just log it
	if err := os.RemoveAll(gitDir); err != nil {
		log.Printf("WARN: failed to cleanup %s after successfully indexing %s: %v", gitDir, o.String(), err)
	}

	return nil
}

func tmpGitDir(name string) (string, error) {
	abs := url.QueryEscape(name)
	if len(abs) > 200 {
		h := sha1.New()
		io.WriteString(h, abs)
		abs = abs[:200] + fmt.Sprintf("%x", h.Sum(nil))[:8]
	}
	dir := filepath.Join(os.TempDir(), abs+".git")
	if _, err := os.Stat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	return dir, nil
}
