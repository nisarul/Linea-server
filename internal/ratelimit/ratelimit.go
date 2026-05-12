// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ratelimit provides a small, in-process token-bucket
// rate limiter keyed by an opaque string (typically client IP or
// JWT subject). It deliberately depends on no external systems;
// across multiple server replicas the limit is per-replica.
//
// The v0.3 plan replaces this with a Redis-backed limiter so
// limits become global; the public Limiter interface stays the
// same so call sites do not change.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter decides whether a given key may make a request now.
type Limiter interface {
	Allow(key string) bool
}

// Bucket is a token-bucket Limiter. A bucket grants Capacity
// tokens up-front and refills at RefillPerSecond tokens/sec.
//
// Each unique key gets its own bucket lazily. Idle buckets are
// garbage-collected by an internal sweeper.
type Bucket struct {
	capacity        float64
	refillPerSecond float64
	idleTimeout     time.Duration

	mu      sync.Mutex
	buckets map[string]*entry
	stop    chan struct{}
}

type entry struct {
	tokens  float64
	updated time.Time
}

// Config configures a Bucket.
type Config struct {
	// Capacity is the max tokens (the burst size).
	Capacity float64
	// RefillPerSecond is the steady-state refill rate.
	RefillPerSecond float64
	// IdleTimeout is how long a per-key bucket is kept after the
	// last reference. Zero defaults to 10 minutes.
	IdleTimeout time.Duration
}

// New constructs a Bucket and starts its idle-sweep goroutine.
// The caller MUST call Close to stop the sweeper.
func New(cfg Config) *Bucket {
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 10 * time.Minute
	}
	b := &Bucket{
		capacity:        cfg.Capacity,
		refillPerSecond: cfg.RefillPerSecond,
		idleTimeout:     cfg.IdleTimeout,
		buckets:         make(map[string]*entry),
		stop:            make(chan struct{}),
	}
	go b.sweep()
	return b
}

// Allow consumes one token for key. Returns true if a token was
// available; false otherwise.
func (b *Bucket) Allow(key string) bool {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.buckets[key]
	if !ok {
		e = &entry{tokens: b.capacity, updated: now}
		b.buckets[key] = e
	} else {
		// Refill.
		dt := now.Sub(e.updated).Seconds()
		e.tokens += dt * b.refillPerSecond
		if e.tokens > b.capacity {
			e.tokens = b.capacity
		}
		e.updated = now
	}
	if e.tokens >= 1 {
		e.tokens--
		return true
	}
	return false
}

// Close stops the background sweeper.
func (b *Bucket) Close() {
	close(b.stop)
}

func (b *Bucket) sweep() {
	t := time.NewTicker(b.idleTimeout)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case now := <-t.C:
			b.mu.Lock()
			for k, e := range b.buckets {
				if now.Sub(e.updated) > b.idleTimeout {
					delete(b.buckets, k)
				}
			}
			b.mu.Unlock()
		}
	}
}

// AlwaysAllow is a sentinel Limiter used when rate limiting is
// disabled (e.g. in tests).
type AlwaysAllow struct{}

// Allow on AlwaysAllow always returns true.
func (AlwaysAllow) Allow(string) bool { return true }
