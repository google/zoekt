package main

import (
	"fmt"
	"strconv"
	"testing"
)

func TestQueue(t *testing.T) {
	queue := &Queue{}

	for i := 0; i < 100; i++ {
		queue.AddOrUpdate(fmt.Sprintf("item-%d", i), strconv.Itoa(i))
	}

	// Odd numbers are already at the same commit
	for i := 1; i < 100; i += 2 {
		queue.SetIndexed(fmt.Sprintf("item-%d", i), strconv.Itoa(i))
	}

	// Ensure we process all the even commits first, then odd.
	want := 0
	for {
		name, commit, ok := queue.Pop()
		if !ok {
			break
		}
		got, _ := strconv.Atoi(commit)
		if got != want {
			t.Fatalf("got %v %v, want %v", name, commit, want)
		}
		want += 2
		if want == 100 {
			// We now switch to processing the odd numbers
			want = 1
		}
		// update current, shouldn't put the job in the queue
		queue.SetIndexed(name, commit)
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
		queue.AddOrUpdate(fmt.Sprintf("item-%d", i), strconv.Itoa(i))
	}

	want := 0
	for {
		name, commit, ok := queue.Pop()
		if !ok {
			break
		}
		got, _ := strconv.Atoi(commit)
		if got != want {
			t.Fatalf("got %v %v, want %v", name, commit, want)
		}
		queue.SetIndexed(name, commit)
		want++
	}
	if want != 100 {
		t.Fatalf("only popped %d items", want)
	}
}
