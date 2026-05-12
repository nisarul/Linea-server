// SPDX-License-Identifier: AGPL-3.0-or-later

package quotas_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nisarul/Linea-server/internal/platform"
	"github.com/nisarul/Linea-server/internal/quotas"
)

func newStore(t *testing.T) *platform.Store {
	t.Helper()
	s, err := platform.Open("")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEnforcer_PrivateGenealogyLimit(t *testing.T) {
	s := newStore(t)
	e := quotas.New(s, quotas.Limits{
		MaxPrivateGenealogiesPerUser: 2,
		MaxPersonsPerGenealogy:       1000,
	})
	require.NoError(t, e.CheckCreatePrivateGenealogy("alice"))
	require.NoError(t, e.IncPrivateGenealogy("alice"))
	require.NoError(t, e.IncPrivateGenealogy("alice"))

	err := e.CheckCreatePrivateGenealogy("alice")
	require.True(t, errors.Is(err, quotas.ErrQuotaExceeded))

	require.NoError(t, e.DecPrivateGenealogy("alice"))
	require.NoError(t, e.CheckCreatePrivateGenealogy("alice"))
}

func TestEnforcer_PersonLimit(t *testing.T) {
	s := newStore(t)
	e := quotas.New(s, quotas.Limits{
		MaxPrivateGenealogiesPerUser: 5,
		MaxPersonsPerGenealogy:       3,
	})
	for i := 0; i < 3; i++ {
		require.NoError(t, e.CheckCreatePerson("g1"))
		require.NoError(t, e.IncPerson("g1"))
	}
	err := e.CheckCreatePerson("g1")
	require.True(t, errors.Is(err, quotas.ErrQuotaExceeded))
}

func TestEnforcer_ZeroLimitMeansUnlimited(t *testing.T) {
	s := newStore(t)
	e := quotas.New(s, quotas.Limits{}) // both zero
	for i := 0; i < 100; i++ {
		require.NoError(t, e.CheckCreatePrivateGenealogy("alice"))
		require.NoError(t, e.IncPrivateGenealogy("alice"))
		require.NoError(t, e.CheckCreatePerson("g"))
		require.NoError(t, e.IncPerson("g"))
	}
}

func TestEnforcer_DefaultsMatchSpec(t *testing.T) {
	d := quotas.Default()
	require.Equal(t, uint64(5), d.MaxPrivateGenealogiesPerUser)
	require.Equal(t, uint64(1000), d.MaxPersonsPerGenealogy)
}
