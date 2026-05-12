// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth handles OIDC bearer-token verification, role
// extraction from JWT claims, and request-context plumbing.
//
// Role mapping per CCGGS §8.1:
//
//	Role             Allowed RPC families
//	Viewer           Get*, List*, Queries.*, Server.*
//	Contributor      Viewer + Proposals.CreateProposal, Submit, Withdraw (own)
//	Curator          Contributor + Proposals.Claim, Accept, Reject (any)
//
// Claim source is configurable. By default the verifier reads
// roles from the "groups" claim. See Config.RoleClaim.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Role is a Linea-internal authorisation role.
type Role uint8

const (
	RoleNone Role = iota
	RoleViewer
	RoleContributor
	RoleCurator
)

// String returns the canonical name of the role.
func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "Viewer"
	case RoleContributor:
		return "Contributor"
	case RoleCurator:
		return "Curator"
	default:
		return "None"
	}
}

// Identity carries the authenticated principal for a request.
type Identity struct {
	// Subject is the JWT 'sub' claim. It is used as the actor
	// recorded on proposal transitions.
	Subject string
	// Role is the resolved Linea role for this request.
	Role Role
	// Raw contains the verified JWT claim set, in case downstream
	// code needs additional fields (audit logging, etc.).
	Raw map[string]any
}

type ctxKey struct{}

// WithIdentity returns a copy of ctx carrying id.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityOf returns the Identity attached to ctx, or zero value
// if none is present.
func IdentityOf(ctx context.Context) Identity {
	if v, ok := ctx.Value(ctxKey{}).(Identity); ok {
		return v
	}
	return Identity{}
}

// Config configures the OIDC verifier.
type Config struct {
	// IssuerURL is the OIDC discovery URL (e.g.
	// "http://keycloak:8080/realms/linea").
	IssuerURL string
	// Audience is the expected `aud` claim value (typically the
	// client id).
	Audience string
	// RoleClaim is the JWT claim path holding role names.
	// Default "groups".
	RoleClaim string
	// RoleMap maps incoming claim values (case-insensitive) to
	// Linea Roles. If empty, sensible defaults are used.
	RoleMap map[string]Role
}

// Verifier verifies JWT bearer tokens against an OIDC issuer.
type Verifier struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	cfg      Config
}

// NewVerifier discovers the OIDC issuer and constructs a Verifier.
//
// In tests / disabled-auth modes the caller may pass a zero Config
// and the returned Verifier will reject every token. Use the
// nil-Verifier sentinel in interceptors to opt out instead.
func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("auth: IssuerURL is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("auth: Audience is required")
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "groups"
	}
	if cfg.RoleMap == nil {
		cfg.RoleMap = defaultRoleMap()
	}
	prov, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: discover issuer: %w", err)
	}
	v := prov.Verifier(&oidc.Config{ClientID: cfg.Audience})
	return &Verifier{provider: prov, verifier: v, cfg: cfg}, nil
}

func defaultRoleMap() map[string]Role {
	return map[string]Role{
		"linea-viewer":      RoleViewer,
		"linea-contributor": RoleContributor,
		"linea-curator":     RoleCurator,
	}
}

// VerifyToken parses and verifies a bearer token, returning the
// resolved Identity. ctx is used for issuer JWKS fetches.
func (v *Verifier) VerifyToken(ctx context.Context, raw string) (Identity, error) {
	tok, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: verify: %w", err)
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("auth: decode claims: %w", err)
	}
	subject, _ := claims["sub"].(string)
	if subject == "" {
		return Identity{}, errors.New("auth: token missing 'sub'")
	}
	role := v.resolveRole(claims)
	return Identity{Subject: subject, Role: role, Raw: claims}, nil
}

// resolveRole walks Config.RoleClaim in claims and returns the
// highest Role found. Falls back to RoleViewer if the claim is
// present but contains no recognised value, or RoleNone if the
// claim is absent (which interceptors will treat as unauthorised).
func (v *Verifier) resolveRole(claims map[string]any) Role {
	raw, ok := claims[v.cfg.RoleClaim]
	if !ok {
		return RoleNone
	}
	values := flattenRoleValues(raw)
	if len(values) == 0 {
		return RoleNone
	}
	best := RoleNone
	for _, val := range values {
		if r, ok := v.cfg.RoleMap[strings.ToLower(strings.TrimSpace(val))]; ok && r > best {
			best = r
		}
	}
	if best == RoleNone {
		// Claim present but no recognised values — treat as the
		// least-privileged authenticated role so the user at least
		// gets read access. Operators can tighten this by leaving
		// the role-claim absent in their identity provider.
		return RoleViewer
	}
	return best
}

func flattenRoleValues(v any) []string {
	switch t := v.(type) {
	case string:
		// Comma- or space-separated single string.
		fields := strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == ' '
		})
		return fields
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}
