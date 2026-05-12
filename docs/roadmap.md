# Linea-server Roadmap

## v0.1 — single-tenant, tenant-aware skeleton

- gRPC + grpc-gateway REST.
- OIDC auth, RBAC (Viewer / Contributor / Curator).
- Linea-core Badger backend, single graph at `--data-dir`.
- Tenant context seam: every interceptor calls `auth.TenantOf(ctx)` returning
  the constant `"default"`. Storage paths run through `keys.WithTenant(t, ...)`.
  Config has `linea.tenancy.mode = "single"`.
- All tests pass tenant explicitly so we don't accumulate hidden single-tenant
  assumptions.

## v0.2 — Per-tenant database multi-tenancy

- One Badger directory per tenant: `<data-dir>/tenants/<tenantId>/`.
- Strong isolation, simple security story, easy backup-per-tenant.
- New `Tenants` admin service: Create / List / Archive / Delete, gated by a
  new `PlatformAdmin` role.
- Tenant identity from a configurable JWT claim (default `tenant`); fallback to
  `default` tenant only if a flag explicitly allows it.
- Per-tenant Badger handles cached in a `TenantManager` (bounded LRU); idle
  tenants closed and reopened on demand.
- Conformance suite gains a per-tenant create/destroy hook.
- Public API additions only — no breakage from v0.1.

## v0.3 — Hardening + stronger isolation if needed

- Per-tenant resource quotas (storage size, RPC rate, max persons).
- Per-tenant encryption-at-rest keys (Badger encryption with KMS-backed DEK).
- Cross-tenant query semantics (probably explicit `--scope` or unsupported by
  default).
- Optional per-tenant cell / process isolation for high-security tenants.

## Decisions locked

- Multi-tenancy isolation model: **per-tenant Badger DB** (not shared DB with
  key prefixes).
- Tenancy is OUT of scope for v0.1; only the seam exists in v0.1.

## Open questions for v0.2

- Tenant ID format: opaque UUID vs human slug.
- Whether the `default` tenant continues to exist in v0.2 or is deprecated.
- Subdomain-based tenant routing vs JWT-claim-only.
