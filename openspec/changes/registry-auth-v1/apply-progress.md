# Apply Progress: registry-auth-v1

## Change

- Name: `registry-auth-v1`
- Mode: Standard
- Delivery strategy: `auto-forecast`
- Chain strategy: `stacked-to-main`
- Current work unit: `Work Unit 2 / Registry Enforcement`

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

## Verification

- Command: `GOSUMDB=off go mod tidy`
- Result: PASS
- Command: `GOSUMDB=off go test ./...`
- Result: PASS
- Command: `go test ./internal/ports ./internal/app/registry ./internal/protocol/http ./cmd/registry`
- Result: PASS
- Command: `go test ./...`
- Result: PASS

## Deviations

- None — implementation matches Work Unit 2 and intentionally stops before TUI admin flows or auth-aware smoke-script work.

## Remaining Tasks

- [ ] 3.1 Expand `internal/tui/model.go` and create `internal/tui/admin_users.go`, `admin_grants.go`, and `admin_tokens.go` for admin-only user CRUD, password reset, enable/disable, and repo grant editing.
- [ ] 3.2 Add `internal/tui/model_test.go` coverage for grant assignment, password reset outcomes, and non-admin token-management rejection paths.
- [ ] 4.1 Extend `docs/verification/scripts/docker-push-pull-smoke.sh` for `docker login`, authorized pull/push, and unauthorized catalog/tag scenarios with anonymous pull disabled.
- [ ] 4.2 Extend `docs/verification/scripts/tui-smoke.sh` and update `README.md` with Postgres auth bootstrap/runtime steps and the `bootstrap-admin` workflow.

## Workload / PR Boundary

- Mode: stacked PR slice
- Boundary: registry enforcement only — principal-aware ports, service authorization/filtering, router token+bearer handling, and focused tests
- Review budget impact: kept inside Work Unit 2; TUI admin and smoke/doc work remain out of scope for this slice

## Status

- 9/13 tasks complete overall
- Work Unit 2 complete
- Next recommended phase: continue `sdd-apply` with Work Unit 3
