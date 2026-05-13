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
	// Subject is a stable per-issuer-per-user identifier formatted
	// as "{iss}|{sub}". When only one issuer is configured the
	// prefix is constant and effectively transparent. With multiple
	// issuers (e.g. Microsoft + Google) the prefix prevents cross-
	// issuer collisions for users who happen to share a 'sub'.
	Subject string
	// Issuer is the verifier that accepted the token (raw `iss`).
	Issuer string
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
//
// Multiple issuer/audience pairs may be supplied (paired by index)
// to accept tokens from any of several identity providers. When
// only one is configured the verifier behaves like the legacy
// single-issuer setup.
type Config struct {
	// IssuerURLs are the OIDC discovery URLs to trust.
	IssuerURLs []string
	// Audiences are the expected `aud` claim values, paired with
	// IssuerURLs by index.
	Audiences []string
	// RoleClaim is the JWT claim path holding role names.
	// Default "groups".
	RoleClaim string
	// RoleMap maps incoming claim values (case-insensitive) to
	// Linea Roles. If empty, sensible defaults are used.
	RoleMap map[string]Role

	// IssuerURL / Audience are deprecated single-value fields kept
	// for backward compatibility. When IssuerURLs/Audiences are
	// empty, these are used as a one-element list.
	IssuerURL string
	Audience  string
}

// Verifier verifies JWT bearer tokens against one or more
// configured OIDC issuers. The first issuer whose verifier
// accepts the token wins.
type Verifier struct {
	verifiers []issuerVerifier
	cfg       Config
}

type issuerVerifier struct {
	issuerURL string
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
}

// NewVerifier discovers each configured OIDC issuer and constructs
// a Verifier. Returns an error if none are configured or any
// discovery fails.
func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
	issuers := cfg.IssuerURLs
	auds := cfg.Audiences
	if len(issuers) == 0 && cfg.IssuerURL != "" {
		issuers = []string{cfg.IssuerURL}
	}
	if len(auds) == 0 && cfg.Audience != "" {
		auds = []string{cfg.Audience}
	}
	if len(issuers) == 0 {
		return nil, errors.New("auth: at least one issuer is required")
	}
	if len(auds) != len(issuers) {
		return nil, fmt.Errorf("auth: have %d issuers but %d audiences (must be paired)", len(issuers), len(auds))
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "groups"
	}
	if cfg.RoleMap == nil {
		cfg.RoleMap = defaultRoleMap()
	}
	out := &Verifier{cfg: cfg}
	for i, iss := range issuers {
		prov, err := oidc.NewProvider(ctx, iss)
		if err != nil {
			return nil, fmt.Errorf("auth: discover issuer %s: %w", iss, err)
		}
		out.verifiers = append(out.verifiers, issuerVerifier{
			issuerURL: iss,
			provider:  prov,
			verifier:  prov.Verifier(&oidc.Config{ClientID: auds[i]}),
		})
	}
	return out, nil
}

func defaultRoleMap() map[string]Role {
	return map[string]Role{
		"linea-viewer":      RoleViewer,
		"linea-contributor": RoleContributor,
		"linea-curator":     RoleCurator,
	}
}

// VerifyToken parses and verifies a bearer token against any of
// the configured issuers, returning the resolved Identity.
// ctx is used for issuer JWKS fetches.
func (v *Verifier) VerifyToken(ctx context.Context, raw string) (Identity, error) {
	var lastErr error
	for _, iv := range v.verifiers {
		tok, err := iv.verifier.Verify(ctx, raw)
		if err != nil {
			lastErr = err
			continue
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
		return Identity{
			// Namespace the subject by issuer to prevent cross-issuer
			// collisions when multiple identity providers are configured.
			Subject: tok.Issuer + "|" + subject,
			Issuer:  tok.Issuer,
			Role:    role,
			Raw:     claims,
		}, nil
	}
	if lastErr != nil {
		return Identity{}, fmt.Errorf("auth: verify: %w", lastErr)
	}
	return Identity{}, errors.New("auth: no issuers configured")
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
