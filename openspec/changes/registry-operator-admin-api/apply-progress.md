# Apply Progress: registry-operator-admin-api

## Change

- Name: `registry-operator-admin-api`
- Mode: Standard
- Delivery strategy: `auto-forecast`
- Chain strategy: `stacked-to-main`
- Current work unit: `4`

## Completed Tasks

- [x] 1.1 Update `internal/ports/auth.go` with a focused admin HTTP interface plus request/response types limited to in-scope reversible actions.
- [x] 1.2 Tighten `internal/app/auth/service.go` and `internal/infra/auth/postgres/store.go` for scoped user/token lookup needed by nested revoke and precise `404/409/422` outcomes.
- [x] 1.3 Extend `internal/app/auth/service_test.go` and `internal/infra/auth/postgres/store_test.go` for last-active-admin, missing-user, disabled-user, TTL-cap, and token-ownership cases.
- [x] 2.1 Create `internal/protocol/http/admin_handlers.go` with bearer auth, admin principal checks, JSON parsing, and admin-only error translation.
- [x] 2.2 Modify `internal/protocol/http/router.go` to register `/admin/v1/users`, `/admin/v1/users/{id}:enable`, `/admin/v1/users/{id}:disable`, and `/admin/v1/users/{id}:reset-password` without changing `/auth/token` or `/v2/*` behavior.
- [x] 2.3 Expand `internal/protocol/http/router_test.go` for `401`, `403`, `409`, `422`, list-user redaction, and create/reset/enable/disable happy paths from the user-admin spec.
- [x] 3.1 Extend `internal/protocol/http/admin_handlers.go` for `/admin/v1/users/{id}/grants` list/put/delete with repository and role validation.
- [x] 3.2 Extend `internal/protocol/http/admin_handlers.go` for `/admin/v1/users/{id}/admin-tokens` list/create/delete, returning plaintext secret only on create and `404` on mismatched accessor ownership.
- [x] 3.3 Expand `internal/protocol/http/router_test.go` for grant replacement, invalid grant input, token one-time-secret responses, excessive TTL rejection, and scoped revoke failures.
- [x] 4.1 Update `README.md`, `docs/architecture.md`, and `docs/roadmap.md` to document `/admin/v1`, CLI-next/TUI-later boundaries, and deferred pagination/delete-user scope.
- [x] 4.2 Run `gofmt -w .`, `go test ./...`, `go test -cover ./...`, and `go vet ./...`; record any admin-API follow-ups discovered during verification in the change workflow.

## Files Changed

| File | Action | Notes |
|---|---|---|
| `internal/ports/auth.go` | Modified | Added focused admin HTTP DTOs/interfaces and store token-accessor lookup contract. |
| `internal/app/auth/service.go` | Modified | Added admin-facing wrappers, preserved safeguard reuse, enforced missing-user checks, and scoped admin-token revocation ownership. |
| `internal/infra/auth/postgres/store.go` | Modified | Added admin token lookup by accessor for user-scoped revoke flows. |
| `internal/app/auth/service_test.go` | Modified | Added coverage for last-active-admin, missing-user, disabled-user, TTL-cap, and token-ownership behavior. |
| `internal/infra/auth/postgres/store_test.go` | Modified | Added lookup-by-accessor coverage for persisted admin tokens. |
| `internal/protocol/http/admin_handlers.go` | Created | Implemented `/admin/v1/users` list/create/enable/disable/reset-password handlers with explicit admin auth and JSON validation. |
| `internal/protocol/http/router.go` | Modified | Registered concrete `/admin/v1/users` route patterns while preserving `/auth/token` and `/v2/*` behavior. |
| `internal/protocol/http/router_test.go` | Modified | Added admin route coverage for `401`, `403`, `409`, `422`, list redaction, and create/reset/enable/disable happy paths. |
| `internal/protocol/http/admin_handlers.go` | Modified | Added nested `/grants` and `/admin-tokens` handlers, TTL parsing, nested route parsing, and registry-validation mapping for admin responses. |
| `internal/protocol/http/router_test.go` | Modified | Added integration coverage for grant replacement/deletion, invalid grant input, one-time token secrets, TTL rejection, and scoped token revoke failures. |
| `README.md` | Modified | Documented the shipped `/admin/v1` surface, API-first guardrails, and deferred operator scope. |
| `docs/architecture.md` | Modified | Added the operator-admin runtime flow and clarified the API-first versus TUI-later boundary. |
| `docs/roadmap.md` | Modified | Marked the operator admin API as implemented and kept deferred pagination/delete-user/TUI work explicit. |
| `docs/verification/operator-admin-api.md` | Created | Added the verification guide for the shipped `/admin/v1` slice and repo-wide validation commands. |
| `openspec/changes/registry-operator-admin-api/tasks.md` | Modified | Marked Work Unit 4 tasks complete in OpenSpec. |
| `openspec/changes/registry-operator-admin-api/apply-progress.md` | Modified | Recorded the cumulative Work Unit 4 completion state and repo-wide verification evidence. |
| `internal/protocol/http/router_test.go` | Modified | Added runtime coverage proving unsupported `/admin/v1/users/{id}` delete and broad update mutations stay unavailable with `404` semantics. |

