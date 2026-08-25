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

## Cleanup

Blob garbage collection is a report-then-delete admin flow, off by default. Compute a report with `POST /admin/v1/gc/reports` — always reachable, and safe to run repeatedly since it only reads state — then review it (`GET /admin/v1/gc/reports/{id}`) before acting on it. Deletion (`POST /admin/v1/gc/reports/{id}/delete`) is gated by `REGISTRY_GC_DELETE_ENABLED` (default `false`, returns `UNSUPPORTED`/`501` while disabled); enable it only once you're ready to irreversibly unlink blob files. A 24-hour grace window and a fresh re-check at delete time protect blobs from an in-flight push — see [`docs/registry.md`](docs/registry.md#blob-garbage-collection) for the full mark-sweep-grace mechanism.

Manifest and tag deletion (`DELETE /v2/{repo}/manifests/{ref}`) is a separate, metadata-only operation gated by its own flag, `REGISTRY_DELETE_ENABLED`. Deleting a manifest/tag does not free the blobs it referenced — run GC afterward to reclaim that space. Do not manually delete blob files from disk outside these flows: manifests reference them by digest, and GC is the only path that safely correlates the two.
