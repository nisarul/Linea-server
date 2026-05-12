// SPDX-License-Identifier: AGPL-3.0-or-later

package platform_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCounters_PersonCountIncDec(t *testing.T) {
	s := newStore(t)

	got, err := s.GetPersonCount("g1")
	require.NoError(t, err)
	require.Equal(t, uint64(0), got)

	got, err = s.IncPersonCount("g1", 5)
	require.NoError(t, err)
	require.Equal(t, uint64(5), got)

	got, err = s.IncPersonCount("g1", -2)
	require.NoError(t, err)
	require.Equal(t, uint64(3), got)

	// Saturate at zero on big negative.
	got, err = s.IncPersonCount("g1", -100)
	require.NoError(t, err)
	require.Equal(t, uint64(0), got)
}

func TestCounters_UserPrivateGenIsolated(t *testing.T) {
	s := newStore(t)
	_, err := s.IncUserPrivateGenCount("alice", 3)
	require.NoError(t, err)
	_, err = s.IncUserPrivateGenCount("bob", 1)
	require.NoError(t, err)

	a, _ := s.GetUserPrivateGenCount("alice")
	b, _ := s.GetUserPrivateGenCount("bob")
	c, _ := s.GetUserPrivateGenCount("carol")
	require.Equal(t, uint64(3), a)
	require.Equal(t, uint64(1), b)
	require.Equal(t, uint64(0), c)
}

func TestCounters_SetPersonCount(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetPersonCount("g1", 42))
	v, err := s.GetPersonCount("g1")
	require.NoError(t, err)
	require.Equal(t, uint64(42), v)
}

func TestCounters_ConcurrentInc(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.IncPersonCount("g1", 1)
		}()
	}
	wg.Wait()
	v, err := s.GetPersonCount("g1")
	require.NoError(t, err)
	require.Equal(t, uint64(n), v)
}
