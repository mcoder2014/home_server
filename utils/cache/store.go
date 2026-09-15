package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type cacheWrite struct {
	key  string
	data []byte
	ttl  time.Duration
}

type cacheStore interface {
	mget(context.Context, []string) ([]interface{}, error)
	write(context.Context, []cacheWrite) error
	delete(context.Context, []string) error
}

type redisStore struct {
	client redis.UniversalClient
}

func (s redisStore) mget(ctx context.Context, keys []string) ([]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.client.MGet(ctx, keys...).Result()
}

func (s redisStore) write(ctx context.Context, writes []cacheWrite) error {
	if len(writes) == 0 {
		return nil
	}
	_, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, write := range writes {
			pipe.Set(ctx, write.key, write.data, write.ttl)
		}
		return nil
	})
	return err
}

func (s redisStore) delete(ctx context.Context, keys []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.client.Del(ctx, keys...).Err()
}

// Delete expects full keys from Client.Key. Cache invalidation errors are
// returned so callers can observe them after their source transaction commits.
func (c *Client) Delete(ctx context.Context, keys []string) error {
	if c == nil || c.store == nil || len(keys) == 0 {
		return nil
	}
	deleteCtx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()
	for start := 0; start < len(keys); start += maxBatch {
		end := start + maxBatch
		if end > len(keys) {
			end = len(keys)
		}
		if err := c.store.delete(deleteCtx, keys[start:end]); err != nil {
			return err
		}
	}
	return nil
}
