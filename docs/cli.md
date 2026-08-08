# CLI Reference

## Comandos

| Comando | Flags principales | Uso |
| --- | --- | --- |
| `serve` | ver `configuration.md` | Inicia HTTP/HTTPS |
| `tui` | `-storage-root`, `-db`, `-tenant`, `-auth-postgres-dsn`, `-api-base-url`, `-snapshot` | Ejecuta consola |
| `bootstrap-admin` | `-auth-postgres-dsn`, `-username`, `-password`, `-password-stdin`, `-rotate-password` | Crea/rota admin inicial |
| `bootstrap` | `-mode`, `-public-url`, `-runtime-tls-mode`, `-tls-cert-file`, `-tls-key-file`, `-addr`, `-storage-root`, `-state-path`, `-unit-path`, `-service`, `-no-start`, `-rollback` | Genera setup systemd |
| `setup` | flags de `bootstrap` más `-admin-username`, `-admin-password`, `-auth-postgres-dsn` | Setup interactivo/automatizado |
| `uninstall` | `-state-path` | Elimina artefactos registrados |
| `upgrade` | `-ref`, `-state-path`, `-yes` | Upgrade con preflight |

No hay subcomandos Cobra/urfave; se usa `flag.NewFlagSet` de la biblioteca estándar.

Ejemplo de snapshot:

```bash
```
