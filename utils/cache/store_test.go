package cache

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	mu        sync.Mutex
	values    map[string][]byte
	writes    []cacheWrite
	reads     [][]string
	readErr   error
	writeErr  error
	deleteErr error
}

func (s *memoryStore) mget(ctx context.Context, keys []string) ([]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads = append(s.reads, append([]string(nil), keys...))
	result := make([]interface{}, len(keys))
	for i, key := range keys {
		if value, ok := s.values[key]; ok {
			result[i] = string(value)
		}
	}
	return result, s.readErr
}

func (s *memoryStore) write(ctx context.Context, writes []cacheWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writes = append(s.writes, writes...)
	for _, write := range writes {
		s.values[write.key] = append([]byte(nil), write.data...)
	}
	return nil
}

func (s *memoryStore) delete(ctx context.Context, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.values, key)
	}
	return s.deleteErr
}

func memoryClient() (*Client, *memoryStore) {
	store := &memoryStore{values: make(map[string][]byte)}
	client := New(nil, "test")
	client.store = store
	return client, store
}

func TestCacheRoundTrip(t *testing.T) {
	t.Run("zero TTL never creates immortal positive or negative entries", func(t *testing.T) {
		client, store := memoryClient()
		calls := 0
		loader := func(context.Context, []int) (map[int]string, error) {
			calls++
			return map[int]string{1: "present"}, nil
		}
		for i := 0; i < 2; i++ {
			values, err := MGet(context.Background(), client, []int{1, 2}, strconv.Itoa, Policy{}, loader)
			require.NoError(t, err)
			require.Equal(t, map[int]string{1: "present"}, values)
		}
		require.Equal(t, 2, calls)
		require.Empty(t, store.writes)
	})
	t.Run("zero values and confirmed absence are distinct", func(t *testing.T) {
		client, store := memoryClient()
		policy := Policy{Namespace: "zero", TTL: time.Minute, NegativeTTL: time.Second}
		calls := 0
		loader := func(ctx context.Context, keys []int) (map[int]bool, error) {
			calls++
			require.Equal(t, []int{1, 2}, keys)
			return map[int]bool{1: false}, nil
		}
		for i := 0; i < 2; i++ {
			values, err := MGet(context.Background(), client, []int{1, 2, 1}, strconv.Itoa, policy, loader)
			require.NoError(t, err)
			require.Equal(t, map[int]bool{1: false}, values)
		}
		require.Equal(t, 1, calls)
		require.Len(t, store.writes, 2)
		for _, write := range store.writes {
			ttl := policy.TTL
			if write.key == client.Key("zero", "2") {
				ttl = policy.NegativeTTL
			}
			require.InDelta(t, ttl, write.ttl, float64(ttl)/10+float64(time.Millisecond))
		}
	})
	t.Run("corrupt schema missing marker and expired data reload", func(t *testing.T) {
		client, store := memoryClient()
		for key, value := range []string{"broken", `{"schema":2,"found":false}`, `{"schema":1}`, `{"schema":1,"found":true,"value":"old","expires_at":1}`} {
			store.values[client.Key("schema", strconv.Itoa(key))] = []byte(value)
		}
		values, err := MGet(context.Background(), client, []int{0, 1, 2, 3}, strconv.Itoa,
			Policy{Namespace: "schema", TTL: time.Minute}, func(ctx context.Context, keys []int) (map[int]string, error) {
				require.Equal(t, []int{0, 1, 2, 3}, keys)
				return map[int]string{0: "a", 1: "b", 2: "c", 3: "d"}, nil
			})
		require.NoError(t, err)
		require.Equal(t, map[int]string{0: "a", 1: "b", 2: "c", 3: "d"}, values)
	})
	t.Run("source errors preserve successes and never negative cache incomplete keys", func(t *testing.T) {
		client, _ := memoryClient()
		failure := errors.New("upstream failed")
		var calls [][]int
		loader := func(ctx context.Context, keys []int) (map[int]string, error) {
			calls = append(calls, append([]int(nil), keys...))
			return map[int]string{1: "known"}, failure
		}
		policy := Policy{Namespace: "partial", TTL: time.Minute, NegativeTTL: time.Minute}
		for i := 0; i < 2; i++ {
			values, err := MGet(context.Background(), client, []int{1, 2}, strconv.Itoa, policy, loader)
			require.ErrorIs(t, err, failure)
			require.Equal(t, map[int]string{1: "known"}, values)
		}
		require.Equal(t, [][]int{{1, 2}, {2}}, calls)
	})
	t.Run("cache outages do not replace source result", func(t *testing.T) {
		client, store := memoryClient()
		store.readErr, store.writeErr = errors.New("redis read"), errors.New("redis write")
		values, err := MGet(context.Background(), client, []int{1, 2}, strconv.Itoa,
			Policy{TTL: time.Minute}, func(context.Context, []int) (map[int]string, error) {
				return map[int]string{1: "live", 2: "source"}, nil
			})
		require.NoError(t, err)
		require.Equal(t, map[int]string{1: "live", 2: "source"}, values)
	})
	t.Run("large values return but are not stored", func(t *testing.T) {
		client, store := memoryClient()
		value := strings.Repeat("x", 300)
		values, err := MGet(context.Background(), client, []int{1}, strconv.Itoa,
			Policy{TTL: time.Minute, MaxValueBytes: 128}, func(context.Context, []int) (map[int]string, error) {
				return map[int]string{1: value}, nil
			})
		require.NoError(t, err)
		require.Equal(t, value, values[1])
		require.Empty(t, store.writes)
	})
	t.Run("expiry caps jitter and expired credentials are never stored", func(t *testing.T) {
		client, store := memoryClient()
		deadline := time.Now().Add(200 * time.Millisecond)
		values, err := MGet(context.Background(), client, []int{1, 2}, strconv.Itoa,
			Policy{TTL: time.Hour}, func(context.Context, []int) (map[int]fixedExpiry, error) {
				return map[int]fixedExpiry{1: {Value: "fresh", Until: deadline}, 2: {Value: "expired", Until: time.Now().Add(-time.Second)}}, nil
			})
		require.NoError(t, err)
		require.Len(t, values, 2)
		require.Len(t, store.writes, 1)
		require.Equal(t, client.Key("", "1"), store.writes[0].key)
		require.Positive(t, store.writes[0].ttl)
		require.LessOrEqual(t, store.writes[0].ttl, 200*time.Millisecond)
	})
	t.Run("delete invalidates full keys and reports failure", func(t *testing.T) {
		client, store := memoryClient()
		key := client.Key("delete", "1")
		store.values[key] = []byte("cached")
		require.NoError(t, client.Delete(context.Background(), []string{key}))
		require.Empty(t, store.values)
		store.deleteErr = errors.New("delete unavailable")
		require.ErrorIs(t, client.Delete(context.Background(), []string{key}), store.deleteErr)
	})
}
