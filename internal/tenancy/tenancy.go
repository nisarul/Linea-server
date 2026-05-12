// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tenancy carries tenant identity through request contexts.
//
// In v0.1 every interceptor calls TenantOf(ctx); the result is the
// constant DefaultTenant ("default"). In v0.2 a real auth-derived
// tenant id will flow through the same seam without any service
// or storage code change.
//
// Storage adapters MUST scope keys with WithTenant so that v0.2's
// per-tenant Badger directories drop in cleanly.
package tenancy

import "context"

// TenantID is the identifier of a tenant. v0.1 always uses
// DefaultTenant.
type TenantID string

// DefaultTenant is the only legal tenant id in v0.1.
const DefaultTenant TenantID = "default"

type ctxKey struct{}

// WithTenant returns a copy of ctx carrying the supplied tenant.
// Pass DefaultTenant in v0.1 callers.
func WithTenant(ctx context.Context, t TenantID) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// TenantOf returns the tenant id carried by ctx, or DefaultTenant
// if none was attached. v0.1 always returns DefaultTenant.
func TenantOf(ctx context.Context) TenantID {
	if v, ok := ctx.Value(ctxKey{}).(TenantID); ok && v != "" {
		return v
	}
	return DefaultTenant
}
