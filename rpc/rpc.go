// Package rpc provides a zoekt.Searcher over RPC using net/rpc.
package rpc

import (
	"context"
	"encoding/gob"
	"fmt"
	"net/rpc"
	"sync"

	"github.com/google/zoekt"
	"github.com/google/zoekt/query"
	"github.com/google/zoekt/rpc/internal/srv"
)

// Register will register the searcher on server. To do remote calls against
// searcher, use DialHTTP to the server.
func Register(server *rpc.Server, searcher zoekt.Searcher) {
	registerGob()
	server.Register(&srv.Searcher{Searcher: searcher})
}

// DialHTTP connects to a Searcher HTTP RPC server at the specified network
// address listening.
func DialHTTP(address string) (zoekt.Searcher, error) {
	registerGob()
	cl, err := rpc.DialHTTP("tcp", address)
	if err != nil {
		return nil, err
	}
	return &client{
		cl:      cl,
		address: address,
	}, nil
}

type client struct {
	cl      *rpc.Client
	address string
}

func (c *client) Search(ctx context.Context, q query.Q, opts *zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	var reply srv.SearchReply
	err := c.call(ctx, "Searcher.Search", &srv.SearchArgs{Q: q, Opts: opts}, &reply)
	return reply.Result, err
}

func (c *client) List(ctx context.Context, q query.Q) (*zoekt.RepoList, error) {
	var reply srv.ListReply
	err := c.call(ctx, "Searcher.List", &srv.ListArgs{Q: q}, &reply)
	return reply.List, err
}

func (c *client) call(ctx context.Context, serviceMethod string, args interface{}, reply interface{}) error {
	call := c.cl.Go(serviceMethod, args, reply, make(chan *rpc.Call, 1))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.Done:
		return call.Error
	}
}

func (c *client) Close() {
	c.cl.Close()
}

func (c *client) String() string {
	return fmt.Sprintf("rpcSearcher(%s)", c.address)
}

var once sync.Once

func registerGob() {
	once.Do(func() {
		gob.Register(&query.And{})
		gob.Register(&query.Or{})
		gob.Register(&query.Regexp{})
		gob.Register(&query.Language{})
		gob.Register(&query.Const{})
		gob.Register(&query.Repo{})
		gob.Register(&query.RepoSet{})
		gob.Register(&query.Substring{})
		gob.Register(&query.Not{})
		gob.Register(&query.Branch{})
	})
}
