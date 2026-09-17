package utils

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestGenInt64IDReturnsPositiveUniqueIDsConcurrently(t *testing.T) {
	const workers, each = 32, 256
	ids := make(chan int64, workers*each)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < each; i++ {
				ids <- GenInt64ID()
			}
		}()
	}
	group.Wait()
	close(ids)
	seen := make(map[int64]bool, workers*each)
	for id := range ids {
		if id <= 0 {
			t.Fatalf("generated non-positive ID: %d", id)
		}
		if seen[id] {
			t.Fatalf("concurrent IDs collided: %d", id)
		}
		seen[id] = true
	}
}

func TestGenInt64IDRejectsExhaustedPositiveRange(t *testing.T) {
	idGenerator.Lock()
	previous := idGenerator.last
	idGenerator.last = math.MaxInt64
	idGenerator.Unlock()
	defer func() {
		panicValue := recover()
		idGenerator.Lock()
		current := idGenerator.last
		idGenerator.last = previous
		idGenerator.Unlock()
		if panicValue == nil {
			t.Fatal("exhaustion returned an invalid ID instead of failing")
		}
		if current != math.MaxInt64 {
			t.Fatal("exhaustion corrupted the last valid ID")
		}
	}()
	GenInt64ID()
}

func TestGenInt64IDRemainsPositiveAndMonotonicAfterClockRollback(t *testing.T) {
	// A previously issued timestamp ahead of the wall clock models rollback.
	idGenerator.Lock()
	previous := idGenerator.last
	future := time.Now().Add(time.Hour).UnixNano()
	idGenerator.last = future
	idGenerator.Unlock()
	t.Cleanup(func() {
		idGenerator.Lock()
		idGenerator.last = previous
		idGenerator.Unlock()
	})
	first, second := GenInt64ID(), GenInt64ID()
	if first <= future || second <= first || first <= 0 || second <= 0 {
		t.Fatal("clock rollback produced a non-positive or repeated ID")
	}
}
