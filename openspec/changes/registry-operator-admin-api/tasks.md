# Tasks: Registry Operator Admin API

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 1000-1350 |
| 400-line budget risk | High |
| 1200-line budget risk | Medium |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 foundation → PR 2 user routes → PR 3 access routes/docs |
| Delivery strategy | auto-forecast |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Narrow service/store contract and admin error mapping seam | PR 1 | Include service/store tests; if chained, choose stacked-to-main or feature-branch-chain before apply. |
| 2 | Ship `/admin/v1/users` list/create/enable/disable/reset-password routes | PR 2 | Depends on PR 1; include router integration coverage. |
| 3 | Ship grants + admin-token routes and docs | PR 3 | Depends on PR 2; include one-time-secret and nested revoke coverage. |

## Phase 1: Foundation

- [x] 1.1 Update `internal/ports/auth.go` with a focused admin HTTP interface plus request/response types limited to in-scope reversible actions.
- [x] 1.2 Tighten `internal/app/auth/service.go` and `internal/infra/auth/postgres/store.go` for scoped user/token lookup needed by nested revoke and precise `404/409/422` outcomes.
- [x] 1.3 Extend `internal/app/auth/service_test.go` and `internal/infra/auth/postgres/store_test.go` for last-active-admin, missing-user, disabled-user, TTL-cap, and token-ownership cases.

## Phase 2: User Administration Routes

- [x] 2.1 Create `internal/protocol/http/admin_handlers.go` with bearer auth, admin principal checks, JSON parsing, and admin-only error translation.
- [x] 2.2 Modify `internal/protocol/http/router.go` to register `/admin/v1/users`, `/admin/v1/users/{id}:enable`, `/admin/v1/users/{id}:disable`, and `/admin/v1/users/{id}:reset-password` without changing `/auth/token` or `/v2/*` behavior.
- [x] 2.3 Expand `internal/protocol/http/router_test.go` for `401`, `403`, `409`, `422`, list-user redaction, and create/reset/enable/disable happy paths from the user-admin spec.

## Phase 3: Access Administration Routes

- [x] 3.1 Extend `internal/protocol/http/admin_handlers.go` for `/admin/v1/users/{id}/grants` list/put/delete with repository and role validation.
- [x] 3.2 Extend `internal/protocol/http/admin_handlers.go` for `/admin/v1/users/{id}/admin-tokens` list/create/delete, returning plaintext secret only on create and `404` on mismatched accessor ownership.
- [x] 3.3 Expand `internal/protocol/http/router_test.go` for grant replacement, invalid grant input, token one-time-secret responses, excessive TTL rejection, and scoped revoke failures.

## Phase 4: Docs and Final Verification

- [x] 4.1 Update `README.md`, `docs/architecture.md`, and `docs/roadmap.md` to document `/admin/v1`, CLI-next/TUI-later boundaries, and deferred pagination/delete-user scope.
- [x] 4.2 Run `gofmt -w .`, `go test ./...`, `go test -cover ./...`, and `go vet ./...`; record any admin-API follow-ups discovered during verification in the change workflow.
