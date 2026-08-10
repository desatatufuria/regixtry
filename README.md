# Regixtry

Regixtry es un registry de imágenes de bajo consumo, distribuido como un binario Go único. Expone una superficie HTTP compatible con los flujos Docker Registry/OCI implementados por este repositorio, guarda los blobs en el filesystem y la metadata en SQLite. La autenticación opcional usa PostgreSQL.

No es un reemplazo completo de Harbor: la versión actual es single-tenant, local y deliberadamente limitada.

## Quick Start local

Requisitos: Go `1.26.0` o Docker/Compose.

```bash
go run ./cmd/regixtry serve \
  -addr 127.0.0.1:5000 \
  -public-url http://127.0.0.1:5000 \
  -storage-root ./data
```

Comprobar el registry:

```bash
curl -i http://127.0.0.1:5000/v2/
```

Para autenticación y Compose, consultar [`docs/getting-started.md`](docs/getting-started.md).

## Capacidades confirmadas

- Push y pull de manifests y blobs mediante la superficie `/v2/` implementada.
- Listado de catálogo y tags con paginación `n`/`last`.
- Uploads por `POST`, `PATCH` y `PUT`, con validación SHA-256 antes de publicar el blob.
- Metadata de registry en SQLite y contenido en filesystem.
- Usuarios, grants por repositorio y tokens en PostgreSQL cuando se configura `-auth-postgres-dsn`.
- Challenge Bearer y emisión de tokens en `/auth/token`.
- API administrativa autenticada bajo `/admin/v1`.
- CLI de servicio, setup, bootstrap, uninstall, upgrade y TUI Bubble Tea.
- Instalador Linux de releases con SHA-256 para `amd64` y `arm64`.

## Instalación

```bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash
regixtry setup
```

El script instala el binario; `regixtry setup` gestiona el runtime Linux + systemd. Para procedimientos completos, consultar [`docs/installation.md`](docs/installation.md).

### Optional Trivy rescans

Regixtry now ships an optional Trivy rescan slice for already-published images. The setup/bootstrap lifecycle writes disabled-by-default runtime settings into the managed env file and lifecycle provenance.

Example setup flags:

```bash
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url https://registry.example.com \
  --runtime-tls-mode reverse-proxy \
  --trivy-enabled \
  --trivy-schedule-enabled \
  --trivy-interval 6h \
  --trivy-timeout 20m \
  --trivy-cache-dir /var/lib/regixtry/trivy-cache \
  --trivy-max-concurrency 2
```

Admin API endpoints for this slice:

- `GET/PUT /admin/v1/scan-settings`
- `POST /admin/v1/scan-runs`
- `GET /admin/v1/scan-runs?repository=&limit=`

The richer TUI management flow for scan settings/history remains deferred.

## Documentación

- [Inicio rápido](docs/getting-started.md)
- [Instalación](docs/installation.md)
- [Configuración](docs/configuration.md)
- [Arquitectura](docs/architecture.md)
- [Autenticación y usuarios](docs/authentication.md)
- [API HTTP](docs/api.md)
- [Registry y Docker/OCI](docs/registry.md)
- [CLI](docs/cli.md) y [TUI](docs/tui.md)
- [CI/CD](docs/ci-cd.md)
- [Operación](docs/operations.md)
- [Seguridad](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Desarrollo](docs/development.md)
- [Referencia de código](docs/code-reference.md)
- [Auditoría de documentación](docs/documentation-audit.md)

## Límites actuales

- Solo existe un resolver de tenant single-tenant; el tenant por defecto es `default`.
- No se confirmó almacenamiento remoto, replicación, multi-tenant, métricas ni health endpoint dedicado.
- No hay endpoint de garbage collection ni API de borrado de manifests/blobs.
- `DELETE` de un upload devuelve `UNSUPPORTED`.
- La TUI inspecciona el registry y permite una parte de la administración autenticada, pero no implementa todas las mutaciones de `/admin/v1`.
- ⚠️ No se ha podido confirmar a partir del código actual una certificación de compatibilidad completa con OCI Distribution Specification o multi-arch.

El alcance detallado y las ausencias verificadas están en [`docs/documentation-audit.md`](docs/documentation-audit.md).
