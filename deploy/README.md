# Linea Server — Local Dev Setup

## TL;DR

```sh
cd deploy
docker compose -f docker-compose.dev.yml up
```

This brings up:

- **Keycloak** at `http://localhost:8089` (admin: `admin / admin`)
  - A pre-imported `linea` realm with three demo users:
    | username | password | role        |
    |----------|----------|-------------|
    | viewer   | viewer   | Viewer      |
    | author   | author   | Contributor |
    | curator  | curator  | Curator     |
  - Public OIDC client `linea-cli` (no client secret).
- **lineasrv** at `http://localhost:8080` (REST) and `localhost:9090` (gRPC).

## Get a token

```sh
TOKEN=$(curl -s \
  -d "client_id=linea-cli" \
  -d "username=curator" \
  -d "password=curator" \
  -d "grant_type=password" \
  http://localhost:8089/realms/linea/protocol/openid-connect/token \
  | jq -r .access_token)
```

## Call the API

```sh
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/v1/version
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/v1/persons
```

## Auth-disabled mode (faster local dev, NEVER for prod)

```sh
LINEA_AUTH_MODE=disabled \
LINEA_DATA_DIR=./.devdata \
go run ./cmd/lineasrv
```

The server refuses to start with `LINEA_AUTH_MODE=disabled` if
`LINEA_ENV=production`.
