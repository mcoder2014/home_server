package cache

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestRedisCache only uses the explicit, isolated Redis fixture. It never
// guesses a server address, flushes a database, or accesses the production port.
func TestRedisCache(t *testing.T) {
	address := os.Getenv("HOME_SERVER_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("HOME_SERVER_TEST_REDIS_ADDR is not configured")
	}
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.NotEqual(t, "6379", port, "cache integration test requires an isolated Redis port")
	raw := redis.NewClient(&redis.Options{
		Addr: address, MaxRetries: -1, PoolSize: 2, ContextTimeoutEnabled: true,
		DialTimeout: redisTimeout, ReadTimeout: redisTimeout, WriteTimeout: redisTimeout, PoolTimeout: redisTimeout,
	})
	t.Cleanup(func() { _ = raw.Close() })
	ctx := context.Background()
	require.NoError(t, raw.Ping(ctx).Err())
	client := New(raw, fmt.Sprintf("cache-test:%d", time.Now().UnixNano()))
	keys := []string{client.Key("values", "1"), client.Key("values", "2")}
	t.Cleanup(func() { require.NoError(t, client.Delete(ctx, keys)) })
	var calls int
	loader := func(context.Context, []int) (map[int]string, error) {
		calls++
		return map[int]string{1: "present"}, nil
	}
	policy := Policy{Namespace: "values", TTL: time.Minute, NegativeTTL: time.Second}
	for i := 0; i < 2; i++ {
		values, loadErr := MGet(ctx, client, []int{1, 2}, strconv.Itoa, policy, loader)
		require.NoError(t, loadErr)
		require.Equal(t, map[int]string{1: "present"}, values)
	}
	require.Equal(t, 1, calls)
	ttl, err := raw.PTTL(ctx, keys[0]).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 50*time.Second)
	require.LessOrEqual(t, ttl, 66*time.Second)
	require.NoError(t, client.Delete(ctx, keys))
	_, err = MGet(ctx, client, []int{1, 2}, strconv.Itoa, policy, loader)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestRedisTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		<-release // Accept a connection but never answer Redis commands.
	}()
	t.Cleanup(func() { close(release); _ = listener.Close(); <-serverDone })
	raw := redis.NewClient(&redis.Options{
		Addr: listener.Addr().String(), MaxRetries: -1, ContextTimeoutEnabled: true,
		DialTimeout: redisTimeout, ReadTimeout: redisTimeout, WriteTimeout: redisTimeout, PoolTimeout: redisTimeout,
	})
	t.Cleanup(func() { _ = raw.Close() })
	start := time.Now()
	values, err := MGet(context.Background(), New(raw, "timeout"), []int{1}, strconv.Itoa,
		Policy{TTL: time.Minute}, func(context.Context, []int) (map[int]string, error) {
			return map[int]string{1: "live"}, nil
		})
	require.NoError(t, err)
	require.Equal(t, map[int]string{1: "live"}, values)
	require.Less(t, time.Since(start), 250*time.Millisecond, "silent Redis must not block source fallback")
}
