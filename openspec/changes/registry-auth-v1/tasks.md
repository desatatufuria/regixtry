# Tasks: Registry Auth V1

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | 1300-1700 |
| 1200-line budget risk | High |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 auth foundation -> PR 2 registry enforcement -> PR 3 auth-mode TUI hardening + smoke/docs alignment |
| Delivery strategy | auto-forecast |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|---|---|---|---|
| 1 | Postgres auth domain, store, bootstrap CLI | PR 1 | Base slice; include unit/integration tests |
| 2 | `/auth/token`, bearer middleware, registry authorization | PR 2 | Depends on PR 1; keep router + service tests together |
| 3 | Auth-enabled TUI hardening, inspection-only notice, and smoke/docs alignment | PR 3 | Depends on PR 2; safe authenticated operator admin remains follow-up work |

## Phase 1: Auth Foundation

- [x] 1.1 Add `github.com/jackc/pgx/v5/stdlib` in `go.mod` and wire auth DSN/config parsing in `cmd/registry/main.go` plus `cmd/registry/main_test.go`.
- [x] 1.2 Create `internal/domain/auth/user.go`, `token.go`, `grant.go`, `principal.go`, and `errors.go` for admin flag, repo roles, TTL, and revocation semantics.
- [x] 1.3 Create `internal/ports/auth.go` and `internal/app/auth/service.go` for login, token verification, admin token CRUD, password reset, and grant management contracts.
- [x] 1.4 Create `internal/infra/auth/postgres/store.go`, `migrations.go`, and `store_test.go`; bootstrap `auth_users`, `auth_tokens`, and `auth_repo_grants` without touching SQLite schema.
- [x] 1.5 Add `bootstrap-admin` flow in `cmd/registry/main.go` so auth-enabled startup fails fast without a global admin and the bootstrap command is idempotent.

## Phase 2: Registry Enforcement

- [x] 2.1 Extend `internal/ports/registry.go` with principal-aware `Action`, `Challenge`, and authorization contracts used by registry and HTTP layers.
- [x] 2.2 Update `internal/app/registry/service.go` and `queries.go` to authorize pull/push/catalog/tag access, filter catalog results, and apply admin bypass.
- [x] 2.3 Update `internal/protocol/http/router.go` to add `/auth/token`, Basic credential exchange, bearer parsing, scoped `WWW-Authenticate` challenges, and context principal injection.
- [x] 2.4 Add auth-focused cases in `internal/app/registry/service_test.go` and `internal/protocol/http/router_test.go` for token issuance, expired/revoked bearer rejection, reader-vs-writer rules, and catalog filtering.

## Phase 3: Operator Administration

> Current shipped status: the auth-enabled TUI is intentionally inspection-only. A previous local admin shortcut was disabled before ship because it bypassed the backend auth boundary. A safe authenticated operator admin UI remains pending.

- [ ] 3.1 Deliver a safe authenticated operator admin UI for local user CRUD, password reset, enable/disable, grant editing, and token management without bypassing backend auth.
- [ ] 3.2 Add auth-enabled TUI verification coverage for the future authenticated operator admin UI once that workflow exists.

## Phase 4: Verification and Docs

- [x] 4.1 Extend `docs/verification/scripts/docker-push-pull-smoke.sh` for `docker login`, authorized pull/push, and unauthorized catalog/tag scenarios with anonymous pull disabled.
- [x] 4.2 Extend `docs/verification/scripts/tui-smoke.sh` and update `README.md` with Postgres auth bootstrap/runtime steps plus the auth-mode inspection/security-notice behavior.
