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
| `-max-manifest-bytes` | `REGISTRY_MAX_MANIFEST_BYTES` | `4194304` (4 MiB) | Maximum manifest `PUT` body; larger is `413` `SIZE_INVALID`. Negative disables |
| `-max-upload-bytes` | `REGISTRY_MAX_UPLOAD_BYTES` | `5368709120` (5 GiB) | Maximum total size of one blob upload, enforced while streaming; larger is `413` `SIZE_INVALID`. Negative disables |
| `-max-open-uploads` | `REGISTRY_MAX_OPEN_UPLOADS` | `1000` | Maximum concurrently open blob uploads; more is `429` `TOOMANYREQUESTS`. Negative disables |
| `-max-open-uploads-per-repo` | `REGISTRY_MAX_OPEN_UPLOADS_PER_REPO` | `200` | Maximum concurrently open blob uploads per repository. Negative disables |
| `-upload-ttl` | `REGISTRY_UPLOAD_TTL` | `24h` | Idle time after which an abandoned upload (row and temp file) is deleted by a background reaper. Negative disables the reaper |
| `-trusted-proxies` | `REGISTRY_TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs of reverse proxies whose `X-Forwarded-For` is trusted when logging the client IP of a failed login. Empty ignores `X-Forwarded-For`. An invalid entry is a startup error. See [operations.md](operations.md#reverse-proxy-and-client-ip) |

For the five upload/manifest limits above (`-max-manifest-bytes`, `-max-upload-bytes`, `-max-open-uploads`, `-max-open-uploads-per-repo`, `-upload-ttl`): `0` = use the default, a negative value = disable the limit. A non-numeric environment value is a startup error naming the variable.

Managed scanner runtimes (Trivy, Gitleaks) are installed from GitHub releases. The requested version must be `v1.2.3` or `1.2.3` with an optional `-prerelease` (at most 64 characters); it is validated at the admin API, in the service and again where the release URL is built, and the tag is escaped as one URL path segment. The archive and checksums URLs from the release metadata must be `https` on `github.com`, `api.github.com` or `*.githubusercontent.com`, and redirects are followed only over HTTPS to those hosts or the original host (at most 5). Release metadata and checksums bodies are limited to 1 MiB and archives to 512 MiB. Extraction aborts, removing partial output, beyond 512 MiB for the binary, 1 GiB of decompressed archive data in total or 10000 entries, and replaces (never follows) a pre-existing symlink at the binary path. The archive SHA-256 is checked against the release's checksums file; this does not authenticate the release itself, because both come from the same release (no signature is verified).

Push-triggered scans are admitted against three caps (environment only, no flag): service-wide, per repository, and per pushing principal across all its repositories, so one user with many repositories cannot take every slot. A push over a cap never fails: no scan is queued and one scan row per tenant and repository with status `skipped` and error `skipped: push scan queue full` records the newest dropped digest that has no real run. A skipped row never counts as the latest scan of a digest or repository and never affects the pull policy. Each scan run, and its wait for a slot, is also bounded by the configured scan timeout plus one minute, so a hung runner releases its slot. A secret-scan runner that ignores cancellation leaks one goroutine per scan; at most 16 such stuck scans are tolerated, after which new push scans are recorded as skipped until they return.

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

On a Linux daemon install the auth DSN (it carries the Postgres password) is stored only in `regixtry.env`, a root-owned `0600` file created with that mode and loaded through the unit's `EnvironmentFile=`. It is never an `ExecStart` argument: earlier versions wrote `-auth-postgres-dsn=${REGISTRY_AUTH_POSTGRES_DSN}` into the unit, and systemd expands that into the process arguments, so the password was readable by every local user through `ps` and `/proc/<pid>/cmdline`. The lifecycle provenance file records only a redacted DSN (`auth_postgres_dsn_redacted`, password replaced by `xxxxx`, or `[redacted]` when the DSN cannot be parsed). Env values containing control characters (newline, tab, NUL, ...) are rejected before anything is written. The "Rerun with" hint printed after a permission failure never contains the DSN: it shows it redacted and tells you to supply it through `REGISTRY_AUTH_POSTGRES_DSN` (`sudo --preserve-env=REGISTRY_AUTH_POSTGRES_DSN regixtry setup ...`).

Upgrade note: running `regixtry upgrade` (or `setup`) on an install made by an older version rewrites `regixtry.env` as `0600` (previously `0644`), removes the DSN flag from the unit, and strips the clear-text DSN from the provenance file. Until you do, the old files remain readable by all local users; rotate the Postgres password if the host has untrusted local users.

## Setup-managed environment

Setup writes `REGISTRY_ADDR`, `REGISTRY_PUBLIC_URL`, `REGISTRY_STORAGE_ROOT`, `REGISTRY_DATABASE_PATH`, `REGISTRY_SERVICE_NAME`, and, when applicable, `REGISTRY_AUTH_POSTGRES_DSN`, `REGISTRY_TLS_CERT_FILE`, and `REGISTRY_TLS_KEY_FILE`. These values are interpreted in `internal/infra/install/linux/templates.go`.

## Other subcommands

See [cli.md](cli.md) for the flags of `bootstrap`, `bootstrap-admin`, `setup`, `uninstall`, and `upgrade`.
