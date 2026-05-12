// SPDX-License-Identifier: AGPL-3.0-or-later

// Package quotas centralises the v0.2 free-tier limits and the
// quota-check helpers callers use before doing the work that
// would consume a counter slot.
//
// All limits are configurable via the server's Config; the
// defaults below match the spec in /memories/session/plan.md.
package quotas

import (
	"errors"

	"github.com/nisarul/Linea-server/internal/platform"
)

// ErrQuotaExceeded is returned by the Check* methods when a
// limit has been reached. Caller maps it to a user-visible error.
var ErrQuotaExceeded = errors.New("quotas: limit exceeded")

// Limits configures the per-user / per-genealogy hard limits.
type Limits struct {
	// MaxPrivateGenealogiesPerUser caps how many Private
	// genealogies a single user may own concurrently. Public and
	// Unlisted genealogies do NOT count against this quota in
	// v0.2 (operators can revisit later).
	MaxPrivateGenealogiesPerUser uint64

	// MaxPersonsPerGenealogy caps the number of Person entities
	// accepted into a genealogy. Once reached, further
	// Create-Person proposals will be rejected at the Accept
	// step.
	MaxPersonsPerGenealogy uint64
}

// Default returns the v0.2 spec defaults.
func Default() Limits {
	return Limits{
		MaxPrivateGenealogiesPerUser: 5,
		MaxPersonsPerGenealogy:       1000,
	}
}

// Enforcer checks limits against the platform counters.
type Enforcer struct {
	platform *platform.Store
	limits   Limits
}

// New constructs an Enforcer with the supplied limits.
func New(p *platform.Store, limits Limits) *Enforcer {
	return &Enforcer{platform: p, limits: limits}
}

// CheckCreatePrivateGenealogy verifies the user can create one
// more private genealogy. Returns ErrQuotaExceeded if at limit.
func (e *Enforcer) CheckCreatePrivateGenealogy(sub string) error {
	if e.limits.MaxPrivateGenealogiesPerUser == 0 {
		return nil // 0 means "no limit"
	}
	cur, err := e.platform.GetUserPrivateGenCount(sub)
	if err != nil {
		return err
	}
	if cur >= e.limits.MaxPrivateGenealogiesPerUser {
		return ErrQuotaExceeded
	}
	return nil
}

// CheckCreatePerson verifies the genealogy has room for one more
// person. Returns ErrQuotaExceeded if at limit.
func (e *Enforcer) CheckCreatePerson(gid string) error {
	if e.limits.MaxPersonsPerGenealogy == 0 {
		return nil
	}
	cur, err := e.platform.GetPersonCount(gid)
	if err != nil {
		return err
	}
	if cur >= e.limits.MaxPersonsPerGenealogy {
		return ErrQuotaExceeded
	}
	return nil
}

// IncPrivateGenealogy atomically increments the user's count.
// Use after a successful CreateGenealogy.
func (e *Enforcer) IncPrivateGenealogy(sub string) error {
	_, err := e.platform.IncUserPrivateGenCount(sub, 1)
	return err
}

// DecPrivateGenealogy atomically decrements the user's count.
// Use after a successful DeleteGenealogy or visibility change
// from Private -> Public/Unlisted.
func (e *Enforcer) DecPrivateGenealogy(sub string) error {
	_, err := e.platform.IncUserPrivateGenCount(sub, -1)
	return err
}

// IncPerson atomically increments the genealogy's person count.
// Use after a successful Create-Person proposal Accept.
func (e *Enforcer) IncPerson(gid string) error {
	_, err := e.platform.IncPersonCount(gid, 1)
	return err
}
