package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestIndexArgs(t *testing.T) {
	root, err := url.Parse("http://api.test")
	if err != nil {
		t.Fatal(err)
	}

	minimal := indexArgs{
		Name:   "test/repo",
		Commit: "deadbeef",
	}
	want := []string{
		"-name", "test/repo",
		"-commit", "deadbeef",
		"-disable_ctags",
		"http://api.test/.internal/git/test/repo/tar/deadbeef",
	}
	if got := minimal.CmdArgs(root); !cmp.Equal(got, want) {
		t.Errorf("all mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}

	all := indexArgs{
		Name:              "test/repo",
		Commit:            "deadbeef",
		Incremental:       true,
		IndexDir:          "/data/index",
		Parallelism:       4,
		FileLimit:         123,
		Branch:            "HEAD",
		DownloadLimitMBPS: "1000",
		LargeFiles:        []string{"foo", "bar"},
		Symbols:           true,
	}
	want = []string{
		"-name", "test/repo",
		"-commit", "deadbeef",
		"-incremental",
		"-branch", "HEAD",
		"-download-limit-mbps", "1000",
		"-file_limit", "123",
		"-parallelism", "4",
		"-index", "/data/index",
		"-require_ctags",
		"-large_file", "foo",
		"-large_file", "bar",
		"http://api.test/.internal/git/test/repo/tar/deadbeef",
	}
	if got := all.CmdArgs(root); !cmp.Equal(got, want) {
		t.Errorf("all mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}
}

func TestServer_defaultArgs(t *testing.T) {
	s := &Server{
		IndexDir: "/testdata/index",
		CPUCount: 6,
	}
	want := indexArgs{
		IndexDir:          "/testdata/index",
		Parallelism:       6,
		Incremental:       true,
		Branch:            "HEAD",
		FileLimit:         1 << 20,
		DownloadLimitMBPS: "1000",
	}
	got := s.defaultArgs()
	if !cmp.Equal(got, want) {
		t.Errorf("mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}
}

func TestGetIndexOptions(t *testing.T) {
	var response []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(response)
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]indexArgs{
		`{"Symbols": true, "LargeFiles": ["foo","bar"]}`: indexArgs{
			Symbols:    true,
			LargeFiles: []string{"foo", "bar"},
		},

		`{"Symbols": false, "LargeFiles": ["foo","bar"]}`: indexArgs{
			LargeFiles: []string{"foo", "bar"},
		},

		`{}`: indexArgs{},

		`{"Symbols": true}`: indexArgs{
			Symbols: true,
		},
	}

	for r, want := range cases {
		response = []byte(r)

		// Test we mix in the response
		want.Name = "test/repo"
		want.Commit = "deadbeef"
		got := indexArgs{
			Name:   "test/repo",
			Commit: "deadbeef",
		}

		if err := getIndexOptions(u, &got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !cmp.Equal(got, want) {
			t.Log("response", r)
			t.Errorf("mismatch (-want +got):\n%s", cmp.Diff(want, got))
		}
	}
}

func TestListRepos(t *testing.T) {
	var gotBody string
	var gotURL *url.URL
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL

		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(b)

		_, err = w.Write([]byte(`{"RepoNames": ["foo", "bar", "baz"]}`))
		if err != nil {
			t.Fatal(err)
		}
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	gotRepos, err := listRepos(context.Background(), "test-indexed-search-1", u, []string{"foo", "bam"})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"foo", "bar", "baz"}; !cmp.Equal(gotRepos, want) {
		t.Errorf("repos mismatch (-want +got):\n%s", cmp.Diff(want, gotRepos))
	}
	if want := `{"Hostname":"test-indexed-search-1","Indexed":["foo","bam"]}`; gotBody != want {
		t.Errorf("body mismatch (-want +got):\n%s", cmp.Diff(want, gotBody))
	}
	if want := "/.internal/repos/index"; gotURL.Path != want {
		t.Errorf("request path mismatch (-want +got):\n%s", cmp.Diff(want, gotURL.Path))
	}
}

func TestPing(t *testing.T) {
	var response []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.internal/ping" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("service") != "gitserver" {
			http.Error(w, "expected service gitserver in request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write(response)
	}))
	defer server.Close()

	root, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Ping fails
	response = []byte("hello")
	err = ping(root)
	if got, want := fmt.Sprintf("%v", err), "did not receive pong"; !strings.Contains(got, want) {
		t.Errorf("wanted ping to fail,\ngot:  %q\nwant: %q", got, want)
	}

	response = []byte("pong")
	err = ping(root)
	if err != nil {
		t.Errorf("wanted ping to succeed, got: %v", err)
	}

	// We expect waitForFrontend to just work now
	done := make(chan struct{})
	go func() {
		waitForFrontend(root)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waitForFrontend blocking")
	}
}
