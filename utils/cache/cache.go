// Package cache provides bounded, disposable JSON caches. Loaders remain the source of truth.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	maxBatch       = 100
	maxValueBytes  = 64 << 10
	redisTimeout   = 20 * time.Millisecond
	loaderTimeout  = 3 * time.Second
	loaderParallel = 16
)

// Loader must return only requested keys and respect ctx. A nil error means all
// requested keys were checked, which permits negative caching when enabled.
type Loader[K comparable, V any] func(context.Context, []K) (map[K]V, error)

type Policy struct {
	Namespace     string
	TTL           time.Duration
	NegativeTTL   time.Duration
	MaxBatch      int
	MaxValueBytes int
}

// Expiring is optional for DTOs with an absolute deadline, such as credentials.
// A zero or past deadline skips caching; it does not change the loader result.
type Expiring interface {
	CacheExpiresAt() time.Time
}

type Client struct {
	store       cacheStore
	prefix      string
	mu          sync.Mutex
	flights     map[string]*flight
	stats       map[string]*namespaceStats
	limit       chan struct{}
	loadTimeout time.Duration
}

// New does not own or close the Redis connection. Enable context timeouts on the
// supplied client; this package gives each Redis MGET/SET/DEL its own 20ms
// deadline, so lower transport timeouts would cut that budget short. Cross-slot
// Redis Cluster MGET is unsupported. A nil Redis client retains local flight
// merging; passing a nil *Client to MGet disables the cache completely.
func New(client redis.UniversalClient, prefix string) *Client {
	c := &Client{
		prefix:  strings.TrimSuffix(prefix, ":") + ":cache:v1:",
		flights: make(map[string]*flight), limit: make(chan struct{}, loaderParallel),
		stats:       make(map[string]*namespaceStats),
		loadTimeout: loaderTimeout,
	}
	if client != nil {
		c.store = redisStore{client: client}
	}
	return c
}

// Key includes the instance prefix and schema. Namespace/id must be stable and
// collision-free; composite ids must include every ownership/version dimension.
func (c *Client) Key(namespace, id string) string {
	prefix := "cache:v1:"
	if c != nil {
		prefix = c.prefix
	}
	return prefix + namespace + ":" + id
}

// MGet deduplicates and batches keys, keeps partial source successes with their
// error, and returns only present requested keys. DTOs must round-trip through
// JSON; each waiter decodes its own value. A nil client calls the loader directly.
func MGet[K comparable, V any](ctx context.Context, c *Client, keys []K, keyOf func(K) string,
	p Policy, loader Loader[K, V]) (map[K]V, error) {
	result := make(map[K]V)
	if len(keys) == 0 {
		return result, nil
	}
	if p.MaxBatch <= 0 || p.MaxBatch > maxBatch {
		p.MaxBatch = maxBatch
	}
	if p.MaxValueBytes <= 0 || p.MaxValueBytes > maxValueBytes {
		p.MaxValueBytes = maxValueBytes
	}
	unique := make([]K, 0, len(keys))
	seen := make(map[K]bool, len(keys))
	for _, key := range keys {
		if !seen[key] {
			unique, seen[key] = append(unique, key), true
		}
	}
	var resultErr error
	for start := 0; start < len(unique); start += p.MaxBatch {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(resultErr, err)
		}
		end := start + p.MaxBatch
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		missing := readBatch(ctx, c, batch, keyOf, p, result)
		if len(missing) == 0 {
			continue
		}
		var values map[K]V
		var err error
		if c == nil {
			values, err = loader(ctx, missing)
		} else {
			values, err = loadBatch(ctx, c, missing, keyOf, p, loader)
		}
		for _, key := range missing {
			if value, found := values[key]; found {
				result[key] = value
			}
		}
		resultErr = errors.Join(resultErr, err)
	}
	return result, resultErr
}

type envelope struct {
	Schema    int             `json:"schema"`
	Found     *bool           `json:"found"`
	Value     json.RawMessage `json:"value,omitempty"`
	ExpiresAt int64           `json:"expires_at"`
}

func readBatch[K comparable, V any](ctx context.Context, c *Client, keys []K,
	keyOf func(K) string, p Policy, result map[K]V) (missing []K) {
	if c == nil {
		return keys
	}
	stats := c.namespaceStats(p.Namespace)
	defer func() { stats.miss.Add(uint64(len(missing))) }()
	if c.store == nil {
		return keys
	}
	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = c.Key(p.Namespace, keyOf(key))
	}
	readCtx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()
	values, err := c.store.mget(readCtx, fullKeys)
	if err != nil || len(values) != len(keys) {
		stats.redisReadError.Add(1)
		return keys
	}
	missing = make([]K, 0, len(keys))
	for i, key := range keys {
		if values[i] == nil {
			missing = append(missing, key)
			continue
		}
		raw, ok := values[i].(string)
		var entry envelope
		if !ok || len(raw) > p.MaxValueBytes || json.Unmarshal([]byte(raw), &entry) != nil ||
			entry.Schema != 1 || entry.Found == nil || entry.ExpiresAt <= 0 {
			stats.decodeError.Add(1)
			missing = append(missing, key)
			continue
		}
		if entry.ExpiresAt <= time.Now().UnixNano() {
			missing = append(missing, key)
			continue
		}
		if !*entry.Found {
			stats.negativeHit.Add(1)
			continue
		}
		var value V
		if len(entry.Value) == 0 || json.Unmarshal(entry.Value, &value) != nil {
			stats.decodeError.Add(1)
			missing = append(missing, key)
			continue
		}
		result[key] = value
		stats.hit.Add(1)
	}
	return missing
}

// cacheEntry computes jitter before the optional hard deadline. A zero TTL must
// never reach Redis SET, where it would create an immortal cache entry.
func cacheEntry(key string, value []byte, found bool, deadline time.Time, p Policy) (cacheWrite, bool) {
	ttl := p.TTL
	if !found {
		ttl = p.NegativeTTL
	}
	if ttl <= 0 {
		return cacheWrite{}, false
	}
	ttl = time.Duration(float64(ttl) * (0.9 + rand.Float64()*0.2))
	now := time.Now()
	expires := now.Add(ttl)
	if !deadline.IsZero() && deadline.Before(expires) {
		expires = deadline
	}
	ttl = expires.Sub(now)
	if ttl < time.Millisecond {
		return cacheWrite{}, false
	}
	data, err := json.Marshal(envelope{Schema: 1, Found: &found, Value: value, ExpiresAt: expires.UnixNano()})
	if err != nil || len(data) > p.MaxValueBytes {
		return cacheWrite{}, false
	}
	return cacheWrite{key: key, data: data, ttl: ttl}, true
}
