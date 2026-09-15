package cache

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientStats(t *testing.T) {
	client, store := memoryClient()
	ctx := context.Background()
	policy := Policy{Namespace: "books", TTL: time.Minute, NegativeTTL: time.Minute}
	loader := func(context.Context, []int) (map[int]string, error) {
		return map[int]string{1: "present"}, nil
	}
	for i := 0; i < 2; i++ {
		_, err := MGet(ctx, client, []int{1, 2}, strconv.Itoa, policy, loader)
		require.NoError(t, err)
	}
	first := client.Stats()["books"]
	require.EqualValues(t, 1, first.Hit)
	require.EqualValues(t, 1, first.NegativeHit)
	require.EqualValues(t, 2, first.Miss)
	require.EqualValues(t, 1, first.Load)
	require.Positive(t, first.LoadDuration)
	store.values[client.Key("books", "1")] = []byte("corrupt")
	_, err := MGet(ctx, client, []int{1}, strconv.Itoa, policy, loader)
	require.NoError(t, err)
	store.readErr, store.writeErr = errors.New("redis read"), errors.New("redis write")
	failure := errors.New("source partial failure")
	_, err = MGet(ctx, client, []int{1}, strconv.Itoa, policy,
		func(context.Context, []int) (map[int]string, error) { return map[int]string{1: "known"}, failure })
	require.ErrorIs(t, err, failure)
	stats := client.Stats()
	require.Len(t, stats, 1, "statistics must not contain individual object keys")
	require.EqualValues(t, 4, stats["books"].Miss)
	require.EqualValues(t, 1, stats["books"].DecodeError)
	require.EqualValues(t, 1, stats["books"].RedisReadError)
	require.EqualValues(t, 1, stats["books"].WriteError)
	require.EqualValues(t, 1, stats["books"].LoaderError)
	require.EqualValues(t, 3, stats["books"].Load)
	delete(stats, "books")
	require.Len(t, client.Stats(), 1, "snapshot mutation must not alter live counters")
}
