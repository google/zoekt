// This binary fetches all repos on a sourcegraph instance.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/google/zoekt/gitindex"
)

func main() {
	dest := flag.String("dest", "", "destination directory")
	namePattern := flag.String("name", "", "only clone repos whose name matches the given regexp.")
	excludePattern := flag.String("exclude", "", "don't mirror repos whose names match this regexp.")
	flag.Parse()

	if len(flag.Args()) < 1 {
		log.Fatal("must provide URL argument. Probably http://sourcegraph-frontend-internal or http://localhost:3090")
	}

	root, err := url.Parse(flag.Arg(0))
	if err != nil {
		log.Fatal("url.Parse(): %v", err)
	}

	if *dest == "" {
		log.Fatal("must set --dest")
	}
	if err := os.MkdirAll(*dest, 0755); err != nil {
		log.Fatal(err)
	}

	filter, err := gitindex.NewFilter(*namePattern, *excludePattern)
	if err != nil {
		log.Fatal(err)
	}

	repos, err := listRepos(root)
	if err != nil {
		log.Fatal(err)
	}

	{
		trimmed := repos[:0]
		for _, r := range repos {
			if filter.Include(r) {
				trimmed = append(trimmed, r)
			}
		}
		repos = trimmed
	}

	if err := cloneRepos(*dest, root, repos); err != nil {
		log.Fatal(err)
	}
}

func listRepos(root *url.URL) ([]string, error) {
	u := root.ResolveReference(&url.URL{Path: "/.internal/repos/list"})
	resp, err := http.Post(u.String(), "application/json; charset=utf8", bytes.NewReader([]byte(`{"PerPage": 10000}`)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Repos []struct {
			URI string
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	repos := make([]string, len(data.Repos))
	for i, r := range data.Repos {
		repos[i] = r.URI
	}
	return repos, nil
}

func cloneRepos(destDir string, root *url.URL, repos []string) error {
	var firstErr error
	for _, r := range repos {
		config := map[string]string{
			"zoekt.name": r,
			// A lie, but we need to set this since zoekt currently has a
			// false assumption. It clones into the repo_cache based on the
			// name we pass to CloneRepo. However, in the indexserver (and
			// some other places) it maps an index to something in the
			// repo_cache based on the web-url. The underlying bug should be
			// fixed, but I am not familiar enough with internals to do
			// that. - keegan
			"zoekt.web-url": "https://" + r,
		}
		cloneURL := root.ResolveReference(&url.URL{Path: "/.internal/git/" + r})
		if err := gitindex.CloneRepo(destDir, r, cloneURL.String(), config); err != nil {
			firstErr = err
		}
	}
	return firstErr
}
