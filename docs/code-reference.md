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
| Keyless (Fulcio/OIDC) offline bundle verification against a pinned trusted root (`VerifyKeyless`) | `internal/domain/signing/keyless.go` | `security.md`, `features.md` |
| Linux bootstrap/systemd | `internal/infra/install/linux/` | `installation.md`, `operations.md` |
| Release installer | `install.sh`, `.goreleaser.yaml` | `installation.md` |
| TUI root model: key and mouse routing, login, navigation | `internal/tui/model.go`, `model_console.go`, `model_delegate.go` | `tui.md` |
| TUI admin HTTP client and the `AdminClient` interface | `internal/tui/admin_client.go` | `tui.md`, `authentication.md` |
| TUI per-screen contract (`adminScreen`, `screenEnv`, `screenFrame`, `mouseScreen`, `slotFor`, `navigate`) | `internal/tui/screen.go` | `architecture.md` |
| TUI router (`routeAdminKey`, `routeAdminMsg`) | `internal/tui/admin_router.go` | `architecture.md` |
| TUI session (`AdminSession`) and modal field state | `internal/tui/session.go` | `tui.md` |
| TUI application shell: header, sidebar (`shellNavGroups`), footer (`renderShell`) | `internal/tui/shell.go` | `tui.md`, `architecture.md` |
| TUI theme and palette (`palette*Hex`) | `internal/tui/admin_theme.go` | `tui.md` |
| TUI keymap-derived help (`screenKeys`, `shortHelpView`) and key helpers | `internal/tui/admin_keys.go`, `keyinput.go` | `architecture.md`, `tui.md` |
| TUI mouse router, `F2` switch and list helpers (`listMouse`) | `internal/tui/mouse.go`, `screen_*_mouse.go` | `tui.md` |
| TUI confirm primitive and dialog (`confirmPrompt`) | `internal/tui/confirm.go` | `architecture.md`, `tui.md` |
| TUI too-small notice (150x24 minimum) | `internal/tui/screen_too_small.go`, `viewport.go` | `tui.md` |
| TUI token lifetime selector (presets, Custom input, ceiling `MaxAdminTokenTTL`) | `internal/tui/token_lifetime.go` | `tui.md`, `users.md` |
| TUI shared Access helpers (time left text and bars, one-time secrets) | `internal/tui/access_common.go`, `access_secrets.go` | `tui.md` |
| TUI Overview (`overviewScreen`: needs attention, recent changes) | `internal/tui/screen_overview*.go` | `tui.md` |
| TUI login screen rendering | `internal/tui/screen_login_view.go` | `tui.md` |
| TUI Users, profile, New user, Reset password | `internal/tui/screen_users*.go`, `screen_edit_user*.go`, `screen_create_user*.go`, `screen_reset_password*.go`, `user_format.go` | `tui.md` |
| TUI Robots, New robot, Tokens | `internal/tui/screen_robots*.go`, `screen_create_robot*.go`, `screen_tokens*.go` | `tui.md` |
| TUI Grant repository access (`grantAccessScreen`) | `internal/tui/screen_grant_access*.go` | `tui.md` |
| TUI Repository access (`repoAccessScreen`, paged `repository-grants`) | `internal/tui/screen_repo_access*.go` | `tui.md` |
| TUI delegate screen for repository administrators (`delegateScreen`) | `internal/tui/screen_delegate*.go`, `model_delegate.go` | `tui.md`, `users.md` |
| TUI Console: Repositories, Projects, Tags and their deletes | `internal/tui/screen_console_*.go`, `sort_mode.go` | `tui.md` |
| TUI security feature screens: Image scanning, Secret scanning, Signing policy | `internal/tui/screen_trivy_*.go`, `screen_gitleaks_*.go`, `screen_signing_*.go`, `security_*.go` | `tui.md` |
| TUI per-repository override lists and editor (`overrideEditor`, `featureOverridesScreen`) | `internal/tui/override_editor*.go`, `overrides_*.go`, `screen_gitleaks_repos.go`, `screen_signing_repos.go` | `tui.md`, `architecture.md` |
| TUI trusted-key and trusted-identity lists (Signing) | `internal/tui/trusted_key_list.go`, `trusted_identity_list.go` | `tui.md`, `features.md` |
| TUI Scan runs, Secret findings and scan history (`scanRunsScreen`, `scanHistoryScreen`) | `internal/tui/screen_scan_*.go`, `admin_scan_history.go`, `admin_tables.go` | `tui.md`, `security.md` |
| TUI Update channel (`updateChannelScreen`) | `internal/tui/screen_update_channel*.go` | `tui.md` |
| TUI admin workspace frame and status line (`renderAdminWorkspace`) | `internal/tui/admin_views.go` | `tui.md` |
| TUI overlay compositing for dialogs over the Console (`compositeOverlay`) | `internal/tui/admin_overlay.go` | `architecture.md` |
| TUI open-URL helper (`openURLInBrowser`, `isHTTPURL`) | `internal/tui/openurl.go` | `tui.md`, `security.md` |
| Runtime image/Compose | `Dockerfile`, `docker-compose.yml` | `getting-started.md`, `ci-cd.md` |
| Tests | `**/*_test.go` | `development.md`, `documentation-audit.md` |

## Notes on this table

- Line-number references are point-in-time and drift as the code changes; treat the function/struct name in parentheses as the more durable anchor, and re-verify the exact line before relying on it precisely.
- `internal/tui/*.go` above lists the meaningful non-test source files as of this revision (confirmed via `ls internal/tui/*.go`); test files (`*_test.go`) are intentionally omitted.
