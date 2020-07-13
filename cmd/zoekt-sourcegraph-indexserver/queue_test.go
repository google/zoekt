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
		queue.SetIndexed(fmt.Sprintf("item-%d", i), mkHEADIndexOptions(strconv.Itoa(i)))
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
		queue.SetIndexed(name, opts)
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
		queue.SetIndexed(name, opts)
		want++
	}
	if want != 100 {
		t.Fatalf("only popped %d items", want)
	}
}

func mkHEADIndexOptions(version string) IndexOptions {
	return IndexOptions{
		Branches: []zoekt.RepositoryBranch{{Name: "HEAD", Version: version}},
	}
}
