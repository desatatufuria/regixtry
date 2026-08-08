# Regixtry

Regixtry is a low-resource, single-binary OCI registry for internal and OSS use. V1 is intentionally narrow: deliver correct local registry behavior first, keep operator visibility simple, and leave platform expansion behind explicit seams instead of mixing it into the first release.

## Quick path

1. Treat the OCI Distribution API as the product boundary.
2. Keep v1 single-tenant, local-storage, and operationally simple.
3. Use `docs/` for reader-facing decisions and `openspec/changes/registry-foundation/`, `openspec/changes/registry-auth-v1/`, and `openspec/changes/registry-operator-admin-api/` for the current implementation contracts.

## Install from GitHub Releases

Use the repo-hosted installer when you want a verified Linux release asset. The installer now owns download, checksum verification, and binary placement only. Lifecycle setup and removal are owned by the installed `regixtry` binary.

### Quick path

1. Run `curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash` to install the verified Linux binary.
2. Finish lifecycle setup with `regixtry setup` for interactive prompts, or pass `--mode binary-only` / `--mode daemon-sqlite --public-url <url> --runtime-tls-mode <local-http|reverse-proxy|direct-tls> --addr <listen-address>` explicitly. Interactive setup can also enable Postgres-backed auth, persist `REGISTRY_AUTH_POSTGRES_DSN`, and bootstrap the first admin.
3. When setup uses `daemon-sqlite`, confirm reachability with `curl -fsSI <public-url>/v2/` and expect HTTP `200` or `401`.

If the installer placed the binary under `~/.local/bin`, use that absolute path with `sudo` for privileged lifecycle commands. The installer and `regixtry setup --mode binary-only` print the exact rerun command for the current binary location.

### Installer and lifecycle contract

| Topic | Decision |
| --- | --- |
| Installer scope | `install.sh` downloads, verifies, and places the `regixtry` binary only. It does not prompt for lifecycle mode, run setup, or run uninstall. |
| Lifecycle entrypoints | `regixtry setup` and `regixtry uninstall` are the operator-facing lifecycle commands in this slice. |
| Interactive setup flow | `regixtry setup` still prompts for `binary-only` vs `daemon-sqlite`; choosing `daemon-sqlite` now also prompts for runtime TLS mode, then prompts for editable `Listen address` and `Public URL`, asks whether auth should be enabled, and when auth is enabled collects separate Postgres host, port, user, password, and ssl mode fields before assembling `REGISTRY_AUTH_POSTGRES_DSN` internally. The auth database name is chosen by the application as `regixtry_auth`. It asks for local cert/key paths only in `direct-tls` mode unless those values were already supplied explicitly. |
| Supported automated lifecycle target | Linux + systemd only. The current host validation accepts Debian, Ubuntu, Linux Mint, and RHEL 9.x/10.x before claiming `daemon-sqlite` success. |
| Deferred automation | Container deployment and `regixtry upgrade` remain manual or deferred in this slice. |
| Binary-only truth | `regixtry setup --mode binary-only` prints next steps only. It does not claim setup success and it writes no lifecycle provenance. |
| Daemon/service truth | `regixtry setup --mode daemon-sqlite` succeeds only when the binary is installed, the service is running, and the configured backend listen address answers `/v2/` with HTTP `200` or `401` using the selected runtime TLS mode. |
| Uninstall truth | `regixtry uninstall` removes recorded lifecycle artifacts best effort from persisted provenance and reports removed, missing, skipped, or failed items truthfully. |

### Common install and lifecycle commands

```bash
# Install the latest verified Linux release binary.
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash

# Install a specific release tag.
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --ref v1.2.3

# Install into a custom directory.
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --dir "$HOME/.local/bin"

# Binary-owned guidance-only setup.
regixtry setup --mode binary-only

# Interactive setup prompts for mode, runtime TLS mode, and, when needed, the daemon listen address, public URL, and auth bootstrap inputs.
regixtry setup

# Linux + systemd lifecycle setup behind a reverse proxy.
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url https://regixtry.example.com \
  --runtime-tls-mode reverse-proxy \
  --addr 127.0.0.1:5000 \
  --storage-root /var/lib/regixtry \
  --state-path /etc/regixtry/bootstrap-state.json \
  --unit-path /etc/systemd/system/regixtry.service

# Linux + systemd lifecycle setup with regixtry serving TLS directly.
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url https://regixtry.example.com \
  --runtime-tls-mode direct-tls \
  --addr 0.0.0.0:5443 \
  --tls-cert-file /etc/regixtry/tls/registry.crt \
  --tls-key-file /etc/regixtry/tls/registry.key \
  --storage-root /var/lib/regixtry \
  --state-path /etc/regixtry/bootstrap-state.json \
  --unit-path /etc/systemd/system/regixtry.service

# Provenance-driven uninstall.
sudo /absolute/path/to/regixtry uninstall
```

