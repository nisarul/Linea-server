// SPDX-License-Identifier: AGPL-3.0-or-later

package ratelimit_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nisarul/Linea-server/internal/ratelimit"
)

func TestBucket_AllowsBurstThenRefill(t *testing.T) {
	b := ratelimit.New(ratelimit.Config{
		Capacity:        3,
		RefillPerSecond: 1000, // very fast refill so the test is quick
		IdleTimeout:     time.Hour,
	})
	defer b.Close()

	// Burst: 3 allowed, 4th denied immediately.
	require.True(t, b.Allow("k"))
	require.True(t, b.Allow("k"))
	require.True(t, b.Allow("k"))
	require.False(t, b.Allow("k"))

	// Wait long enough for one token to refill.
	time.Sleep(5 * time.Millisecond)
	require.True(t, b.Allow("k"))
}

func TestBucket_KeysAreIndependent(t *testing.T) {
	b := ratelimit.New(ratelimit.Config{Capacity: 1, RefillPerSecond: 0})
	defer b.Close()
	require.True(t, b.Allow("a"))
	require.False(t, b.Allow("a"))
	// "b" gets its own fresh bucket
	require.True(t, b.Allow("b"))
}

func TestBucket_ConcurrentAllow(t *testing.T) {
	b := ratelimit.New(ratelimit.Config{Capacity: 100, RefillPerSecond: 0})
	defer b.Close()
	var wg sync.WaitGroup
	const n = 200
	wg.Add(n)
	allowed := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if b.Allow("hot") {
				allowed <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(allowed)
	count := 0
	for range allowed {
		count++
	}
	require.Equal(t, 100, count, "exactly capacity tokens must be granted")
}

func TestAlwaysAllow(t *testing.T) {
	require.True(t, ratelimit.AlwaysAllow{}.Allow("anything"))
}
