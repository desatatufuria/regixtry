# Apply Progress: registry-auth-v1

## Change

- Name: `registry-auth-v1`
- Mode: Standard
- Delivery strategy: `auto-forecast`
- Chain strategy: `stacked-to-main`
- Current work unit: `Post-ship cleanup for auth-enabled TUI shipped scope`

## Completed Tasks

- [x] 1.1 Add `github.com/jackc/pgx/v5/stdlib` in `go.mod` and wire auth DSN/config parsing in `cmd/registry/main.go` plus `cmd/registry/main_test.go`.
- [x] 1.2 Create `internal/domain/auth/user.go`, `token.go`, `grant.go`, `principal.go`, and `errors.go` for admin flag, repo roles, TTL, and revocation semantics.
- [x] 1.3 Create `internal/ports/auth.go` and `internal/app/auth/service.go` for login, token verification, admin token CRUD, password reset, and grant management contracts.
- [x] 1.4 Create `internal/infra/auth/postgres/store.go`, `migrations.go`, and `store_test.go`; bootstrap `auth_users`, `auth_tokens`, and `auth_repo_grants` without touching SQLite schema.
- [x] 1.5 Add `bootstrap-admin` flow in `cmd/registry/main.go` so auth-enabled startup fails fast without a global admin and the bootstrap command is idempotent.
- [x] 2.1 Extend `internal/ports/registry.go` with principal-aware `Action`, `Challenge`, and authorization contracts used by registry and HTTP layers.
- [x] 2.2 Update `internal/app/registry/service.go` and `queries.go` to authorize pull/push/catalog/tag access, filter catalog results, and apply admin bypass.
- [x] 2.3 Update `internal/protocol/http/router.go` to add `/auth/token`, Basic credential exchange, bearer parsing, scoped `WWW-Authenticate` challenges, and context principal injection.
- [x] 2.4 Add auth-focused cases in `internal/app/registry/service_test.go` and `internal/protocol/http/router_test.go` for token issuance, expired/revoked bearer rejection, reader-vs-writer rules, and catalog filtering.
- [x] 4.1 Extend `docs/verification/scripts/docker-push-pull-smoke.sh` for `docker login`, authorized pull/push, and unauthorized catalog/tag scenarios with anonymous pull disabled.
- [x] 4.2 Extend `docs/verification/scripts/tui-smoke.sh` and update `README.md` with Postgres auth bootstrap/runtime steps plus the auth-mode inspection/security-notice behavior.

## Files Changed

| File | Action | Notes |
|---|---|---|
| `cmd/registry/main.go` | Modified | Added auth DSN flags, `bootstrap-admin` subcommand, and auth-enabled startup guard. |
| `cmd/registry/main_test.go` | Modified | Covered auth DSN parsing, bootstrap idempotence, and fail-fast startup. |
| `go.mod`, `go.sum` | Modified | Added `pgx/v5`, `google/uuid`, and `x/crypto` dependencies. |
| `internal/domain/auth/*.go` | Created | Added auth domain entities, repo roles, principal model, TTL helpers, and typed errors. |
| `internal/ports/auth.go` | Created | Defined auth store and service contracts for login, verification, grants, and admin token workflows. |
| `internal/app/auth/service.go` | Created | Implemented bootstrap admin, password login, preissued-token login, access-token verification, and admin-only grant/token operations. |
| `internal/infra/auth/postgres/migrations.go` | Created | Bootstraps only auth tables. |
| `internal/infra/auth/postgres/store.go` | Created | Added SQL-backed auth persistence over `pgx` with portable schema statements. |
| `internal/infra/auth/postgres/store_test.go` | Created | Validated auth table bootstrap and persisted user/token/grant lifecycle without touching registry metadata tables. |
| `internal/ports/registry.go` | Modified | Added principal-aware actions, scoped challenges, and context principal helpers. |
| `internal/ports/defaults.go` | Modified | Added scoped challenge generation and principal-aware access controller for auth-enabled registry requests. |
| `internal/app/registry/service.go` | Modified | Attached principals from context before authorization and exposed scoped challenges. |
| `internal/app/registry/queries.go` | Modified | Filtered catalog responses by readable grants while preserving admin bypass. |
| `internal/protocol/http/router.go` | Modified | Added `/auth/token`, bearer verification, principal injection, and scoped `WWW-Authenticate` handling. |
| `internal/app/registry/service_test.go` | Modified | Covered reader-vs-writer enforcement, catalog filtering, and admin bypass. |
| `internal/protocol/http/router_test.go` | Modified | Covered token issuance plus expired/revoked bearer rejection and challenge scopes. |
| `cmd/registry/main.go` | Modified | Removed the local fabricated admin shortcut; auth-enabled TUI now stays inspection-only and shows a security notice. |
| `cmd/registry/main_test.go` | Modified | Updated auth-enabled `/v2/` ping expectation to the current Bearer challenge behavior. |
| `internal/ports/auth.go` | Modified | Added user-list/delete and grant-list contracts needed by the TUI admin slice. |
| `internal/app/auth/service.go` | Modified | Added admin-only user list/create/update/enable-disable/delete flows plus grant listing. |
| `internal/infra/auth/postgres/store.go` | Modified | Added auth user listing and deletion persistence to support TUI CRUD. |
| `internal/tui/model.go` | Modified | Preserved inspection flows and added the auth-mode security notice for the shipped behavior. |
| `internal/tui/model.go` | Modified | Removed dormant auth-admin navigation so the shipped TUI stays inspection-only with the existing notice. |
| `internal/tui/admin_users.go` | Deleted | Removed deferred user-admin workflow code that was not reachable in shipped auth-enabled mode. |
| `internal/tui/admin_grants.go` | Deleted | Removed deferred grant-management workflow code that was not reachable in shipped auth-enabled mode. |
| `internal/tui/admin_tokens.go` | Deleted | Removed deferred token-management workflow code that was not reachable in shipped auth-enabled mode. |
| `internal/tui/model_test.go` | Modified | Dropped unreachable admin-workflow tests and kept inspection/mutation-notice coverage only. |
| `docs/verification/scripts/docker-push-pull-smoke.sh` | Modified | Added auth-enabled bootstrap, docker login, and anonymous challenge checks. |
| `docs/verification/scripts/tui-smoke.sh` | Modified | Added auth-enabled bootstrap flow and verification for the inspection snapshot plus security notice. |
| `README.md` | Modified | Documented compose bootstrap/runtime flow and clarified that auth-enabled TUI admin mutations are disabled. |

