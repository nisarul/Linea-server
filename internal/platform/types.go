// SPDX-License-Identifier: AGPL-3.0-or-later

package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Visibility per the v0.2 spec. The zero value is VisibilityPrivate
// (the safe default); persisted records always set it explicitly.
type Visibility uint8

const (
	VisibilityPrivate  Visibility = 1
	VisibilityUnlisted Visibility = 2
	VisibilityPublic   Visibility = 3
)

// IsValid reports whether v is one of the three legal values.
func (v Visibility) IsValid() bool {
	return v == VisibilityPrivate || v == VisibilityUnlisted || v == VisibilityPublic
}

// String returns the canonical name.
func (v Visibility) String() string {
	switch v {
	case VisibilityPrivate:
		return "Private"
	case VisibilityUnlisted:
		return "Unlisted"
	case VisibilityPublic:
		return "Public"
	}
	return "Unknown"
}

// ParseVisibility accepts the canonical names case-insensitively.
func ParseVisibility(s string) (Visibility, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "private":
		return VisibilityPrivate, nil
	case "unlisted":
		return VisibilityUnlisted, nil
	case "public":
		return VisibilityPublic, nil
	}
	return 0, fmt.Errorf("platform: unknown visibility %q", s)
}

// Role per the v0.2 spec. Higher numeric value = more privileged.
type Role uint8

const (
	// RoleNone is the absence of a role; not stored.
	RoleNone        Role = 0
	RoleViewer      Role = 1
	RoleContributor Role = 2
	RoleCurator     Role = 3
	RoleOwner       Role = 4
)

// IsValid reports whether r is a known role.
func (r Role) IsValid() bool { return r >= RoleViewer && r <= RoleOwner }

// String returns the canonical name.
func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "Viewer"
	case RoleContributor:
		return "Contributor"
	case RoleCurator:
		return "Curator"
	case RoleOwner:
		return "Owner"
	}
	return "None"
}

// User is a registered identity. Records are auto-created on
// first successful OIDC login; no separate signup flow exists.
type User struct {
	Subject   string `json:"sub"`
	Email     string `json:"email,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Suspended bool   `json:"suspended,omitempty"`
}

// Genealogy is the unit of multi-tenancy and the user-facing
// concept. Each Genealogy owns its own per-tenant Linea-core
// Badger directory at <data-dir>/genealogies/<ID>.
type Genealogy struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Visibility Visibility `json:"visibility"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  int64      `json:"created_at"`
}

// Membership records an explicit (user, genealogy, role) tuple.
// Implicit roles derived from a Genealogy's Visibility are NOT
// stored; they are computed by the auth flow at request time.
type Membership struct {
	Subject     string `json:"sub"`
	GenealogyID string `json:"genealogy_id"`
	Role        Role   `json:"role"`
	GrantedBy   string `json:"granted_by,omitempty"`
	GrantedAt   int64  `json:"granted_at"`
}

// Ban records that a user is forbidden from submitting proposals
// to a specific Genealogy. Bans do not block reads (Public reads
// stay public), only proposal submission.
type Ban struct {
	Subject     string `json:"sub"`
	GenealogyID string `json:"genealogy_id"`
	Reason      string `json:"reason,omitempty"`
	By          string `json:"by"`
	At          int64  `json:"at"`
}

// Now returns the current Unix-second timestamp; package-private
// indirection so tests can pin time.
var Now = func() int64 { return time.Now().Unix() }

// ErrNotFound is returned by Get* methods when the requested key
// is absent. Storage-layer errors are returned verbatim.
var ErrNotFound = errors.New("platform: not found")
