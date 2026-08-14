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

There is no garbage collector or retention policy implemented. Do not manually delete blobs without correlating metadata: manifests reference them by digest.
