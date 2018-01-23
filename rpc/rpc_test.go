package rpc_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
	"github.com/google/zoekt/rpc"
)

func TestClientServer(t *testing.T) {
	mock := &mockSearcher{
		wantSearch: query.NewAnd(mustParse("hello world|universe"), query.NewRepoSet("foo/bar", "baz/bam")),
		searchResult: &zoekt.SearchResult{
			Files: []zoekt.FileMatch{
				{FileName: "bin.go"},
			},
		},

		wantList: &query.Const{Value: true},
		repoList: &zoekt.RepoList{
			Repos: []*zoekt.RepoListEntry{
				{
					Repository: zoekt.Repository{
						Name: "foo/bar",
					},
				},
			},
		},
	}

	ts := httptest.NewServer(rpc.Server(mock))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := rpc.Client(u.Host)

	r, err := client.Search(context.Background(), mock.wantSearch, &zoekt.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, mock.searchResult) {
		t.Fatalf("got %+v, want %+v", r, mock.searchResult)
	}

	l, err := client.List(context.Background(), mock.wantList)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, mock.repoList) {
		t.Fatalf("got %+v, want %+v", l, mock.repoList)
	}
}

type mockSearcher struct {
	wantSearch   query.Q
	searchResult *zoekt.SearchResult

	wantList query.Q
	repoList *zoekt.RepoList
}

func (s *mockSearcher) Search(ctx context.Context, q query.Q, opts *zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	if q.String() != s.wantSearch.String() {
		return nil, fmt.Errorf("got query %s != %s", q.String(), s.wantSearch.String())
	}
	return s.searchResult, nil
}

func (s *mockSearcher) List(ctx context.Context, q query.Q) (*zoekt.RepoList, error) {
	if q.String() != s.wantList.String() {
		return nil, fmt.Errorf("got query %s != %s", q.String(), s.wantList.String())
	}
	return s.repoList, nil
}

func (*mockSearcher) Close() {}

func (*mockSearcher) String() string {
	return "mockSearcher"
}

func mustParse(s string) query.Q {
	q, err := query.Parse(s)
	if err != nil {
		panic(err)
	}
	return q
}
