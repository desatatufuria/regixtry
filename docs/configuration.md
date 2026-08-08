# Configuración

## `serve`

| Flag | ENV inicial | Default | Descripción |
| --- | --- | --- | --- |
| `-addr` | | `127.0.0.1:5000` | Dirección TCP de escucha |
| `-public-url` | `REGISTRY_PUBLIC_URL` | vacío | URL absoluta HTTP/HTTPS anunciada a clientes; obligatoria en `serve` |
| `-tls-cert-file` | `REGISTRY_TLS_CERT_FILE` | vacío | PEM de certificado |
| `-tls-key-file` | `REGISTRY_TLS_KEY_FILE` | vacío | PEM de clave privada |
| `-storage-root` | | `./data` | Raíz de almacenamiento |
| `-db` | | `<storage-root>/metadata.db` | SQLite de metadata |
| `-tenant` | | `default` | Identificador del único tenant activo |
| `-allow-anonymous-pull` | | `false` | Permite pulls e inspección sin principal |
| `-allow-anonymous-push` | | `false` | Permite writes sin principal |
| `-auth-postgres-dsn` | `REGISTRY_AUTH_POSTGRES_DSN` | vacío | DSN PostgreSQL para auth |
| `-auth-token-realm` | `REGISTRY_AUTH_TOKEN_REALM_URL` | derivado | Debe coincidir con `<public-url>/auth/token` |
| `-realm` | | `regixtry` | Realm del challenge |
| `-service` | | `regixtry` | Service del challenge |
| `-read-header-timeout` | | `5s` | Timeout de headers |
| `-read-timeout` | | `60s` | Timeout de request |
| `-write-timeout` | | `30s` | Timeout de response |
| `-idle-timeout` | | `120s` | Keep-alive idle |
| `-shutdown-timeout` | | `10s` | Graceful shutdown |

Implementación: `cmd/regixtry/main.go` (`parseServeConfig`, `normalizeRuntimeConfig`).

`-public-url` debe ser absoluta, usar HTTP o HTTPS y no contener query ni fragment. Certificado y clave deben proporcionarse juntos. Una URL pública `http` no puede combinarse con TLS directo. Una URL `https` puede representar TLS directo o reverse proxy.

## TUI

| Flag | ENV/default | Descripción |
| --- | --- | --- |
| `-storage-root` | `./data` o valor detectado del setup | Raíz de datos |
| `-db` | `<storage-root>/metadata.db` | SQLite |
| `-tenant` | `default` | Tenant |
| `-auth-postgres-dsn` | `REGISTRY_AUTH_POSTGRES_DSN` | DSN auth |
| `-api-base-url` | `REGISTRY_API_BASE_URL` | Base URL absoluta del admin API |
| `-snapshot` | `false` | Renderiza la primera vista y termina |

Una instalación gestionada puede autodetectar valores desde `/etc/regixtry/regixtry.env`; flags explícitos tienen prioridad.

## Setup-managed environment

El setup escribe `REGISTRY_ADDR`, `REGISTRY_PUBLIC_URL`, `REGISTRY_STORAGE_ROOT`, `REGISTRY_DATABASE_PATH`, `REGISTRY_SERVICE_NAME` y, cuando corresponda, `REGISTRY_AUTH_POSTGRES_DSN`, `REGISTRY_TLS_CERT_FILE` y `REGISTRY_TLS_KEY_FILE`. Estos valores se interpretan en `internal/infra/install/linux/templates.go`.

## Otros subcomandos

Consultar [cli.md](cli.md) para flags de `bootstrap`, `bootstrap-admin`, `setup`, `uninstall` y `upgrade`.
