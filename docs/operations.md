# Operations

## Service

For managed installations:

```bash
systemctl status regixtry
systemctl restart regixtry
journalctl -u regixtry -f
```

The server logs method, path, status, duration, and the challenge when present. There is no dedicated metrics or health endpoint; readiness checks use `/v2/` and accept `200` or `401`.

## Backup

Registry state requires keeping `metadata.db` and the `content` directory together. PostgreSQL holds users, grants, and tokens when auth is enabled. The current code does not confirm a built-in backup/restore command; use external SQLite/PostgreSQL tooling per your operational policy.

## TLS

`local-http` does not encrypt traffic. `reverse-proxy` delegates TLS to a proxy. `direct-tls` uses `-tls-cert-file` and `-tls-key-file`. The public URL must match the scheme and the derived realm.

## Reverse proxy and client IP

Failed logins are logged with the client IP and the reason (see [Failed logins](security.md#failed-logins-and-rate-limiting)). By default the IP is the TCP peer (`RemoteAddr`) and `X-Forwarded-For` is **ignored**, so a client cannot choose the address it is logged under. Behind a reverse proxy the peer is the proxy, so every failure would be attributed to it. Set `-trusted-proxies` (env `REGISTRY_TRUSTED_PROXIES`) to the proxy addresses or CIDRs:

- If the TCP peer is not in the list, forwarding headers are ignored.
- If it is, `X-Forwarded-For` (all header lines merged) is read right to left, skipping trusted hops; the first untrusted address is the client. Only the rightmost untrusted value is reliable: anything to its left may have been forged by the client. `X-Real-IP` is never used.
- Keep the list as narrow as possible. A range that includes untrusted hosts lets them forge their logged IP.

The proxy must **append** the peer it saw to `X-Forwarded-For` (not overwrite it with a client value). Per-IP volume limiting stays in the proxy; the registry only logs and counts.

nginx (on `192.168.1.245`, which sends `X-Real-IP` and appends the client to `X-Forwarded-For`):

```nginx
location / {
    proxy_pass         http://127.0.0.1:5000;
    proxy_set_header   Host $host;
    proxy_set_header   X-Real-IP $remote_addr;
    proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header   X-Forwarded-Proto $scheme;
    client_max_body_size 0;
    # optional per-IP limit for the token endpoint
    # limit_req_zone $binary_remote_addr zone=authtoken:10m rate=30r/m;
    # location = /auth/token { limit_req zone=authtoken burst=20 nodelay; proxy_pass http://127.0.0.1:5000; ... }
}
```

```
regixtry serve -trusted-proxies 192.168.1.245
```

Traefik (v3, file provider; Traefik appends the client to `X-Forwarded-For` itself, and by default does not trust incoming forwarded headers from untrusted clients):

```yaml
entryPoints:
  websecure:
    address: ":443"
    forwardedHeaders:
      trustedIPs: ["203.0.113.0/24"]   # only proxies/load balancers in FRONT of Traefik
```

Run the registry with `-trusted-proxies` set to Traefik's address on the Docker/host network (for example `172.18.0.0/16`).

Caddy (`reverse_proxy` sets `X-Forwarded-For` by appending the peer; by default it discards an incoming value from untrusted clients):

```
registry.example.com {
    reverse_proxy 127.0.0.1:5000
}
```

with `-trusted-proxies 127.0.0.1` (add `::1` if Caddy connects over IPv6 loopback).

## Upgrade and rollback

`upgrade` uses the setup provenance record, runs preflight checks, supports `--ref` and `--yes`, and shows progress. Bootstrap rollback removes generated artifacts and stops the service. Review the command output and `journalctl` before retrying.

### Upgrade to the projects release

This release adds projects (the first path segment of a repository name), project roles, and turns project enforcement **on by default when auth is enabled** (off when anonymous). The release notes are in [release-notes-projects.md](release-notes-projects.md). Read the three warnings first.

> **Irreversible: the scan-history purge.** The first start of this version runs a one-time migration that permanently deletes duplicate scan history (see [the purge note](#upgrade-note-scan-history-purge-irreversible) below; about 12 s per 200k scan runs). Restoring a backup is the only undo.
>
> **Enforcement is on by default when auth is enabled.** Pushes to a project that does not exist are refused with `404 NAME_UNKNOWN`, and a new repository without a slash is refused with `400 NAME_INVALID`. Existing repositories keep working.
>
> **A deployment without auth (no `-auth-postgres-dsn`) keeps enforcement off by default**, because projects are created through the admin API, which is not mounted without auth. An explicit `-require-project=true` without a DSN fails at startup (`project enforcement needs the admin API: configure -auth-postgres-dsn or set -require-project=false`).

**Before upgrading**

1. Back up SQLite: stop the service cleanly (or copy `metadata.db` together with its `-wal` and `-shm` files) and keep `metadata.db` and the `content` directory together. The new `projects` table and the project backfill are additive, but the purge is not.
2. Back up the Postgres auth database (for example `pg_dump --format=custom "$DSN" > auth.dump`). The release adds one table, `auth_project_grants`, created automatically at start; nothing existing is changed.
3. Decide whether to run with enforcement (auth deployments: on unless you say otherwise). To turn it off, set `REGISTRY_REQUIRE_PROJECT=false` (systemd: a line in the `EnvironmentFile`; Docker Compose: set `REGIXTRY_REQUIRE_PROJECT=false` in `.env`; the shipped compose file forwards it, and an empty value means the default; a compose file from an earlier release needs `REGISTRY_REQUIRE_PROJECT: "${REGIXTRY_REQUIRE_PROJECT:-}"` added under the `regixtry` service `environment`) or pass `-require-project=false`. After a `setup` or `upgrade` that regenerates those files, check the setting is still there.

**What the first start does.** It creates the `projects` table and, once (marker `projects_from_repositories_v1`), one project per first path segment of the slashed repositories that exist at that moment, with `created_by` `migration`. Flat repositories create no project and keep working with their exact grants. Nothing is deleted. The backfill does not run again: a repository pushed later while enforcement was off gets no project, and a project you delete is not recreated. `serve` prints the effective mode and why (`project enforcement: on (default: admin API available)`, `off (default: no admin API, ...)` or `on|off (explicit)`) before `regixtry serving on ...`.

**Create projects before opening pushes to a new customer.** Only a global admin creates projects. Get an unrestricted token (a `docker login` token carries scopes and is refused by the admin routes):

```sh
TOKEN=$(curl -s -u admin:PASSWORD 'https://registry.example.com/auth/token?service=regixtry' | jq -r .token)
curl -X POST -H "Authorization: Bearer $TOKEN" -d '{"name":"odabank"}' \
  https://registry.example.com/admin/v1/projects
curl -X PUT -H "Authorization: Bearer $TOKEN" -d '{"role":"repo-writer"}' \
  https://registry.example.com/admin/v1/projects/odabank/members/ana
```

In the TUI (`regixtry tui -api-base-url ...`): `Admin > Access > Projects`, `n` to create, `Enter` on a project and `g` to give a role. See [tui.md](tui.md#projects).

**Troubleshooting**

| Symptom | Cause and fix |
| --- | --- |
| Push fails with `404` `NAME_UNKNOWN` (`project "odabank" does not exist; ask an administrator to create it`) | The project has no row. Create it as above. A caller without push permission never sees this message (authorization is checked first). |
| Push fails with `400` `NAME_INVALID` (`repository "x" has no project; use <project>/x ...`) | New flat names are refused while enforcement is on. Push under a project (`odabank/x`). |
| A repository that used to push now gets `NAME_UNKNOWN` | It was created after the backfill ran with enforcement off, or while an older binary was running after a rollback. Create its project. |
| Nothing can create projects | No auth database is configured, so enforcement defaults to off and an explicit `true` refuses to start. Add `-auth-postgres-dsn` to use projects. |
| Admin call answers `503` `... was deleted but its grants could not be purged; retry the delete` | The auth database was unreachable during a project delete. Repeat the same `DELETE`. |

**Legacy flat repositories.** Existing flat repositories (no slash) keep working: push, pull, tags, and their exact grants are unchanged, and no project is needed. What is refused is creating a new flat name. A flat repository whose first push failed midway has no repository row, so it cannot be retried while enforcement is on.

**Rollback.** Stop the service and start the previous binary. It ignores the `projects` table and `auth_project_grants`, and does not enforce projects, so pushes to any name work again. Project grants are **not honored** by the old binary: a user or robot whose access came only from a project role has no access until it gets exact repository grants (this fails closed). Project roles, projects and their data stay in place and work again after you upgrade. Repositories created while the old binary runs have no project row, so after re-upgrading with enforcement on, pushing to them answers `NAME_UNKNOWN` until you create their project (the backfill does not run again). The scan-history purge is **not** undone by switching binaries; only restoring the `metadata.db` backup brings that history back, together with the loss of everything pushed since the backup. Restore the Postgres dump only if you also want to discard project roles and any other auth change made since.

**Behavior changes in this release**

- SQLite writes wait instead of failing: single-statement writes now go through the same FIFO write queue as write transactions, so a push no longer surfaces a raw `SQLITE_BUSY` when a long write (for example a garbage-collection reclaim) is running. A push in that window is slower, not failed.
- Cancelling or reaping an upload no longer waits for a client that stopped sending: `DELETE .../blobs/uploads/<id>` and the reaper return while a writer is parked in a body read, and that writer's request ends.

Note on the upload reaper: besides expired upload rows, it removes upload directories under `<storage-root>/content/uploads/` that have no session row and are older than the upload TTL.

### Upgrade note: scan history purge (irreversible)

Scan results are now latest-only: one result per (tenant, repository, digest). The first start of a version with this model runs a one-time migration (`scan_runs_latest_only_purge_v1` in `schema_backfills`) that **permanently deletes** the old duplicate scan history. Per digest it keeps the newest completed run with its findings and DB freshness (else the newest failed run, else the newest skipped row); queued and running runs are never deleted, and skipped rows are removed only for digests that have a completed or failed result. Secret-scan history is not touched.

**Back up the SQLite database (`metadata.db`, with its `-wal` file or after a clean stop) before upgrading.** There is no undo.

The purge runs in batches of at most 200 deleted runs, each in its own short write transaction, so pushes keep flowing while it runs. It is resumable: if the process stops midway, the next start continues, and the marker is written only when nothing is left. Later starts only check the marker. The file does not shrink by itself; `VACUUM` is an optional operator step to reclaim disk, run with the registry stopped because it rewrites the whole database and takes an exclusive lock.

Expect the purge to take roughly 12 s per 200k scan runs (measured on a 200k-run database); pushes interleave between batches.

Release note text:

- Scan results are latest-only: one result per image digest. Digests are rescanned only when the vulnerability DB or the scan settings change; a manual scan always rescans.
- The first start after upgrading permanently deletes duplicate scan history (irreversible, see above). Back up `metadata.db` first.
- Stricter pull policy: a digest whose newest run failed over an older critical completed run is now blocked. The previous behavior allowed it. A failed rescan keeps the previous completed result (marked stale) and the policy keeps enforcing it.
- A failed vulnerability DB refresh during the sweep continues with the cached DB; the Image scanning screen then reads `DB refresh failed, results may be stale`.

## Cleanup

Blob garbage collection is a report-then-delete admin flow, off by default. Compute a report with `POST /admin/v1/gc/reports` — always reachable, and safe to run repeatedly since it only reads state — then review it (`GET /admin/v1/gc/reports/{id}`) before acting on it. Deletion (`POST /admin/v1/gc/reports/{id}/delete`) is gated by `REGISTRY_GC_DELETE_ENABLED` (default `false`, returns `UNSUPPORTED`/`501` while disabled); enable it only once you're ready to irreversibly unlink blob files. A 24-hour grace window and a fresh re-check at delete time protect blobs from an in-flight push, and each blob's removal is atomic with manifest publishes and upload links (a push racing a delete either publishes with all its blobs intact or is refused with a `409`, never leaving a manifest with a missing layer) — see [`docs/registry.md`](registry.md#blob-garbage-collection) for the full mark-sweep-grace mechanism.

Abandoned blob uploads are cleaned up automatically: a background reaper (runs in `serve`, every `-upload-ttl`/4 at most hourly) deletes uploads idle longer than `REGISTRY_UPLOAD_TTL` (default `24h`), both the session row and the temp file under `<storage-root>/content/uploads/`. It skips an upload with a write or commit in flight and never touches committed blobs. Clients can also cancel their own upload with `DELETE /v2/<name>/blobs/uploads/<id>`. To bound disk use further, tune `REGISTRY_MAX_UPLOAD_BYTES`, `REGISTRY_MAX_OPEN_UPLOADS` and `REGISTRY_MAX_OPEN_UPLOADS_PER_REPO` (see [configuration.md](configuration.md)); the manifest body cap is `REGISTRY_MAX_MANIFEST_BYTES`.

Upload locking and the reaper are per process: two servers sharing one storage volume do not see each other's in-flight uploads and rely on file mtime versus `REGISTRY_UPLOAD_TTL` only, so run a single server per storage root. A committed blob is published by renaming the verified upload file into the content store, so no upload path keeps a link to it; its file mode is `0444`, but that mode does not restrain a server running as root (the default systemd unit), which is why the design never leaves a writable path to a committed blob instead of relying on permissions.

Manifest and tag deletion (`DELETE /v2/{repo}/manifests/{ref}`) is a separate, metadata-only operation gated by its own flag, `REGISTRY_DELETE_ENABLED`. Deleting a manifest/tag does not free the blobs it referenced — run GC afterward to reclaim that space. Do not manually delete blob files from disk outside these flows: manifests reference them by digest, and GC is the only path that safely correlates the two.