### Installer behavior

| Topic | Decision |
| --- | --- |
| Source | Resolves the latest release or `--ref <tag>` from GitHub Releases. |
| Verification | Downloads the matching `regixtry_<version>_linux_<arch>.tar.gz` plus `regixtry_<version>_checksums.txt` and verifies the archive before install. |
| Host scope | Installer automation is Linux only, with `amd64` and `arm64` release assets. |
| Success contract | Success means the verified `regixtry` binary is installed and the installer prints the next binary-owned lifecycle commands. |
| Failure mode | Hard-fails on release asset, checksum, extraction, or unsupported target problems. Archive/download failures print manual guidance. |
| Install target | Uses `/usr/local/bin` when writable, otherwise `~/.local/bin`, or `--dir` when provided. |
| Installed binary name | Always installs the executable as `regixtry`. |

### Daemon/service setup and uninstall assumptions

| Topic | Decision |
| --- | --- |
| Privileges | The default `daemon-sqlite` lifecycle paths write under `/etc` and `/var/lib`, so use a root-owned command context when you want that flow. |
| Service manager | Automated lifecycle setup requires systemd. Regixtry does not pretend SysV, OpenRC, or user-level services are supported here. |
| Reachability check | `setup --mode daemon-sqlite` probes the configured backend listen address at `/v2/` over HTTP for `local-http` and `reverse-proxy`, or over HTTPS for `direct-tls`, and succeeds only on HTTP `200` or `401`. |
| Provenance path | Lifecycle provenance defaults beside the bootstrap receipt as `/etc/regixtry/regixtry-lifecycle-state.json`. |
| Uninstall scope | `regixtry uninstall` removes only recorded lifecycle-managed artifacts and the recorded installed binary. Unrecorded drift stays truthfully out of scope. |

### Manual fallback

Use the manual paths below when you want only the release asset, need a deferred host or deployment model, or cannot satisfy the daemon/service bootstrap assumptions.

- Download a verified Linux asset manually from GitHub Releases when you want a release-backed path without `curl | bash`.
- Build from source manually when you need a non-release or non-Linux path:

```bash
git clone https://github.com/desatatufuria/workspace.git
cd workspace
go build -o regixtry ./cmd/regixtry
install -m 0755 regixtry "$HOME/.local/bin/regixtry"
```

## Local Docker Compose helper runtime

Use Docker Compose when you want a disposable local helper runtime for manual testing with the registry plus Postgres-backed auth.

This top-level Compose setup is a convenience for local bring-up and smoke-style manual checks. It is **not** the primary product verification contract. The canonical verification baseline for this repository remains the Go test/build evidence plus any explicitly captured runtime/manual evidence called out in the verification artifacts.

### Quick path

1. Start Postgres: `docker compose up -d postgres`
2. Bootstrap the first admin: `printf '%s\n' '<admin-password>' | docker compose run --rm -T regixtry bootstrap-admin -password-stdin`
3. Start the registry: `docker compose up -d regixtry`
4. Log in from Docker: `printf '%s\n' '<admin-password>' | docker login localhost:${REGISTRY_PORT:-5517} -u admin --password-stdin`

Compose publishes the registry on `127.0.0.1:${REGISTRY_PORT:-5517}` and sets `REGISTRY_PUBLIC_URL=http://localhost:${REGISTRY_PORT:-5517}` for the explicit local HTTP path. It keeps SQLite/blob data in the `registry-data` volume and auth state in the `postgres-data` volume.

Before the first compose run on a fresh machine, make sure the external Docker network expected by the devcontainer/runtime exists:

```bash
docker network create dtf-netwok
```

### Bootstrap and runtime steps

Use this order whenever auth is enabled:

1. `docker compose up -d postgres`
2. `printf '%s\n' '<admin-password>' | docker compose run --rm -T regixtry bootstrap-admin -username admin -password-stdin`
3. `docker compose up -d regixtry`
4. `printf '%s\n' '<admin-password>' | docker login localhost:${REGISTRY_PORT:-5517} -u admin --password-stdin`
5. Push or pull images against `localhost:${REGISTRY_PORT:-5517}`.

If you need to rotate the bootstrap password later, rerun the bootstrap command with `-rotate-password`.

### Runtime hardening modes

Use one canonical public surface per runtime. The registry derives `/auth/token` from `REGISTRY_PUBLIC_URL`, so operator-facing examples must stay aligned with that URL.

