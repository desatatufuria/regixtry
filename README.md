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
- Three built-in features with a shared lifecycle (list/show/status/configure/install/upgrade/rollback): **Trivy** vulnerability scanning with an optional pull-blocking policy gate, **Gitleaks** secret scanning, and cosign-based image **signing** verification against one or more trusted public keys, globally or per repository — see [`docs/features.md`](docs/features.md).
- CLI for serving, setup/bootstrap/uninstall/upgrade, and a Bubble Tea TUI that doubles as a local read-only console and, when pointed at a running server, a full HTTP admin client with a domain-grouped admin menu (Browse / Security & Compliance / Identity & Access / Operations) — see [`docs/tui.md`](docs/tui.md).
- A Linux release installer with SHA-256-verified `amd64`/`arm64` binaries, driven by `regixtry setup`/`upgrade`.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/regixtry/main/install.sh | bash
regixtry setup
```

`install.sh` installs the binary; `regixtry setup` provisions the Linux/systemd runtime (or use `regixtry serve` directly for local/manual runs, as above). Full procedures, including reverse-proxy and direct-TLS modes: [`docs/installation.md`](docs/installation.md). To move an existing install to a newer release: `regixtry upgrade` (resolves the latest non-prerelease tag from GitHub Releases automatically, or pass `-ref` to pin one).

## Run as a container

Every tagged release also publishes a multi-arch (`linux/amd64`, `linux/arm64`) image to `ghcr.io/desatatufuria/regixtry`. This is a manual, first-class path: run it directly with `docker run`. **`install.sh` and `regixtry setup` do not offer a container-selection branch** — they provision the Linux/systemd host runtime only.

```bash
docker volume create regixtry-data
docker run -d --name regixtry \
  -p 5000:5000 \
  -v regixtry-data:/var/lib/regixtry \
  ghcr.io/desatatufuria/regixtry:latest \
  serve -addr 0.0.0.0:5000 -storage-root /var/lib/regixtry \
    -public-url http://127.0.0.1:5000 \
    -allow-anonymous-pull -allow-anonymous-push
```

Check it's up (the same `/v2/` probe backs the image's built-in `HEALTHCHECK`):

```bash
curl -i http://127.0.0.1:5000/v2/
```

Push and pull with a real Docker client, exactly as in [Quick start (no auth)](#quick-start-no-auth) above:

```bash
docker pull alpine:3.20
docker tag alpine:3.20 127.0.0.1:5000/test/alpine:3.20
docker push 127.0.0.1:5000/test/alpine:3.20
docker pull 127.0.0.1:5000/test/alpine:3.20
```

The image runs as a non-root user (uid `65532`), and data written to `/var/lib/regixtry` survives a container restart as long as it keeps using the same named volume.

### Postgres-backed authentication in a container

Same primitives as [Quick start with authentication and access control](#quick-start-with-authentication-and-access-control) above — same `postgres:17-alpine` pin, same `regixtry_auth`/`registry` DSN shape, same `bootstrap-admin -password-stdin` → `serve -auth-postgres-dsn` order — expressed with `docker network create` + `docker run` instead of `docker compose`:

```bash
# 1. Ephemeral network + Postgres for auth state
docker network create regixtry-net
docker run -d --name regixtry-postgres --network regixtry-net \
  -e POSTGRES_DB=regixtry_auth -e POSTGRES_USER=registry -e POSTGRES_PASSWORD=registry \
  postgres:17-alpine
DSN="postgres://registry:registry@regixtry-postgres:5432/regixtry_auth?sslmode=disable"

# 2. Bootstrap the first global admin -- MUST run before step 3: serve
#    refuses to start with auth enabled and no existing admin
printf '%s\n' 'change-me-now' | docker run --rm -i --network regixtry-net \
  ghcr.io/desatatufuria/regixtry:latest \
  bootstrap-admin -auth-postgres-dsn "$DSN" -username admin -password-stdin

# 3. Serve, with auth enabled
docker volume create regixtry-data
docker run -d --name regixtry --network regixtry-net -p 5000:5000 \
  -v regixtry-data:/var/lib/regixtry \
  -e REGISTRY_AUTH_POSTGRES_DSN="$DSN" \
  ghcr.io/desatatufuria/regixtry:latest

# 4. Log in and push
docker login 127.0.0.1:5000 -u admin -p change-me-now
docker push 127.0.0.1:5000/test/alpine:3.20
```

The `registry:registry` Postgres credential above is a throwaway local-experimentation default, exactly as in the `docker compose` quick start — use real secrets for anything beyond a scratch environment.

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
- Blob garbage collection and manifest/tag delete both exist but are off by default (`REGISTRY_GC_DELETE_ENABLED`, `REGISTRY_DELETE_ENABLED`) — see [`docs/registry.md`](docs/registry.md) and [`docs/operations.md`](docs/operations.md). Upload cancellation returns `UNSUPPORTED`.
- The TUI's local Console browsing (repositories/tags/manifests) is **not** access-controlled — it requires no login at all, and any local session can browse everything. The real enforcement boundaries are the Docker registry protocol (`/v2/...`) and the admin API (`/admin/v1/...`). See [`docs/security.md`](docs/security.md).
- No confirmed full OCI Distribution Specification conformance certification or multi-arch manifest-list handling.

Verified scope and known gaps are tracked in [`docs/documentation-audit.md`](docs/documentation-audit.md).
