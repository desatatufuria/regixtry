# CLI Reference

## Comandos

| Comando | Flags principales | Uso |
| --- | --- | --- |
| `serve` | ver `configuration.md` | Inicia HTTP/HTTPS |
| `tui` | `-storage-root`, `-db`, `-tenant`, `-auth-postgres-dsn`, `-api-base-url`, `-snapshot` | Ejecuta consola |
| `bootstrap-admin` | `-auth-postgres-dsn`, `-username`, `-password`, `-password-stdin`, `-rotate-password` | Crea/rota admin inicial |
| `bootstrap` | `-mode`, `-public-url`, `-runtime-tls-mode`, `-tls-cert-file`, `-tls-key-file`, `-addr`, `-storage-root`, `-state-path`, `-unit-path`, `-service`, `-no-start`, `-rollback` | Genera setup systemd |
| `setup` | flags de `bootstrap` más `-admin-username`, `-admin-password`, `-auth-postgres-dsn`, legacy `-trivy-*` import bridge | Setup interactivo/automatizado |
| `feature list` | `-storage-root`, `-db`, `-tenant` | Lists built-in features |
| `feature show <name>` | `-storage-root`, `-db`, `-tenant` | Shows feature-owned configuration |
| `feature status <name>` | `-storage-root`, `-db`, `-tenant` | Shows feature state plus runtime health |
| `feature enable <name>` / `disable <name>` | `-storage-root`, `-db`, `-tenant` | Toggles a built-in feature |
| `feature configure <name>` | `-storage-root`, `-db`, `-tenant`, `-enabled`, `-schedule-enabled`, `-interval`, `-timeout`, `-service-url`, `-registry-reachable-url`, optional `-auth-token`, `-tls-ca-cert-path`, `-tls-insecure-skip-verify`, `-max-concurrency` | Updates feature-owned configuration |
| `uninstall` | `-state-path` | Elimina artefactos registrados |
| `upgrade` | `-ref`, `-state-path`, `-yes` | Upgrade con preflight |

No hay subcomandos Cobra/urfave; se usa `flag.NewFlagSet` de la biblioteca estándar.

Ejemplo de snapshot:

```bash
```
