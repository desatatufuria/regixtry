# Arquitectura

## Resumen

El proceso es un binario Go único. `serve` construye router HTTP, servicio de registry, controlador de acceso, blob store filesystem y metadata store SQLite. Si se entrega un DSN PostgreSQL, también construye el servicio de auth y exige que exista un administrador global activo antes de servir.

```mermaid
flowchart LR
    Client[Docker / OCI client] --> HTTP[HTTP router /v2]
    Operator[CLI / TUI] --> AuthAPI[/auth/token y /admin/v1]
    HTTP --> Access[AccessController]
    AuthAPI --> Auth[Auth service]
    Auth --> PG[(PostgreSQL auth)]
    HTTP --> Registry[Application registry service]
    Registry --> Meta[(SQLite metadata)]
    Registry --> Blobs[(Filesystem blobs/uploads)]
```

## Capas

| Capa | Responsabilidad | Implementación |
| --- | --- | --- |
| Dominio | digest, repository, manifest, upload, usuarios, grants y tokens | `internal/domain/` |
| Aplicación | workflows de push/pull, consultas y auth | `internal/app/` |
| Ports | interfaces de storage, auth, access, tenant y jobs | `internal/ports/` |
| Infraestructura | SQLite, filesystem, PostgreSQL, lifecycle Linux y releases | `internal/infra/` |
| Protocolo | routing HTTP, challenges y serialización | `internal/protocol/http/` |
| TUI | cliente de inspección y admin autenticado | `internal/tui/` |

## Persistencia

Los bytes viven en `<storage-root>/content/blobs/sha256/<digest-hex>`. Los uploads temporales viven en `<storage-root>/content/uploads/<id>/data` junto con `state.json`. SQLite mantiene repositories, manifests, tags, relaciones manifest/blob y uploads. PostgreSQL mantiene usuarios, grants y tokens de auth.

El job runner actual es inline; no existe worker persistente ni cola asíncrona.

## Flujos

Push: iniciar upload, añadir chunks, calcular SHA-256, mover el archivo a blobs solo si coincide el digest, y publicar el manifest solo si los blobs referenciados existen.

Pull: resolver manifest por tag o digest, leer sus bytes desde SQLite y blobs desde filesystem.

Auth: challenge Bearer, intercambio Basic en `/auth/token`, emisión de bearer de 15 minutos, verificación contra PostgreSQL y autorización por grants/scopes.

Trazabilidad: `cmd/regixtry/main.go`, `internal/app/regixtry/`, `internal/protocol/http/`, `internal/infra/storage/fsblob/`, `internal/infra/metadata/sqlite/`.