## Verification

- Command: `gofmt -w internal/protocol/http/admin_handlers.go internal/protocol/http/router.go internal/protocol/http/router_test.go`
- Result: PASS
- Command: `go test ./internal/protocol/http`
- Result: PASS
- Command: `go test ./internal/protocol/http ./internal/app/auth ./internal/infra/auth/postgres`
- Result: PASS
- Command: `gofmt -w internal/protocol/http/admin_handlers.go internal/protocol/http/router_test.go`
- Result: PASS
- Command: `go test ./internal/protocol/http ./internal/app/auth ./internal/infra/auth/postgres`
- Result: PASS
- Command: `gofmt -w .`
- Result: PASS
- Command: `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...`
- Result: PASS — `cmd/registry`, `internal/app/auth`, `internal/app/registry`, `internal/domain/registry`, `internal/infra/auth/postgres`, `internal/infra/metadata/sqlite`, `internal/infra/storage/fsblob`, `internal/ports`, `internal/protocol/http`, and `internal/tui` passed; `internal/domain/auth` has no test files.
- Command: `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -cover ./...`
- Result: PASS — coverage reported successfully for every package (`cmd/registry` 66.1%, `internal/app/auth` 30.3%, `internal/app/registry` 60.0%, `internal/domain/auth` 0.0%, `internal/domain/registry` 64.0%, `internal/infra/auth/postgres` 57.7%, `internal/infra/metadata/sqlite` 58.3%, `internal/infra/storage/fsblob` 61.7%, `internal/ports` 60.0%, `internal/protocol/http` 71.3%, `internal/tui` 59.2%).
- Command: `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...`
- Result: PASS
- Command: `go test ./internal/protocol/http`
- Result: PASS
- Command: `go test ./...`
- Result: PASS

## Deviations

- None — implementation matches the planned Work Unit 4 close-out boundary and keeps pagination, delete-user, and richer CLI/TUI clients deferred.

## Remaining Tasks

- None.

## Follow-ups Discovered During Verification

- None — the repo-wide verification pass did not surface additional operator-admin regressions or new scope changes.

## Workload / PR Boundary

- Mode: stacked PR slice
- Boundary: closes PR 4 with docs alignment, verification guidance, and repo-wide validation for the shipped `/admin/v1` operator API; no new admin capability, local shortcut, or TUI workflow was added.
- Review budget impact: documentation plus verification refresh only, comfortably inside the 1200-line PR 4 budget.

## Status

- 11/11 tasks complete overall
- Work Unit 4 is complete and the full change has repo-wide verification evidence recorded
- Verify-gap follow-up is complete: router coverage now proves out-of-scope `/admin/v1` user mutation routes remain unavailable at runtime
- Next recommended phase: `sdd-verify`
