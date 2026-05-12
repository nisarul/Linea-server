# Linea Server

Production-grade gRPC + REST service exposing the [Linea](https://github.com/nisarul/Linea-specs)
genealogical graph framework, built on [`Linea-core`](https://github.com/nisarul/Linea-core).

> Linea — lineage, without assumptions.

## Status

Pre-release. Implements spec v1.1.0.

**v0.1 ships single-tenant** with a tenant-aware skeleton already in place;
**v0.2 brings real multi-tenancy with one Badger database per tenant.**
The public API will not break between v0.1 and v0.2 — only the `Tenants`
admin service is added.

## Features (v0.1)

- gRPC service definitions in [`proto/linea/v1/`](./proto/linea/v1)
- Auto-generated REST gateway via `grpc-gateway` (one source of truth)
- OIDC bearer-token authentication (Keycloak in dev, any OIDC issuer in prod)
- RBAC roles per CCGGS §8.1: Viewer, Contributor, Curator
- All graph mutation goes through the proposal lifecycle (no direct PUT RPCs on the wire)
- OpenTelemetry traces, Prometheus `/metrics`, structured JSON logs
- `/healthz` liveness + `/readyz` readiness probes

## What v0.1 does NOT do

- True multi-tenancy (planned for v0.2 — see [docs/roadmap.md](./docs/roadmap.md))
- TLS termination (deploy behind an ingress / sidecar)
- Rate limiting (planned for v0.2)
- Streaming change subscriptions (planned for v0.3)

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
