package cache

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mutableDTO struct {
	Items  []string
	Fields map[string]string
}

type batchResult struct {
	values map[int]*mutableDTO
	err    error
}

func TestConcurrentBatch(t *testing.T) {
	t.Run("overlapping leaders batch new keys and canceled leader leaves waiters intact", func(t *testing.T) {
		client, _ := memoryClient()
		started := make(chan []int, 3)
		sourceContexts := make(chan context.Context, 3)
		release := make(chan struct{})
		loader := func(ctx context.Context, keys []int) (map[int]*mutableDTO, error) {
			started <- append([]int(nil), keys...)
			sourceContexts <- ctx
			<-release
			values := make(map[int]*mutableDTO)
			for _, key := range keys {
				values[key] = &mutableDTO{Items: []string{"original"}, Fields: map[string]string{"field": "original"}}
			}
			return values, nil
		}
		policy := Policy{Namespace: "overlap", TTL: time.Minute}
		leaderCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		leaderDone := make(chan error, 1)
		go func() {
			_, err := MGet(leaderCtx, client, []int{1, 2}, strconv.Itoa, policy, loader)
			leaderDone <- err
		}()
		require.Equal(t, []int{1, 2}, <-started)
		firstSourceCtx := <-sourceContexts
		results := make(chan batchResult, 2)
		go func() {
			values, err := MGet(context.Background(), client, []int{2, 3, 4}, strconv.Itoa, policy, loader)
			results <- batchResult{values, err}
		}()
		require.Equal(t, []int{3, 4}, <-started, "new leader keys must remain one source batch")
		cancel()
		require.ErrorIs(t, <-leaderDone, context.Canceled)
		require.NoError(t, firstSourceCtx.Err(), "first request must not cancel shared source work")
		joining := make(chan struct{})
		go func() {
			keyCalls := 0
			keyOf := func(key int) string {
				if key == 2 {
					keyCalls++
					if keyCalls == 2 {
						close(joining)
					}
				}
				return strconv.Itoa(key)
			}
			values, err := MGet(context.Background(), client, []int{2, 3, 4}, keyOf, policy, loader)
			results <- batchResult{values, err}
		}()
		<-joining
		close(release)
		a, b := <-results, <-results
		require.NoError(t, a.err)
		require.NoError(t, b.err)
		require.Len(t, a.values, 3)
		require.Len(t, b.values, 3)
		a.values[2].Items[0] = "changed"
		a.values[2].Fields["field"] = "changed"
		require.Equal(t, "original", b.values[2].Items[0])
		require.Equal(t, "original", b.values[2].Fields["field"])
		require.Empty(t, started, "overlapping keys must not be reloaded")
	})
	t.Run("waiter cancellation is immediate", func(t *testing.T) {
		client, _ := memoryClient()
		started := make(chan struct{})
		release := make(chan struct{})
		loader := func(context.Context, []int) (map[int]string, error) {
			close(started)
			<-release
			return map[int]string{1: "loaded"}, nil
		}
		done := make(chan error, 1)
		go func() {
			_, err := MGet(context.Background(), client, []int{1}, strconv.Itoa, Policy{}, loader)
			done <- err
		}()
		<-started
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := MGet(ctx, client, []int{1}, strconv.Itoa, Policy{}, loader)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		close(release)
		require.NoError(t, <-done)
	})
	t.Run("physical loader slots remain held after ignored cancellation", func(t *testing.T) {
		client, _ := memoryClient()
		client.loadTimeout = 80 * time.Millisecond
		client.limit = make(chan struct{}, 2)
		started := make(chan struct{}, 2)
		release := make(chan struct{})
		var active, peak int32
		loader := func(ctx context.Context, keys []int) (map[int]string, error) {
			count := atomic.AddInt32(&active, 1)
			for old := atomic.LoadInt32(&peak); count > old && !atomic.CompareAndSwapInt32(&peak, old, count); old = atomic.LoadInt32(&peak) {
			}
			defer atomic.AddInt32(&active, -1)
			started <- struct{}{}
			<-release // Deliberately violates ctx to test the containment boundary.
			return map[int]string{keys[0]: "late"}, nil
		}
		done := make(chan error, 2)
		for _, key := range []int{1, 2} {
			go func(key int) {
				_, err := MGet(context.Background(), client, []int{key}, strconv.Itoa, Policy{TTL: time.Minute}, loader)
				done <- err
			}(key)
		}
		<-started
		<-started
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
		require.ErrorIs(t, <-done, context.DeadlineExceeded)
		require.EqualValues(t, 2, client.Stats()[""].LoaderError, "shared task timeouts are observable even if the source ignores cancellation")
		client.mu.Lock()
		remaining := len(client.flights)
		client.mu.Unlock()
		require.Zero(t, remaining, "deadline must remove every flight registration")
		_, err := MGet(context.Background(), client, []int{3}, strconv.Itoa, Policy{}, loader)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.EqualValues(t, 2, atomic.LoadInt32(&peak))
		require.EqualValues(t, 2, atomic.LoadInt32(&active))
		close(release)
		require.Eventually(t, func() bool { return len(client.limit) == 0 }, time.Second, time.Millisecond)
	})
	t.Run("source panic releases flights and later call can retry", func(t *testing.T) {
		client, _ := memoryClient()
		_, err := MGet(context.Background(), client, []int{1}, strconv.Itoa, Policy{},
			func(context.Context, []int) (map[int]string, error) { panic("bad source") })
		require.ErrorContains(t, err, "bad source")
		failure := errors.New("retry source error")
		values, err := MGet(context.Background(), client, []int{1}, strconv.Itoa, Policy{},
			func(context.Context, []int) (map[int]string, error) { return map[int]string{1: "known"}, failure })
		require.ErrorIs(t, err, failure)
		require.Equal(t, map[int]string{1: "known"}, values)
	})
}
