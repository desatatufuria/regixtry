# Referencia de código

| Funcionalidad | Archivo/paquete | Documentación |
| --- | --- | --- |
| Entrypoint y subcomandos | `cmd/regixtry/main.go` | `cli.md`, `installation.md` |
| Flags y defaults de serve | `cmd/regixtry/main.go:285` | `configuration.md` |
| Runtime TLS y realm | `cmd/regixtry/main.go:320` | `configuration.md`, `security.md` |
| Router y `/v2` | `internal/protocol/http/router.go` | `api.md`, `registry.md` |
| Token exchange | `internal/protocol/http/router.go:375` | `authentication.md`, `api.md` |
| Admin handlers | `internal/protocol/http/admin_handlers.go` | `authentication.md`, `users.md`, `api.md` |
| Auth workflows | `internal/app/auth/service.go` | `authentication.md`, `users.md` |
| Auth schema/persistencia | `internal/infra/auth/postgres/` | `authentication.md`, `operations.md` |
| Roles y scopes | `internal/domain/auth/`, `internal/ports/` | `authentication.md` |
| Registry workflows | `internal/app/regixtry/` | `architecture.md`, `registry.md` |
| SQLite metadata | `internal/infra/metadata/sqlite/store.go` | `architecture.md`, `operations.md` |
| Filesystem blobs | `internal/infra/storage/fsblob/store.go` | `architecture.md`, `registry.md` |
| Linux bootstrap/systemd | `internal/infra/install/linux/` | `installation.md`, `operations.md` |
| Release installer | `install.sh`, `.goreleaser.yaml` | `installation.md` |
| TUI keys/screens | `internal/tui/model.go` | `tui.md` |
| TUI admin HTTP client | `internal/tui/admin_client.go` | `tui.md`, `authentication.md` |
| Runtime image/Compose | `Dockerfile`, `docker-compose.yml` | `getting-started.md`, `ci-cd.md` |
| Tests | `**/*_test.go` | `development.md`, `documentation-audit.md` |
