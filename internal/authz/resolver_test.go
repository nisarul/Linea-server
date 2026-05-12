// SPDX-License-Identifier: AGPL-3.0-or-later

package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nisarul/Linea-server/internal/authz"
	"github.com/nisarul/Linea-server/internal/platform"
)

func newPlatform(t *testing.T) *platform.Store {
	t.Helper()
	s, err := platform.Open("")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestResolve_PrivateRequiresMembership(t *testing.T) {
	p := newPlatform(t)
	require.NoError(t, p.PutGenealogy(platform.Genealogy{
		ID: "g1", Visibility: platform.VisibilityPrivate, CreatedBy: "owner",
	}))
	r := authz.NewResolver(p, 0)

	// Anonymous: 403.
	_, err := r.Resolve(context.Background(), "", "g1")
	require.True(t, errors.Is(err, authz.ErrNotAllowed))

	// Authenticated stranger: still 403.
	_, err = r.Resolve(context.Background(), "stranger", "g1")
	require.True(t, errors.Is(err, authz.ErrNotAllowed))

	// Member: gets explicit role.
	require.NoError(t, p.PutMembership(platform.Membership{
		Subject: "alice", GenealogyID: "g1", Role: platform.RoleViewer,
	}))
	r.Invalidate("alice", "g1")
	d, err := r.Resolve(context.Background(), "alice", "g1")
	require.NoError(t, err)
	require.Equal(t, platform.RoleViewer, d.Role)
	require.False(t, d.Implicit)
}

func TestResolve_PublicImplicitRoles(t *testing.T) {
	p := newPlatform(t)
	require.NoError(t, p.PutGenealogy(platform.Genealogy{
		ID: "g2", Visibility: platform.VisibilityPublic,
	}))
	r := authz.NewResolver(p, 0)

	d, err := r.Resolve(context.Background(), "", "g2")
	require.NoError(t, err)
	require.Equal(t, platform.RoleViewer, d.Role)
	require.True(t, d.Implicit)

	d, err = r.Resolve(context.Background(), "alice", "g2")
	require.NoError(t, err)
	require.Equal(t, platform.RoleContributor, d.Role)
	require.True(t, d.Implicit)

	// Explicit membership beats implicit.
	require.NoError(t, p.PutMembership(platform.Membership{
		Subject: "alice", GenealogyID: "g2", Role: platform.RoleCurator,
	}))
	r.Invalidate("alice", "g2")
	d, err = r.Resolve(context.Background(), "alice", "g2")
	require.NoError(t, err)
	require.Equal(t, platform.RoleCurator, d.Role)
	require.False(t, d.Implicit)
}

func TestResolve_UnlistedRequiresLogin(t *testing.T) {
	p := newPlatform(t)
	require.NoError(t, p.PutGenealogy(platform.Genealogy{
		ID: "g3", Visibility: platform.VisibilityUnlisted,
	}))
	r := authz.NewResolver(p, 0)

	_, err := r.Resolve(context.Background(), "", "g3")
	require.True(t, errors.Is(err, authz.ErrNotAllowed))

	d, err := r.Resolve(context.Background(), "alice", "g3")
	require.NoError(t, err)
	require.Equal(t, platform.RoleContributor, d.Role)
	require.True(t, d.Implicit)
}

func TestResolve_NotFoundIsIndistinguishable(t *testing.T) {
	p := newPlatform(t)
	r := authz.NewResolver(p, 0)
	_, err := r.Resolve(context.Background(), "alice", "nope")
	require.True(t, errors.Is(err, authz.ErrNotAllowed),
		"missing genealogy must return ErrNotAllowed (existence is privileged info)")
}
