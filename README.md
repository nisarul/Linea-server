# Linea Server

Production-grade gRPC + REST service exposing the [Linea](https://github.com/nisarul/Linea-specs)
genealogical graph framework, built on [`Linea-core`](https://github.com/nisarul/Linea-core).

> Linea — lineage, without assumptions.

## Status

Pre-release. Implements spec v1.1.0.

**v0.2 ships multi-tenancy**: each Genealogy is isolated in its
own embedded Badger database under `<data-dir>/genealogies/<id>/`,
with a global platform DB at `<data-dir>/platform/`.

## Features (v0.2)

- gRPC service definitions in [`proto/linea/v1/`](./proto/linea/v1)
- Auto-generated REST gateway via `grpc-gateway` (one source of truth)
- OIDC bearer-token authentication (Keycloak in dev, any OIDC issuer in prod)
- Per-Genealogy roles: `Owner`, `Curator`, `Contributor`, `Viewer` (CCGGS §8.1 + Owner split)
- Visibility tiers: `Private` (default), `Unlisted`, `Public`
- Implicit roles based on visibility (anonymous Viewer on Public; logged-in Contributor on Public/Unlisted)
- All graph mutation goes through the proposal lifecycle (no direct PUT RPCs on the wire)
- Free-tier quotas: 5 private genealogies/user, 1000 persons/genealogy (configurable)
- Anti-abuse: in-process per-key rate-limit interceptor, ban-from-genealogy, BulkReject RPC
- OpenTelemetry-ready (slog structured JSON for now), Prometheus `/metrics` (planned)
- `/healthz` liveness + `/readyz` readiness probes

## What v0.2 does NOT do

- TLS termination (deploy behind an ingress / sidecar)
- Streaming change subscriptions (planned for v0.3)
- Cross-replica rate limits — current limiter is in-process. v0.3 swaps in Redis.
- Name-based search across Public genealogies (planned for v0.3)
- Per-genealogy custom domains / billing
- Forking a genealogy's data inside one server (forking the *source* is already free via AGPL)

## Quick start (dev)

```sh
# Start Keycloak + the server with a sample realm
cd deploy
docker compose -f docker-compose.dev.yml up

# In another shell, login as the demo user 'curator/curator' and call the server
# (full curl/grpcurl examples live in deploy/README.md)
```

## License

AGPL-3.0-or-later. See [LICENSE](./LICENSE).
The Linea specifications themselves are licensed under CC BY 4.0.
