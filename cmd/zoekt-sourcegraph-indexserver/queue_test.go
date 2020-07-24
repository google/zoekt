package main

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/google/zoekt"
)

func TestQueue(t *testing.T) {
	queue := &Queue{}

	for i := 0; i < 100; i++ {
		queue.AddOrUpdate(fmt.Sprintf("item-%d", i), mkHEADIndexOptions(strconv.Itoa(i)))
	}

	// Odd numbers are already at the same commit
	for i := 1; i < 100; i += 2 {
		queue.SetIndexed(fmt.Sprintf("item-%d", i), mkHEADIndexOptions(strconv.Itoa(i)), indexStateSuccess)
	}

	// Ensure we process all the even commits first, then odd.
	want := 0
	for {
		name, opts, ok := queue.Pop()
		if !ok {
			break
		}
		got, _ := strconv.Atoi(opts.Branches[0].Version)
		if got != want {
			t.Fatalf("got %v %v, want %v", name, opts, want)
		}
		want += 2
		if want == 100 {
			// We now switch to processing the odd numbers
			want = 1
		}
		// update current, shouldn't put the job in the queue
		queue.SetIndexed(name, opts, indexStateSuccess)
	}
	if want != 101 {
		t.Fatalf("only popped %d items", want)
	}
}

func TestQueueFIFO(t *testing.T) {
	// Tests that the queue fallbacks to FIFO if everything has the same
	// priority
	queue := &Queue{}

	for i := 0; i < 100; i++ {
		queue.AddOrUpdate(fmt.Sprintf("item-%d", i), mkHEADIndexOptions(strconv.Itoa(i)))
	}

	want := 0
	for {
		name, opts, ok := queue.Pop()
		if !ok {
			break
		}
		got, _ := strconv.Atoi(opts.Branches[0].Version)
		if got != want {
			t.Fatalf("got %v %v, want %v", name, opts, want)
		}
		queue.SetIndexed(name, opts, indexStateSuccess)
		want++
	}
	if want != 100 {
		t.Fatalf("only popped %d items", want)
	}
}

func TestQueue_MaybeRemoveMissing(t *testing.T) {
	queue := &Queue{}

	queue.AddOrUpdate("foo", mkHEADIndexOptions("foo"))
	queue.AddOrUpdate("bar", mkHEADIndexOptions("bar"))
	queue.MaybeRemoveMissing([]string{"bar"})

	name, _, _ := queue.Pop()
	if name != "bar" {
		t.Fatalf("queue should only contain bar, pop returned %v", name)
	}
	_, _, ok := queue.Pop()
	if ok {
		t.Fatal("queue should be empty")
	}
}

func mkHEADIndexOptions(version string) IndexOptions {
	return IndexOptions{
		Branches: []zoekt.RepositoryBranch{{Name: "HEAD", Version: version}},
	}
}