| Mode | Required inputs | Result |
| --- | --- | --- |
| Explicit local HTTP | `REGISTRY_PUBLIC_URL=http://localhost:${REGISTRY_PORT:-5517}` and no TLS cert/key inputs | Supported local/dev path. Docker clients can use the published port directly. |
| HTTPS runtime | `REGISTRY_PUBLIC_URL=https://<host>:<port>` plus both `REGISTRY_TLS_CERT_FILE` and `REGISTRY_TLS_KEY_FILE` | The registry serves HTTPS directly and advertises an HTTPS token realm derived from the canonical public URL. |

Startup now fails before serving traffic when the runtime surface is inconsistent:

- `https://...` public URLs without both TLS files
- `http://...` public URLs combined with TLS inputs
- compatibility `REGISTRY_AUTH_TOKEN_REALM_URL` values that do not exactly match `REGISTRY_PUBLIC_URL + /auth/token`

For local HTTPS smoke runs, provide trusted cert/key files to `docs/verification/scripts/docker-push-pull-smoke.sh` through `TLS_CERT_FILE` and `TLS_KEY_FILE`. When the certificate is self-signed, also set `TLS_CA_FILE` for curl-based readiness checks and make sure your Docker daemon trusts the registry certificate before attempting push/pull.

### Operator admin API

Auth-backed operator administration now has a narrow HTTP surface under `/admin/v1`.

#### Quick path

1. Bootstrap the first admin with `bootstrap-admin` if the auth store is empty.
2. Exchange Basic credentials or an admin credential token at `/auth/token`.
3. Call `/admin/v1/...` with `Authorization: Bearer <access-token>`.

#### Delivered v1 routes

| Area | Endpoints |
| --- | --- |
| Users | `GET /admin/v1/users`, `POST /admin/v1/users`, `POST /admin/v1/users/{id}:enable`, `POST /admin/v1/users/{id}:disable`, `POST /admin/v1/users/{id}:reset-password` |
| Repository grants | `GET /admin/v1/users/{id}/grants`, `PUT /admin/v1/users/{id}/grants/{repository}`, `DELETE /admin/v1/users/{id}/grants/{repository}` |
| Admin credential tokens | `GET /admin/v1/users/{id}/admin-tokens`, `POST /admin/v1/users/{id}/admin-tokens`, `DELETE /admin/v1/users/{id}/admin-tokens/{accessor}` |

#### Admin API guardrails

| Topic | Decision |
| --- | --- |
| Auth model | `/admin/v1` accepts only Bearer access tokens issued through `/auth/token`. |
| Safety rules | The API reuses the existing backend safeguards for admin-only access, weak-password rejection, disabled-user checks, TTL caps, and last-active-admin protection. |
| Error semantics | Admin routes return explicit `401`, `403`, `404`, `409`, and `422` responses without changing Docker-oriented `/v2/*` challenge behavior. |
| Deferred scope | User-list pagination, delete-user, broad profile edits, and break-glass bootstrap flows over `/admin/v1` remain out of scope for this slice. |
| Future clients | CLI-next and TUI-later must act as authenticated API clients; they must not write auth state through local shortcuts. |

### TUI auth administration

The local TUI no longer fabricates an authenticated admin when `-auth-postgres-dsn` is configured. Repository inspection still works, but auth-backed admin mutations stay disabled until a real operator login flow exists.

When a setup-managed systemd installation exists, `regixtry tui` now auto-detects the installed runtime storage root, metadata DB path, and auth Postgres DSN from `/etc/regixtry/regixtry.env`. Explicit CLI flags still override those detected values.

For now, use `bootstrap-admin` only to create or rotate the initial global admin account, then perform auth administration through `/auth/token` plus `/admin/v1` instead of the local TUI.

Example snapshot run against the compose Postgres service:

```bash
go run ./cmd/regixtry tui \
  -storage-root ./data \
  -auth-postgres-dsn "postgres://registry:registry@127.0.0.1:5432/regixtry_auth?sslmode=disable" \
  -snapshot
```

The snapshot will render a notice explaining that local TUI admin actions are intentionally disabled in auth-backed mode.

### Notes

| Topic | Decision |
| --- | --- |
| Image build | Top-level `Dockerfile` builds `cmd/regixtry` into a single runtime image. |
| Auth wiring | `docker-compose.yml` sets `REGISTRY_AUTH_POSTGRES_DSN` plus a canonical `REGISTRY_PUBLIC_URL`. The registry derives `/auth/token` from that public URL, so the advertised bearer challenge stays aligned with the host/port clients actually use. |
| First startup | Auth-enabled `serve` fails fast until a global admin exists, so bootstrap the admin before bringing up `regixtry`. |

## V1 outcome

