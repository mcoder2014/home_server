package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type flight struct {
	done  chan struct{}
	value flightValue
}

type flightValue struct {
	data  []byte
	found bool
	err   error
}

type loadResult struct {
	values map[string]flightValue
	writes []cacheWrite
	err    error
}

// loadBatch joins existing keys before acquiring a physical loader slot. Only
// after a slot is available may new flights be registered and workers started;
// saturated callers wait in their existing request goroutine with a deadline.
func loadBatch[K comparable, V any](ctx context.Context, c *Client, keys []K,
	keyOf func(K) string, p Policy, loader Loader[K, V]) (map[K]V, error) {
	fullKeys := make(map[K]string, len(keys))
	calls := make(map[K]*flight, len(keys))
	c.mu.Lock()
	for _, key := range keys {
		fullKeys[key] = c.Key(p.Namespace, keyOf(key))
		if call := c.flights[fullKeys[key]]; call != nil {
			calls[key] = call
		}
	}
	c.mu.Unlock()
	if len(calls) == len(keys) {
		return awaitFlights[K, V](ctx, keys, calls)
	}
	queueCtx, cancel := context.WithTimeout(ctx, c.loadTimeout)
	defer cancel()
	select {
	case c.limit <- struct{}{}:
	case <-queueCtx.Done():
		return nil, queueCtx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-c.limit
		return nil, err
	}
	leaders := make([]K, 0, len(keys)-len(calls))
	owned := make(map[string]*flight)
	c.mu.Lock()
	for _, key := range keys {
		if calls[key] != nil {
			continue
		}
		call := c.flights[fullKeys[key]]
		if call == nil {
			call = &flight{done: make(chan struct{})}
			c.flights[fullKeys[key]], owned[fullKeys[key]] = call, call
			leaders = append(leaders, key)
		}
		calls[key] = call
	}
	c.mu.Unlock()
	if len(leaders) == 0 {
		<-c.limit
	} else {
		go runLoader(c, leaders, fullKeys, owned, p, loader)
	}
	return awaitFlights[K, V](ctx, keys, calls)
}

func awaitFlights[K comparable, V any](ctx context.Context, keys []K, calls map[K]*flight) (map[K]V, error) {
	values := make(map[K]V, len(keys))
	var resultErr error
	for _, key := range keys {
		call := calls[key]
		select {
		case <-ctx.Done():
			return values, errors.Join(resultErr, ctx.Err())
		case <-call.done:
		}
		if resultErr == nil {
			resultErr = call.value.err
		}
		if call.value.found {
			var value V
			if err := json.Unmarshal(call.value.data, &value); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("cache DTO decode: %w", err))
				continue
			}
			values[key] = value
		}
	}
	return values, resultErr
}

// runLoader gives shared work its own deadline. The supervisor removes flights
// on timeout even when a bad loader ignores cancellation. Its physical slot is
// held until that loader really exits, limiting stuck workers to 16; at most one
// supervisor and one worker exist per slot. Request cancellation only stops its
// waiter. No caller-owned context values are used by the detached loader.
func runLoader[K comparable, V any](c *Client, keys []K, fullKeys map[K]string,
	owned map[string]*flight, p Policy, loader Loader[K, V]) {
	ctx, cancel := context.WithTimeout(context.Background(), c.loadTimeout)
	defer cancel()
	finished := make(chan struct{})
	defer close(finished)
	loaded := make(chan loadResult, 1)
	go func() {
		defer func() { <-finished; <-c.limit }()
		loaded <- callLoader(ctx, c.namespaceStats(p.Namespace), keys, fullKeys, p, loader)
	}()
	var result loadResult
	var err error
	select {
	case result = <-loaded:
		err = ctx.Err()
		if err == nil && c.store != nil && len(result.writes) > 0 {
			writeCtx, writeCancel := context.WithTimeout(ctx, redisTimeout)
			if writeErr := c.store.write(writeCtx, result.writes); writeErr != nil {
				c.namespaceStats(p.Namespace).writeError.Add(1)
			}
			writeCancel()
		}
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil || result.err != nil {
		c.namespaceStats(p.Namespace).loaderError.Add(1)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, call := range owned {
		if err != nil {
			call.value = flightValue{err: err}
		} else {
			call.value = result.values[key]
		}
		delete(c.flights, key)
		close(call.done)
	}
}

func callLoader[K comparable, V any](ctx context.Context, stats *namespaceStats, keys []K, fullKeys map[K]string,
	p Policy, loader Loader[K, V]) (result loadResult) {
	started := time.Now()
	stats.load.Add(1)
	result.values = make(map[string]flightValue, len(keys))
	defer func() {
		stats.loadNanos.Add(int64(time.Since(started)))
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("cache loader panicked: %v", recovered)
			result.err = err
			result.writes = nil
			for _, key := range keys {
				result.values[fullKeys[key]] = flightValue{err: err}
			}
		}
	}()
	values, sourceErr := loader(ctx, keys)
	result.err = sourceErr
	for _, key := range keys {
		value, found := values[key]
		entry := flightValue{found: found, err: sourceErr}
		var deadline time.Time
		canCache := found || sourceErr == nil
		if found {
			data, err := json.Marshal(value)
			if err != nil {
				entry.found = false
				entry.err = errors.Join(sourceErr, fmt.Errorf("cache DTO encode: %w", err))
				result.err = entry.err
				canCache = false
			}
			entry.data = data
			if expiring, ok := any(value).(Expiring); ok {
				deadline = expiring.CacheExpiresAt()
				canCache = canCache && !deadline.IsZero()
			}
		}
		result.values[fullKeys[key]] = entry
		if canCache {
			if write, ok := cacheEntry(fullKeys[key], entry.data, found, deadline, p); ok {
				result.writes = append(result.writes, write)
			}
		}
	}
	return result
}
