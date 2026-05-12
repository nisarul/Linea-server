// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"github.com/nisarul/Linea-server/internal/authz"
	"github.com/nisarul/Linea-server/internal/platform"
	"github.com/nisarul/Linea-server/internal/quotas"
	"github.com/nisarul/Linea-server/internal/tenants"
)

// platformDeps bundles the v0.2 infrastructure each service
// constructor needs. Centralising this keeps the wire-up code in
// internal/server short and uniform.
type platformDeps struct {
	Platform *platform.Store
	Authz    *authz.Resolver
	Tenants  *tenants.Manager
	Quotas   *quotas.Enforcer
}

func (p *platformDeps) resolver() resolver {
	return resolver{platform: p.Platform, authz: p.Authz, tenants: p.Tenants}
}

// PlatformDeps is the exported alias used by package server.
type PlatformDeps = platformDeps
