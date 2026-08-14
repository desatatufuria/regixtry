# CLI Reference

## Commands

| Command | Main flags | Use |
| --- | --- | --- |
| `serve` | see `configuration.md` | Starts the HTTP/HTTPS registry runtime |
| `tui` | `-storage-root`, `-db`, `-tenant`, `-auth-postgres-dsn`, `-api-base-url`, `-snapshot` | Runs the local console |
| `bootstrap-admin` | `-auth-postgres-dsn`, `-username`, `-password`, `-password-stdin`, `-rotate-password` | Creates or rotates the bootstrap admin |
| `bootstrap` | `-mode`, `-public-url`, `-runtime-tls-mode`, `-tls-cert-file`, `-tls-key-file`, `-addr`, `-storage-root`, `-state-path`, `-unit-path`, `-service`, `-no-start`, `-rollback` | Builds the systemd-oriented base setup |
| `setup` | `bootstrap` flags plus `-admin-username`, `-admin-password`, `-auth-postgres-dsn`, and legacy `-trivy-*` import bridge flags | Runs guided or automated setup |
| `feature list` | `-storage-root`, `-db`, `-tenant` | Lists built-in features in a fixed-width operator table with current/latest/update columns |
| `feature show <name>` | `-storage-root`, `-db`, `-tenant` | Shows feature-owned configuration |
| `feature status <name>` | `-storage-root`, `-db`, `-tenant` | Shows feature intent plus managed runtime status, latest-version awareness, and update state |
| `feature install <name>` / `upgrade <name>` | `-storage-root`, `-db`, `-tenant`, optional `-version` | Installs or upgrades the managed Trivy runtime with staged progress (`resolve`, `download`, `verify`, `extract`, `activate`, `probe`, `complete`) |
| `feature rollback <name>` | `-storage-root`, `-db`, `-tenant` | Restores the previous managed Trivy runtime |
| `feature enable <name>` / `disable <name>` | `-storage-root`, `-db`, `-tenant` | Toggles a built-in feature |
| `feature configure <name>` | `-storage-root`, `-db`, `-tenant`, `-enabled`, `-schedule-enabled`, `-interval`, `-timeout`, `-service-url`, `-registry-reachable-url`, `-auth-token`, `-tls-ca-cert-path`, `-tls-insecure-skip-verify`, `-max-concurrency` | Updates feature-owned intent without taking runtime ownership |
| `uninstall` | `-state-path` | Removes recorded base-install artifacts |
| `upgrade` | `-ref`, `-state-path`, `-yes` | Runs the Regixtry binary upgrade preflight |

Regixtry still uses the Go standard library `flag.NewFlagSet`; there is no Cobra or urfave layer.

Managed Trivy example:

```bash
regixtry feature configure trivy \
  -enabled \
  -schedule-enabled \
  -interval 6h \
  -timeout 20m \
  -registry-reachable-url https://registry.internal:5443 \
  -max-concurrency 2

regixtry feature install trivy -version 0.57.1
regixtry feature status trivy
regixtry feature upgrade trivy -version 0.58.0
regixtry feature rollback trivy
```

`feature list` now prints `NAME`, `KIND`, `ENABLED`, `CONFIGURED`, `CURRENT`, `LATEST`, and `UPDATE` columns. `feature status` now includes `Runtime Latest Version` and `Runtime Update Status`. Latest resolution is request-scoped: if the lookup fails, the read still succeeds and reports `unknown`.
