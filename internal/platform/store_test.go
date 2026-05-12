// SPDX-License-Identifier: AGPL-3.0-or-later

package platform_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nisarul/Linea-server/internal/platform"
)

func newStore(t *testing.T) *platform.Store {
	t.Helper()
	s, err := platform.Open("")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPlatform_UserRoundTrip(t *testing.T) {
	s := newStore(t)
	u, err := s.EnsureUser("alice", "alice@example.com")
	require.NoError(t, err)
	require.Equal(t, "alice", u.Subject)
	require.Equal(t, "alice@example.com", u.Email)

	// Idempotent.
	u2, err := s.EnsureUser("alice", "different-email-ignored@example.com")
	require.NoError(t, err)
	require.Equal(t, u.Email, u2.Email, "EnsureUser must not overwrite an existing email")

	got, err := s.GetUser("alice")
	require.NoError(t, err)
	require.Equal(t, u, got)

	_, err = s.GetUser("ghost")
	require.True(t, errors.Is(err, platform.ErrNotFound))
}

func TestPlatform_GenealogyRoundTrip(t *testing.T) {
	s := newStore(t)
	require.Error(t, s.PutGenealogy(platform.Genealogy{ID: "g1", Visibility: 0}))

	g := platform.Genealogy{
		ID: "g1", Name: "Test", Visibility: platform.VisibilityPrivate,
		CreatedBy: "alice", CreatedAt: 1,
	}
	require.NoError(t, s.PutGenealogy(g))

	got, err := s.GetGenealogy("g1")
	require.NoError(t, err)
	require.Equal(t, g, got)

	require.NoError(t, s.PutGenealogy(platform.Genealogy{
		ID: "g2", Name: "Other", Visibility: platform.VisibilityPublic,
		CreatedBy: "bob", CreatedAt: 2,
	}))

	var ids []string
	require.NoError(t, s.IterateGenealogies(func(g platform.Genealogy) bool {
		ids = append(ids, g.ID)
		return true
	}))
	require.ElementsMatch(t, []string{"g1", "g2"}, ids)

	require.NoError(t, s.DeleteGenealogy("g1"))
	_, err = s.GetGenealogy("g1")
	require.True(t, errors.Is(err, platform.ErrNotFound))
}

func TestPlatform_MembershipForwardAndReverse(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.PutMembership(platform.Membership{
		Subject: "alice", GenealogyID: "g1", Role: platform.RoleOwner,
	}))
	require.NoError(t, s.PutMembership(platform.Membership{
		Subject: "alice", GenealogyID: "g2", Role: platform.RoleViewer,
	}))
	require.NoError(t, s.PutMembership(platform.Membership{
		Subject: "bob", GenealogyID: "g1", Role: platform.RoleCurator,
	}))

	// per-genealogy
	var g1Subs []string
	require.NoError(t, s.IterateMembershipsForGenealogy("g1", func(m platform.Membership) bool {
		g1Subs = append(g1Subs, m.Subject)
		return true
	}))
	require.ElementsMatch(t, []string{"alice", "bob"}, g1Subs)

	// per-user (reverse index)
	var aliceGens []string
	require.NoError(t, s.IterateMembershipsForUser("alice", func(m platform.Membership) bool {
		aliceGens = append(aliceGens, m.GenealogyID)
		return true
	}))
	require.ElementsMatch(t, []string{"g1", "g2"}, aliceGens)

	require.NoError(t, s.DeleteMembership("g1", "alice"))
	g1Subs = g1Subs[:0]
	require.NoError(t, s.IterateMembershipsForGenealogy("g1", func(m platform.Membership) bool {
		g1Subs = append(g1Subs, m.Subject)
		return true
	}))
	require.ElementsMatch(t, []string{"bob"}, g1Subs)

	aliceGens = aliceGens[:0]
	require.NoError(t, s.IterateMembershipsForUser("alice", func(m platform.Membership) bool {
		aliceGens = append(aliceGens, m.GenealogyID)
		return true
	}))
	require.ElementsMatch(t, []string{"g2"}, aliceGens)
}

func TestPlatform_BanRoundTrip(t *testing.T) {
	s := newStore(t)
	banned, err := s.IsBanned("g1", "spammer")
	require.NoError(t, err)
	require.False(t, banned)

	require.NoError(t, s.PutBan(platform.Ban{
		Subject: "spammer", GenealogyID: "g1",
		Reason: "spam", By: "alice",
	}))
	banned, err = s.IsBanned("g1", "spammer")
	require.NoError(t, err)
	require.True(t, banned)

	require.NoError(t, s.DeleteBan("g1", "spammer"))
	banned, err = s.IsBanned("g1", "spammer")
	require.NoError(t, err)
	require.False(t, banned)
}

func TestPlatform_VisibilityParseAndString(t *testing.T) {
	for _, v := range []platform.Visibility{
		platform.VisibilityPrivate, platform.VisibilityUnlisted, platform.VisibilityPublic,
	} {
		got, err := platform.ParseVisibility(v.String())
		require.NoError(t, err)
		require.Equal(t, v, got)
	}
	_, err := platform.ParseVisibility("nope")
	require.Error(t, err)
}

func TestPlatform_ConcurrentEnsureUser(t *testing.T) {
	s := newStore(t)
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.EnsureUser("race", "race@example.com")
		}()
	}
	wg.Wait()
	u, err := s.GetUser("race")
	require.NoError(t, err)
	require.Equal(t, "race", u.Subject)
}
