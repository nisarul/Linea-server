// SPDX-License-Identifier: AGPL-3.0-or-later

// Package service implements the Linea gRPC services as thin
// wrappers around Linea-core. They are deliberately small: the
// real spec semantics (proposal state machine, query ranking,
// path explanations) live in Linea-core and are exposed unchanged.
//
// Tenancy: every service method calls tenancy.TenantOf(ctx) at
// the start; the result is currently unused (single-tenant v0.1)
// but the call shape is preserved so v0.2's per-tenant database
// dispatch can be added in one place without touching service code.
package service
