package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
)

// indexArgs represents the arguments we pass to zoekt-archive-index
type indexArgs struct {
	// Root is the base URL for the Sourcegraph instance to index. Normally
	// http://sourcegraph-frontend-internal or http://localhost:3090.
	Root *url.URL

	// Name is the name of the repository.
	Name string

	// Commit is the absolute commit SHA.
	Commit string

	// Incremental indicates to skip indexing if already indexed.
	Incremental bool

	// IndexDir is the index directory to store the shards.
	IndexDir string

	// Parallelism is the number of shards to compute in parallel.
	Parallelism int

	// FileLimit is the maximum size of a file
	FileLimit int

	// Branch is the branch name.
	Branch string

	// DownloadLimitMBPS is the maximum MB/s to use when downloading the
	// archive.
	DownloadLimitMBPS string

	// LargeFiles is a slice of glob patterns where matching files are indexed
	// regardless of their size.
	LargeFiles []string

	// Symbols is a boolean that indicates whether to generate ctags metadata
	// or not
	Symbols bool
}

// BuildOptions returns a build.Options represented by indexArgs. Note: it
// doesn't set fields like repository/branch.
func (o *indexArgs) BuildOptions() *build.Options {
	return &build.Options{
		// It is important that this RepositoryDescription exactly matches
		// what the indexer we call will produce. This is to ensure that
		// IncrementalSkipIndexing returns true if nothing needs to be done.
		RepositoryDescription: zoekt.Repository{
			Name: o.Name,
			Branches: []zoekt.RepositoryBranch{{
				Name:    o.Branch,
				Version: o.Commit,
			}},
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
	if o.Branch != "" {
		return o.Name + "@" + o.Branch + "=" + o.Commit
	} else {
		return o.Name + "@" + o.Commit
	}
}

func getIndexOptions(args *indexArgs) error {
	u := args.Root.ResolveReference(&url.URL{Path: "/.internal/search/configuration"})
	resp, err := client.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("failed to get configuration options")
	}

	err = json.NewDecoder(resp.Body).Decode(&args)
	if err != nil {
		return fmt.Errorf("error decoding body: %v", err)
	}

	return nil
}

func archiveIndex(o *indexArgs, runCmd func(*exec.Cmd) error) error {
	args := []string{
		"-name", o.Name,
		"-commit", o.Commit,
	}

	// Even though we check for incremental in this process, we still pass it
	// in just in case we regress in how we check in process. We will still
	// notice thanks to metrics and increased load on gitserver.
	if o.Incremental {
		args = append(args, "-incremental")
	}

	if o.Branch != "" {
		args = append(args, "-branch", o.Branch)
	}

	if o.DownloadLimitMBPS != "" {
		args = append(args, "-download-limit-mbps", o.DownloadLimitMBPS)
	}

	args = append(args, o.BuildOptions().Args()...)

	args = append(args, o.Root.ResolveReference(&url.URL{Path: fmt.Sprintf("/.internal/git/%s/tar/%s", o.Name, o.Commit)}).String())

	cmd := exec.Command("zoekt-archive-index", args...)
	// Prevent prompting
	cmd.Stdin = &bytes.Buffer{}
	return runCmd(cmd)
}
