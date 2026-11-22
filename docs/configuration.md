# Configuration

## `serve`

| Flag | Initial ENV | Default | Description |
| --- | --- | --- | --- |
| `-addr` | | `127.0.0.1:5000` | TCP address to listen on |
| `-public-url` | `REGISTRY_PUBLIC_URL` | empty | Absolute HTTP/HTTPS URL advertised to clients; required for `serve` |
| `-tls-cert-file` | `REGISTRY_TLS_CERT_FILE` | empty | Certificate PEM |
| `-tls-key-file` | `REGISTRY_TLS_KEY_FILE` | empty | Private key PEM |
| `-storage-root` | | `./data` | Storage root directory |
| `-db` | | `<storage-root>/metadata.db` | SQLite metadata database |
| `-tenant` | | `default` | Identifier of the single active tenant |
| `-allow-anonymous-pull` | | `false` | Allow pulls and inspection without a principal |
| `-allow-anonymous-push` | | `false` | Allow writes without a principal |
| `-auth-postgres-dsn` | `REGISTRY_AUTH_POSTGRES_DSN` | empty | PostgreSQL DSN for auth |
| `-auth-token-realm` | `REGISTRY_AUTH_TOKEN_REALM_URL` | derived | Must match `<public-url>/auth/token` |
| `-realm` | | `regixtry` | Challenge realm |
| `-service` | | `regixtry` | Challenge service |
| `-read-header-timeout` | | `5s` | Header read timeout |
| `-read-timeout` | | `60s` | Request read timeout |
| `-write-timeout` | | `30s` | Response write timeout |
| `-idle-timeout` | | `120s` | Keep-alive idle timeout |
| `-shutdown-timeout` | | `10s` | Graceful shutdown timeout |
| `-trivy-enabled` | `REGISTRY_TRIVY_ENABLED` | `false` | Enable persisted Trivy rescans |
| `-trivy-schedule-enabled` | `REGISTRY_TRIVY_SCHEDULE_ENABLED` | `false` | Enable periodic Trivy rescans |
| `-trivy-interval` | `REGISTRY_TRIVY_INTERVAL` | `24h` | Interval between periodic Trivy rescans |
| `-trivy-timeout` | `REGISTRY_TRIVY_TIMEOUT` | `15m` | Timeout for each Trivy run |
| `-trivy-cache-dir` | `REGISTRY_TRIVY_CACHE_DIR` | `<storage-root>/trivy-cache` | Shared Trivy cache directory |
| `-trivy-binary-path` | `REGISTRY_TRIVY_BINARY_PATH` | `trivy` | Trivy executable path |
| `-trivy-max-concurrency` | `REGISTRY_TRIVY_MAX_CONCURRENCY` | `1` | Maximum concurrent Trivy runs |
| `-delete-enabled` | `REGISTRY_DELETE_ENABLED` | `false` | Enable `DELETE /v2/<name>/manifests/<reference>` (manifest and tag deletion) |
| `-gc-delete-enabled` | `REGISTRY_GC_DELETE_ENABLED` | `false` | Enable `POST /admin/v1/gc/reports/{id}/delete` (irreversibly unlinks unreferenced blob files); distinct from `-delete-enabled`, which is metadata-only and never touches blob files |

Managed scanner runtimes (Trivy, Gitleaks) are installed from GitHub releases. The requested version must be `v1.2.3` or `1.2.3` with an optional `-prerelease` (at most 64 characters); it is validated at the admin API, in the service and again where the release URL is built, and the tag is escaped as one URL path segment. The archive and checksums URLs from the release metadata must be `https` on `github.com`, `api.github.com` or `*.githubusercontent.com`, and redirects are followed only over HTTPS to those hosts or the original host (at most 5). Release metadata and checksums bodies are limited to 1 MiB and archives to 512 MiB. The archive SHA-256 is checked against the release's checksums file; this does not authenticate the release itself, because both come from the same release (no signature is verified).

Push-triggered scans are admitted against three caps (environment only, no flag): service-wide, per repository, and per pushing principal across all its repositories, so one user with many repositories cannot take every slot. A push over a cap never fails: no scan is queued and one scan row per repository with status `skipped` and error `skipped: push scan queue full` records the newest dropped digest that has no real run. A skipped row never counts as the latest scan of a digest or repository and never affects the pull policy. Each scan run, and its wait for a slot, is also bounded by the configured scan timeout plus one minute, so a hung runner releases its slot.

| Environment | Default | Purpose |
|---|---|---|
| `REGISTRY_PUSH_SCAN_MAX_PENDING` | `64` | Maximum push-triggered scans queued or running service-wide |
| `REGISTRY_PUSH_SCAN_MAX_PER_REPOSITORY` | `8` | Same limit for one repository |
| `REGISTRY_PUSH_SCAN_MAX_PER_PUSHER` | `16` | Same limit for one pushing principal across all repositories (pushes without a principal share one bucket) |

Implementation: `cmd/regixtry/main.go` (`parseServeConfig`, `normalizeRuntimeConfig`).

`-public-url` must be absolute, use HTTP or HTTPS, and contain no query or fragment. Certificate and key must be provided together. An `http` public URL cannot be combined with direct TLS. An `https` public URL can represent either direct TLS or a reverse proxy.

The `-trivy-*` flags configure the built-in Trivy feature at process start. For long-term, mutable Trivy configuration on a managed installation, prefer `regixtry feature configure trivy` over re-supplying these flags on every restart — see [installation.md](installation.md#built-in-trivy-feature-migration).

## TUI

| Flag | ENV/default | Description |
| --- | --- | --- |
| `-storage-root` | `./data` or the value detected from setup | Data root |
| `-db` | `<storage-root>/metadata.db` | SQLite database |
| `-tenant` | `default` | Tenant |
| `-auth-postgres-dsn` | `REGISTRY_AUTH_POSTGRES_DSN` | Auth DSN |
| `-api-base-url` | `REGISTRY_API_BASE_URL` | Absolute base URL of the admin API |
| `-snapshot` | `false` | Render the first view and exit |
| `-delete-enabled` | `REGISTRY_DELETE_ENABLED` | Enable the Console's delete actions (`d` on a tag, repository or project, each behind a confirm dialog); same flag/env pairing and default (`false`) as `serve`'s |
| `-gc-delete-enabled` | `REGISTRY_GC_DELETE_ENABLED` | Same flag/env pairing and default (`false`) as `serve`'s; no TUI action reads it yet |

A managed installation can auto-detect values from `/etc/regixtry/regixtry.env`; explicit flags take priority.

## Setup-managed environment

Setup writes `REGISTRY_ADDR`, `REGISTRY_PUBLIC_URL`, `REGISTRY_STORAGE_ROOT`, `REGISTRY_DATABASE_PATH`, `REGISTRY_SERVICE_NAME`, and, when applicable, `REGISTRY_AUTH_POSTGRES_DSN`, `REGISTRY_TLS_CERT_FILE`, and `REGISTRY_TLS_KEY_FILE`. These values are interpreted in `internal/infra/install/linux/templates.go`.

## Other subcommands

See [cli.md](cli.md) for the flags of `bootstrap`, `bootstrap-admin`, `setup`, `uninstall`, and `upgrade`.
