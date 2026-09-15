package cache

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMGet(t *testing.T) {
	t.Run("empty input and nil client preserve batch results and source errors", func(t *testing.T) {
		failure := errors.New("source unavailable")
		var calls [][]int
		loader := func(ctx context.Context, keys []int) (map[int]string, error) {
			require.NoError(t, ctx.Err())
			calls = append(calls, append([]int(nil), keys...))
			return map[int]string{keys[0]: "value", 99: "unrequested"}, failure
		}
		empty, err := MGet(context.Background(), nil, []int{}, strconv.Itoa, Policy{}, loader)
		require.NoError(t, err)
		require.Equal(t, map[int]string{}, empty)
		values, err := MGet(context.Background(), nil, []int{1, 1, 2, 3}, strconv.Itoa,
			Policy{MaxBatch: 2}, loader)
		require.ErrorIs(t, err, failure)
		require.Equal(t, [][]int{{1, 2}, {3}}, calls)
		require.Equal(t, map[int]string{1: "value", 3: "value"}, values)
	})
	t.Run("canceled request never starts a loader", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		values, err := MGet(ctx, nil, []int{1}, strconv.Itoa, Policy{},
			func(context.Context, []int) (map[int]string, error) {
				t.Fatal("canceled request reached source")
				return nil, nil
			})
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, values)
	})
	t.Run("default batch ceiling limits bulk source calls", func(t *testing.T) {
		keys := make([]int, 205)
		for i := range keys {
			keys[i] = i
		}
		var sizes []int
		values, err := MGet(context.Background(), nil, keys, strconv.Itoa, Policy{MaxBatch: 1000},
			func(ctx context.Context, batch []int) (map[int]int, error) {
				sizes = append(sizes, len(batch))
				result := make(map[int]int)
				for _, key := range batch {
					result[key] = key
				}
				return result, nil
			})
		require.NoError(t, err)
		require.Equal(t, []int{100, 100, 5}, sizes)
		require.Len(t, values, 205)
	})
}

func TestClientKey(t *testing.T) {
	client := New(nil, "hs:test:")
	require.Equal(t, "hs:test:cache:v1:book:978123", client.Key("book", "978123"))
	require.NoError(t, client.Delete(context.Background(), nil))
	require.NoError(t, (*Client)(nil).Delete(context.Background(), []string{"unused"}))
}

// fixedExpiry demonstrates that only DTOs with a real hard expiry need the optional contract.
type fixedExpiry struct {
	Value string
	Until time.Time
}

func (v fixedExpiry) CacheExpiresAt() time.Time { return v.Until }
