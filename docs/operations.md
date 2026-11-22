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

## Upgrade and rollback

`upgrade` uses the setup provenance record, runs preflight checks, supports `--ref` and `--yes`, and shows progress. Bootstrap rollback removes generated artifacts and stops the service. Review the command output and `journalctl` before retrying.

### Upgrade note: scan history purge (irreversible)

Scan results are now latest-only: one result per (tenant, repository, digest). The first start of a version with this model runs a one-time migration (`scan_runs_latest_only_purge_v1` in `schema_backfills`) that **permanently deletes** the old duplicate scan history. Per digest it keeps the newest completed run with its findings and DB freshness (else the newest failed run, else the newest skipped row); queued and running runs are never deleted, and skipped rows are removed only for digests that have a completed or failed result. Secret-scan history is not touched.

**Back up the SQLite database (`metadata.db`, with its `-wal` file or after a clean stop) before upgrading.** There is no undo.

The purge runs in batches of at most 200 deleted runs, each in its own short write transaction, so pushes keep flowing while it runs. It is resumable: if the process stops midway, the next start continues, and the marker is written only when nothing is left. Later starts only check the marker. The file does not shrink by itself; `VACUUM` is an optional operator step to reclaim disk, run with the registry stopped because it rewrites the whole database and takes an exclusive lock.

## Cleanup

Blob garbage collection is a report-then-delete admin flow, off by default. Compute a report with `POST /admin/v1/gc/reports` — always reachable, and safe to run repeatedly since it only reads state — then review it (`GET /admin/v1/gc/reports/{id}`) before acting on it. Deletion (`POST /admin/v1/gc/reports/{id}/delete`) is gated by `REGISTRY_GC_DELETE_ENABLED` (default `false`, returns `UNSUPPORTED`/`501` while disabled); enable it only once you're ready to irreversibly unlink blob files. A 24-hour grace window and a fresh re-check at delete time protect blobs from an in-flight push, and each blob's removal is atomic with manifest publishes and upload links (a push racing a delete either publishes with all its blobs intact or is refused with a `409`, never leaving a manifest with a missing layer) — see [`docs/registry.md`](registry.md#blob-garbage-collection) for the full mark-sweep-grace mechanism.

Abandoned blob uploads are cleaned up automatically: a background reaper (runs in `serve`, every `-upload-ttl`/4 at most hourly) deletes uploads idle longer than `REGISTRY_UPLOAD_TTL` (default `24h`), both the session row and the temp file under `<storage-root>/content/uploads/`. It skips an upload with a write or commit in flight and never touches committed blobs. Clients can also cancel their own upload with `DELETE /v2/<name>/blobs/uploads/<id>`. To bound disk use further, tune `REGISTRY_MAX_UPLOAD_BYTES`, `REGISTRY_MAX_OPEN_UPLOADS` and `REGISTRY_MAX_OPEN_UPLOADS_PER_REPO` (see [configuration.md](configuration.md)); the manifest body cap is `REGISTRY_MAX_MANIFEST_BYTES`.

Upload locking and the reaper are per process: two servers sharing one storage volume do not see each other's in-flight uploads and rely on file mtime versus `REGISTRY_UPLOAD_TTL` only, so run a single server per storage root. A committed blob is published by renaming the verified upload file into the content store, so no upload path keeps a link to it; its file mode is `0444`, but that mode does not restrain a server running as root (the default systemd unit), which is why the design never leaves a writable path to a committed blob instead of relying on permissions.

Manifest and tag deletion (`DELETE /v2/{repo}/manifests/{ref}`) is a separate, metadata-only operation gated by its own flag, `REGISTRY_DELETE_ENABLED`. Deleting a manifest/tag does not free the blobs it referenced — run GC afterward to reclaim that space. Do not manually delete blob files from disk outside these flows: manifests reference them by digest, and GC is the only path that safely correlates the two.
