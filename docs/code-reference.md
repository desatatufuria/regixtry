# Code reference

| Functionality | File/package | Documentation |
| --- | --- | --- |
| Entrypoint and subcommands | `cmd/regixtry/main.go` | `cli.md`, `installation.md` |
| `serve` flags and defaults (`parseServeConfig`) | `cmd/regixtry/main.go:336-365` | `configuration.md` |
| Runtime TLS and token realm derivation (`normalizeRuntimeConfig`) | `cmd/regixtry/main.go:381-428` | `configuration.md`, `security.md` |
| Router and `/v2` | `internal/protocol/http/router.go` | `api.md`, `registry.md` |
| Token exchange (`handleToken`) | `internal/protocol/http/router.go:447` | `authentication.md`, `api.md` |
| Admin handlers | `internal/protocol/http/admin_handlers.go` | `authentication.md`, `users.md`, `api.md` |
| Auth workflows | `internal/app/auth/service.go` | `authentication.md`, `users.md` |
| Auth schema/persistence | `internal/infra/auth/postgres/` | `authentication.md`, `operations.md` |
| Roles and scopes | `internal/domain/auth/`, `internal/ports/` | `authentication.md` |
| Registry workflows | `internal/app/regixtry/` | `architecture.md`, `registry.md` |
| SQLite metadata | `internal/infra/metadata/sqlite/store.go` | `architecture.md`, `operations.md` |
| Filesystem blobs | `internal/infra/storage/fsblob/store.go` | `architecture.md`, `registry.md` |
| Scanning application seam (Trivy/Gitleaks scheduling) | `internal/app/scanning/` | `security.md`, `roadmap.md` |
| Trivy scanning runtime (release fetch, run, runtime manager) | `internal/infra/scanning/trivy/` | `security.md` |
| Gitleaks secret-scanning runtime (release fetch, run, report parsing, runtime manager) | `internal/infra/scanning/gitleaks/` | `security.md` |
| Cosign signature parsing, verification, and signing policy | `internal/domain/signing/` | `security.md` |
| Linux bootstrap/systemd | `internal/infra/install/linux/` | `installation.md`, `operations.md` |
| Release installer | `install.sh`, `.goreleaser.yaml` | `installation.md` |
| TUI keys/screens (root model) | `internal/tui/model.go` | `tui.md` |
| TUI admin HTTP client | `internal/tui/admin_client.go` | `tui.md`, `authentication.md` |
| TUI admin views (screens for users, grants, robots, features, signing policy) | `internal/tui/admin_views.go` | `tui.md` |
| TUI admin table rendering helpers | `internal/tui/admin_tables.go` | `tui.md` |
| TUI Console Repositories table (Name/Tags/Last Pushed) | `internal/tui/console_repositories_table.go` | `tui.md` |
| TUI Console Tags table (Tag/Created/Signed) | `internal/tui/console_tags_table.go` | `tui.md` |
| TUI session state | `internal/tui/session.go` | `tui.md` |
| TUI admin theming | `internal/tui/admin_theme.go` | `tui.md` |
| TUI admin overlay/modal rendering | `internal/tui/admin_overlay.go` | `tui.md` |
| TUI scan history views | `internal/tui/admin_scan_history.go` | `tui.md`, `security.md` |
| TUI scroll viewport helper | `internal/tui/viewport.go` | `tui.md` |
| TUI open-URL helper | `internal/tui/openurl.go` | `tui.md` |
| Runtime image/Compose | `Dockerfile`, `docker-compose.yml` | `getting-started.md`, `ci-cd.md` |
| Tests | `**/*_test.go` | `development.md`, `documentation-audit.md` |

## Notes on this table

- Line-number references are point-in-time and drift as the code changes; treat the function/struct name in parentheses as the more durable anchor, and re-verify the exact line before relying on it precisely.
- `internal/tui/*.go` above lists the meaningful non-test source files as of this revision (confirmed via `ls internal/tui/*.go`); test files (`*_test.go`) are intentionally omitted.