V1 delivers a correct local registry with clear operator visibility.

| Area | Included in v1 |
| --- | --- |
| Regixtry protocol | OCI/Docker-compatible push and pull for manifests and blobs |
| Discovery | Repository listing, tag browsing, manifest inspection, and blob inspection |
| Storage model | Local filesystem blob storage with SQLite-backed metadata |
| Upload lifecycle | Staged uploads, digest validation, and publish-only-on-valid-content rules |
| Operator experience | Keyboard-first Bubble Tea console for inspection and maintenance basics |
| Access model | Postgres-backed auth state, `/auth/token`, repository-scoped enforcement, and narrow `/admin/v1` operator administration for auth-enabled runtime |
| Future readiness | Tenant, storage, and background-job seams kept explicit while broader auth workflows remain intentionally narrow |

## Non-goals

These are intentionally OUT of v1:

- Multi-tenant isolation and complex RBAC.
- Replication, remote object storage, or cross-instance synchronization.
- Signing orchestration, scanning, provenance pipelines, or platform-style admin APIs beyond the narrow `/admin/v1` operator surface.
- Deletion/retention platforms, operator-driven garbage collection controls, or Harbor-like feature expansion.
- Treating the Docker Engine API as the registry contract.
- Letting the TUI own registry rules or read storage directly.

## API boundary

The product contract is the OCI Distribution / Docker Registry HTTP API surface for registry content exchange.

| In boundary | Out of boundary |
| --- | --- |
| Manifest push/pull | Docker daemon lifecycle management |
| Blob upload/download | Container runtime control |
| Catalog and tag listing | Host orchestration features |
| Auth challenge-ready registry access behavior | Docker Engine APIs that do not act as registry clients |

If a capability depends on Docker daemon control instead of registry protocol behavior, it is not part of this product unless introduced later as a separate capability.

## Architecture guardrails

- Single Go binary with `serve` and `tui` entry modes.
- Regixtry semantics live in application/domain layers, not in HTTP handlers or the TUI.
- Filesystem blobs remain authoritative for content bytes.
- SQLite exists to index repositories, tags, manifests, and upload state cheaply.
- Regixtry metadata stays in SQLite while auth state lives in Postgres when auth is enabled.
- Anonymous pull MAY be enabled by configuration; when disabled, the registry advertises Docker-compatible Bearer challenges that lead clients to `/auth/token`.

## Workflow baseline

This repository follows GitFlow plus a feature-branch-chain review strategy for oversized changes.

1. `main` holds the release/bootstrap baseline.
2. `develop` integrates ongoing product work.
3. `feature/registry-foundation` is the base tracker branch, and `registry-auth-v1` currently advances on feature-branch review slices above that foundation.
4. PR 1 targets `feature/registry-foundation`.
5. Later PR slices target the immediate previous PR branch until the tracker branch is ready for `develop`.

Read `docs/contributing.md` before opening or retargeting any PR slice.

## Documentation map

| Path | Purpose |
| --- | --- |
| `docs/architecture.md` | Layer boundaries, runtime flows, ports, and explicit seams |
| `docs/contributing.md` | GitFlow, feature-branch-chain workflow, and documentation duties |
| `docs/glossary.md` | Shared protocol, storage, and architecture vocabulary |
| `docs/roadmap.md` | V1 workstreams, non-goals, and post-v1 sequencing |
| `openspec/changes/registry-foundation/` | Baseline proposal, specs, design, and task tracking for the implemented foundation |
| `openspec/changes/registry-auth-v1/` | Auth-v1 proposal, specs, design, tasks, apply progress, and verify report |
| `openspec/changes/registry-operator-admin-api/` | Operator admin API proposal, specs, design, tasks, and apply progress |

## Current status

`registry-foundation`, `registry-auth-v1`, and `registry-operator-admin-api` are implemented in the repository.

- The Go test suite passes for the current codebase.
- Local smoke verification has confirmed Docker push/pull plus TUI snapshot rendering for the seeded `registry-foundation/smoke` repository.
- Manual checks against the local Compose helper runtime have also produced supporting evidence for authenticated Docker push against the auth-enabled runtime.

Those local Compose checks are supporting runtime evidence only. They do **not** mean the repository currently guarantees Compose automation as a first-class externally verified runtime contract.

This does **not** mean the product is feature-complete beyond the documented v1 scope. The repository currently proves the local single-node foundation plus the auth-backed registry/operator path: OCI/Docker-compatible content flows, SQLite-backed metadata, Postgres-backed auth state, `/auth/token`, bearer challenge interoperability, authenticated `/admin/v1` operator administration, and an inspection-oriented TUI that shows a security notice instead of allowing local auth-backed admin mutations.
