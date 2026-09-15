package cache

import (
	"sync/atomic"
	"time"
)

// Stats is a cumulative, process-local snapshot. Hits/misses count keys; errors
// count operations; LoadDuration includes the completed source load and encoding.
// Only fixed namespaces are recorded, never object keys or credential digests.
type Stats struct {
	Hit            uint64
	NegativeHit    uint64
	Miss           uint64
	DecodeError    uint64
	RedisReadError uint64
	WriteError     uint64
	LoaderError    uint64
	Load           uint64
	LoadDuration   time.Duration
}

type namespaceStats struct {
	hit, negativeHit, miss, decodeError           atomic.Uint64
	redisReadError, writeError, loaderError, load atomic.Uint64
	loadNanos                                     atomic.Int64
}

func (c *Client) namespaceStats(namespace string) *namespaceStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats[namespace]
	if stats == nil {
		stats = &namespaceStats{}
		c.stats[namespace] = stats
	}
	return stats
}

func (c *Client) Stats() map[string]Stats {
	result := make(map[string]Stats)
	if c == nil {
		return result
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for namespace, s := range c.stats {
		result[namespace] = Stats{
			Hit: s.hit.Load(), NegativeHit: s.negativeHit.Load(), Miss: s.miss.Load(),
			DecodeError: s.decodeError.Load(), RedisReadError: s.redisReadError.Load(),
			WriteError: s.writeError.Load(), LoaderError: s.loaderError.Load(),
			Load: s.load.Load(), LoadDuration: time.Duration(s.loadNanos.Load()),
		}
	}
	return result
}
