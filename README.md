# Regixtry

Regixtry is a low-footprint OCI/Docker image registry distributed as a single Go binary. It exposes an HTTP surface compatible with the Docker Registry/OCI push-pull flows this repository implements, stores blobs on the filesystem, and keeps registry metadata in SQLite. Optional authentication and access control run on a separate PostgreSQL database.

It is not a full Harbor replacement: the current scope is single-tenant, single-node, and deliberately narrow — a correct local registry first, platform features later. See [`docs/roadmap.md`](docs/roadmap.md) for what's in and out of scope.

## Quick start (no auth)

Requirements: Go `1.26.0`, or Docker/Compose.

```bash
go run ./cmd/regixtry serve \
  -addr 127.0.0.1:5000 \
  -public-url http://127.0.0.1:5000 \
  -storage-root ./data
```

Check it's up:

```bash
curl -i http://127.0.0.1:5000/v2/
```

Push and pull with a real Docker client:

```bash
docker pull alpine:3.20
docker tag alpine:3.20 127.0.0.1:5000/test/alpine:3.20
docker push 127.0.0.1:5000/test/alpine:3.20
docker pull 127.0.0.1:5000/test/alpine:3.20
```

## Quick start with authentication and access control

Auth is entirely optional — pass `-auth-postgres-dsn` (or set `REGISTRY_AUTH_POSTGRES_DSN`) and Postgres-backed users, grants, and robot accounts turn on. Without it, the registry runs anonymous.

```bash
# 1. Postgres for auth state (separate database from the SQLite registry metadata)
docker network create dtf-netwok   # required once; docker-compose.yml expects this network to already exist
docker compose up -d postgres
DSN="postgres://registry:registry@localhost:15432/regixtry_auth?sslmode=disable"

# 2. Bootstrap the first global admin
regixtry bootstrap-admin -auth-postgres-dsn "$DSN" -username admin -password-stdin <<< 'change-me-now'

# 3. Serve, with auth enabled
regixtry serve -addr 127.0.0.1:5000 -storage-root ./data -auth-postgres-dsn "$DSN"

# 4. Log in and push
docker login 127.0.0.1:5000 -u admin -p change-me-now
docker push 127.0.0.1:5000/test/alpine:3.20
```

From here, `regixtry tui -api-base-url http://127.0.0.1:5000` gives you an interactive console to create users, grant per-repository roles (`repo-reader`/`repo-writer`/`repo-admin`), delegate grant management to a repo-admin without making them a global admin, mint bounded-TTL robot accounts for CI/CD, or flip a user to registry-wide read-only. Full walkthrough: [`docs/users.md`](docs/users.md) and [`docs/tui.md`](docs/tui.md).

## What's implemented

- Push/pull of manifests and blobs over the real `/v2/` surface — catalog and tag listing with `n`/`last` pagination, chunked uploads (`POST`/`PATCH`/`PUT`) with SHA-256 validation before a blob is published.
- Registry metadata in SQLite, blob content on the filesystem.
- Optional PostgreSQL-backed access control: users, per-repository role grants, delegated repo-admin grant management, registry-wide read-only accounts, and bounded-TTL revocable robot accounts for CI — see [`docs/authentication.md`](docs/authentication.md) and [`docs/security.md`](docs/security.md).
- Bearer challenge and token issuance at `/auth/token`; a large authenticated admin API under `/admin/v1` — see [`docs/api.md`](docs/api.md).
- Three built-in features with a shared lifecycle (list/show/status/configure/install/upgrade/rollback): **Trivy** vulnerability scanning with an optional pull-blocking policy gate, **Gitleaks** secret scanning, and cosign-based image **signing** verification — see [`docs/features.md`](docs/features.md).
- CLI for serving, setup/bootstrap/uninstall/upgrade, and a Bubble Tea TUI that doubles as a local read-only console and, when pointed at a running server, a full HTTP admin client.
- A Linux release installer with SHA-256-verified `amd64`/`arm64` binaries, driven by `regixtry setup`/`upgrade`.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/regixtry/main/install.sh | bash
regixtry setup
```

`install.sh` installs the binary; `regixtry setup` provisions the Linux/systemd runtime (or use `regixtry serve` directly for local/manual runs, as above). Full procedures, including reverse-proxy and direct-TLS modes: [`docs/installation.md`](docs/installation.md). To move an existing install to a newer release: `regixtry upgrade` (resolves the latest non-prerelease tag from GitHub Releases automatically, or pass `-ref` to pin one).

## Try a feature: vulnerability scanning

Trivy is the most complete built-in feature end to end — a good first thing to try after setup:

```bash
regixtry feature configure trivy \
  -enabled -schedule-enabled -interval 6h -timeout 20m \
  -service-url https://scanner.example.com \
  -registry-reachable-url https://registry.internal:5443 \
  -max-concurrency 2

regixtry feature status trivy
docker push 127.0.0.1:5000/test/alpine:3.20   # triggers a push-scan once enabled
curl -s http://127.0.0.1:5000/v2/test/alpine/manifests/3.20/scan-status
```

Gitleaks and signing follow the same `regixtry feature ...` shape — full detail, including each feature's specific quirks (Gitleaks has no independent trigger; signing is fail-closed by default, unlike Trivy), in [`docs/features.md`](docs/features.md).

## Documentation

- [Getting started](docs/getting-started.md)
- [Installation](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Architecture](docs/architecture.md)
- [Authentication and access control](docs/authentication.md)
- [Users, grants, and robot accounts](docs/users.md)
- [HTTP API](docs/api.md)
- [Registry / Docker-OCI protocol](docs/registry.md)
- [CLI](docs/cli.md) and [TUI](docs/tui.md)
- [Built-in features (Trivy, Gitleaks, Signing)](docs/features.md)
- [CI/CD](docs/ci-cd.md)
- [Operations](docs/operations.md)
- [Security](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Development](docs/development.md)
- [Code reference](docs/code-reference.md)
- [Documentation audit](docs/documentation-audit.md)
- [Roadmap](docs/roadmap.md)

## Current limits

- Single-tenant only; the default (and only) tenant is `default`.
- No remote/replicated storage, no metrics or dedicated health endpoint — readiness is `/v2/`.
- No garbage collection and no manifest/blob delete API; upload cancellation returns `UNSUPPORTED`.
- The TUI's local Console browsing (repositories/tags/manifests) is **not** access-controlled — it requires no login at all, and any local session can browse everything. The real enforcement boundaries are the Docker registry protocol (`/v2/...`) and the admin API (`/admin/v1/...`). See [`docs/security.md`](docs/security.md).
- No confirmed full OCI Distribution Specification conformance certification or multi-arch manifest-list handling.

Verified scope and known gaps are tracked in [`docs/documentation-audit.md`](docs/documentation-audit.md).
