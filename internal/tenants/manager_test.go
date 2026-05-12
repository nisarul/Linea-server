// SPDX-License-Identifier: AGPL-3.0-or-later

package tenants_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nisarul/Linea-core/store"

	"github.com/nisarul/Linea-server/internal/tenants"
)

func newManager(t *testing.T, maxOpen int) *tenants.Manager {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tenants")
	m, err := tenants.New(tenants.Config{Root: root, MaxOpen: maxOpen})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestManager_GetOpensAndCaches(t *testing.T) {
	m := newManager(t, 4)
	ctx := context.Background()

	a, err := m.Get(ctx, "g1")
	require.NoError(t, err)
	require.NotNil(t, a)
	require.Equal(t, 1, m.Len())

	a2, err := m.Get(ctx, "g1")
	require.NoError(t, err)
	require.Same(t, a, a2, "second Get for same id must return cached handle")
	require.Equal(t, 1, m.Len())
}

func TestManager_LRUEvictionClosesOldest(t *testing.T) {
	m := newManager(t, 2)
	ctx := context.Background()
	for _, id := range []string{"g1", "g2", "g3"} {
		_, err := m.Get(ctx, id)
		require.NoError(t, err)
	}
	require.Equal(t, 2, m.Len())

	// g1 should have been evicted; reopening it returns a fresh handle.
	st, err := m.Get(ctx, "g1")
	require.NoError(t, err)
	v, err := st.CurrentVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, store.Version(0), v)
}

func TestManager_DropClosesAndRemoves(t *testing.T) {
	m := newManager(t, 4)
	ctx := context.Background()
	_, err := m.Get(ctx, "g1")
	require.NoError(t, err)
	require.Equal(t, 1, m.Len())
	require.NoError(t, m.Drop("g1"))
	require.Equal(t, 0, m.Len())
}

func TestManager_DropMissingIsNoOp(t *testing.T) {
	m := newManager(t, 4)
	require.NoError(t, m.Drop("never-opened"))
}

func TestManager_GetAfterCloseReturnsShutdown(t *testing.T) {
	m := newManager(t, 4)
	require.NoError(t, m.Close())
	_, err := m.Get(context.Background(), "g1")
	require.True(t, errors.Is(err, tenants.ErrShutdown))
}

// Concurrent Get for the same id must coalesce: only one open is
// performed; both callers receive the same handle.
func TestManager_GetCoalescesConcurrent(t *testing.T) {
	m := newManager(t, 4)
	ctx := context.Background()
	const n = 16
	var wg sync.WaitGroup
	results := make([]store.Store, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			st, err := m.Get(ctx, "g1")
			require.NoError(t, err)
			results[i] = st
		}(i)
	}
	wg.Wait()
	first := results[0]
	for _, r := range results[1:] {
		require.Same(t, first, r, "all concurrent Gets must return the same handle")
	}
	require.Equal(t, 1, m.Len())
}