## Verification

- Command: `GOSUMDB=off go mod tidy`
- Result: PASS
- Command: `GOSUMDB=off go test ./...`
- Result: PASS
- Command: `go test ./internal/ports ./internal/app/registry ./internal/protocol/http ./cmd/registry`
- Result: PASS
- Command: `go test ./...`
- Result: PASS
- Command: `go test ./internal/tui ./internal/app/auth ./internal/infra/auth/postgres ./internal/protocol/http ./cmd/registry`
- Result: PASS
- Command: `go test ./internal/tui`
- Result: PASS
- Command: `go test ./internal/tui ./cmd/registry`
- Result: PASS
- Command: `go test ./...`
- Result: PASS

## Deviations

- Post-review security hardening: auth-backed local TUI administration is intentionally disabled until a real operator login flow exists. This narrows the originally planned Work Unit 3 scope because the prior local admin shortcut violated the auth boundary.
- Artifact alignment: OpenSpec tasks/spec/verify artifacts now describe the shipped inspection-only TUI behavior instead of claiming local admin mutation workflows are delivered.
- Dormant-code cleanup: removed deferred `internal/tui/admin_*.go` files and related unreachable tests so shipped code now matches the narrowed auth-enabled TUI scope.

## Remaining Tasks

- 3.1 Deliver a safe authenticated operator admin UI for local user CRUD, password reset, enable/disable, grant editing, and token management without bypassing backend auth.
- 3.2 Add auth-enabled TUI verification coverage for the future authenticated operator admin UI once that workflow exists.

## Workload / PR Boundary

- Mode: stacked PR slice
- Boundary: shipped auth-enabled TUI inspection mode, security notice behavior, and dormant-code cleanup; safe operator admin UI remains outside the current shipped slice
- Review budget impact: focused cleanup under the shipped TUI slice; registry auth runtime behavior remains unchanged

## Status

- 11/13 shipped tasks complete overall
- Auth-enabled TUI admin UI deferred pending a real operator login flow
- Next recommended phase: continue with `sdd-verify`

## Post-merge Security Fixes

- Removed the fabricated `IsAdmin: true` local TUI principal from `cmd/registry/main.go`.
- Kept auth-enabled TUI repository inspection available, but replaced the old admin shortcut with an explicit security notice.
- Updated `cmd/registry/main_test.go`, `README.md`, and `docs/verification/scripts/tui-smoke.sh` to lock the secure behavior in place.
- Updated the OpenSpec spec/tasks/verify artifacts so they describe the shipped inspection-only TUI behavior instead of claiming local admin workflows are delivered.
- Verification: `go test ./cmd/registry ./internal/tui` and `go test ./...` both PASS after the fix.
- Bound `/auth/token` requested repository scopes to the issued access token by intersecting requested actions with the authenticated user's actual repository grants before persisting the token.
- Enforced stored access-token scopes during `/v2/*` authorization and catalog filtering so token-restricted pull access can no longer be upgraded into push or broader repository visibility.
- Added malformed-scope rejection plus persistence/authorization tests in `internal/app/auth/service_test.go`, `internal/ports/defaults_test.go`, `internal/protocol/http/router_test.go`, and `internal/infra/auth/postgres/store_test.go`.
- Verification: `go test ./internal/ports ./internal/app/registry ./internal/protocol/http ./internal/app/auth ./internal/infra/auth/postgres ./internal/tui` and `go test ./...` both PASS after the scope-binding fix.
- Removed dormant `internal/tui/admin_*.go` files plus unreachable admin-workflow tests so the shipped branch now contains only the inspection-only auth-enabled TUI behavior.
- Verification: `go test ./internal/tui ./cmd/registry` and `go test ./...` both PASS after the cleanup.
