## Verification Report

**Change**: registry-auth-v1
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 13 |
| Tasks complete | 13 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Tests**: ✅ Full Go suite passed
```text
$ go test ./...
ok   registry/cmd/registry                    (cached)
?    registry/internal/app/auth               [no test files]
ok   registry/internal/app/registry           (cached)
?    registry/internal/domain/auth            [no test files]
ok   registry/internal/domain/registry        (cached)
ok   registry/internal/infra/auth/postgres    (cached)
ok   registry/internal/infra/metadata/sqlite  (cached)
ok   registry/internal/infra/storage/fsblob   (cached)
ok   registry/internal/ports                  (cached)
ok   registry/internal/protocol/http          (cached)
ok   registry/internal/tui                    (cached)
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ go test -cover ./...
ok   registry/cmd/registry                    0.446s  coverage: 62.4% of statements
registry/internal/app/auth                            coverage: 0.0% of statements
ok   registry/internal/app/registry           0.410s  coverage: 60.0% of statements
registry/internal/domain/auth                         coverage: 0.0% of statements
ok   registry/internal/domain/registry        0.010s  coverage: 64.0% of statements
ok   registry/internal/infra/auth/postgres    0.289s  coverage: 56.6% of statements
ok   registry/internal/infra/metadata/sqlite  0.219s  coverage: 58.3% of statements
ok   registry/internal/infra/storage/fsblob   0.012s  coverage: 61.7% of statements
ok   registry/internal/ports                  0.009s  coverage: 60.7% of statements
ok   registry/internal/protocol/http          0.545s  coverage: 74.3% of statements
ok   registry/internal/tui                    0.011s  coverage: 52.6% of statements
```

**Runtime / manual evidence**: ✅ Verified from established session evidence
```text
- Auth-enabled local compose runtime started successfully with Postgres and bootstrap-admin.
- Unauthenticated GET /v2/_catalog returned 401 Unauthorized with a Bearer challenge.
- Docker-compatible GET /auth/token?... returned 200 after the /v2/ ping challenge fix.
- Authenticated docker push to 127.0.0.1:5517/registry-foundation/smoke:latest succeeded end-to-end.
- Registry logs showed the expected flow: unauthenticated /v2/ challenge, /auth/token 200, successful blob HEAD/POST/PATCH/PUT, and successful manifest PUT.
- Manual bearer-authenticated blob upload start, PATCH, and final PUT also succeeded.
```

**Local smoke rerun in this verify environment**: ⚠️ Not rerun here
```text
Current verify container still lacks Docker daemon access, so this refresh relies on the already-established session runtime evidence above rather than a fresh local smoke-script execution.
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Local user login and token lifecycle | Password login issues a bearer token | `internal/protocol/http/router_test.go > TestRouterIssuesAccessTokenFromBasicCredentials` | ✅ COMPLIANT |
| Local user login and token lifecycle | Revoked or expired token is rejected | `internal/protocol/http/router_test.go > TestRouterRejectsInvalidBearerTokens` | ✅ COMPLIANT |
| Standard bearer challenge behavior | Discovery is challenged when anonymous pull is off | External runtime evidence from this session: unauthenticated `GET /v2/_catalog` returned `401 Unauthorized` with Bearer challenge against the auth-enabled compose runtime | ✅ COMPLIANT |
| Fixed repository role grants | Reader can pull but not push | `internal/app/registry/service_test.go > TestServiceAuthorizesRepositoryActionsAndFiltersCatalog` | ✅ COMPLIANT |
| Fixed repository role grants | Authorization is repository-scoped | `internal/app/registry/service_test.go > TestServiceAuthorizesRepositoryActionsAndFiltersCatalog` | ✅ COMPLIANT |
| Discovery visibility follows grants | Catalog excludes unauthorized repositories | `internal/app/registry/service_test.go > TestServiceAuthorizesRepositoryActionsAndFiltersCatalog` | ✅ COMPLIANT |
| Minimal TUI user administration | Operator grants repository access | `internal/tui/model_test.go > TestModelAssignsGrantViaAdminWorkflow` | ✅ COMPLIANT |
| Minimal TUI user administration | Operator resets a password | `internal/tui/model_test.go > TestModelResetsPasswordViaAdminWorkflow` | ✅ COMPLIANT |
| Admin-only token administration | Non-admin cannot preissue a token | `internal/tui/model_test.go > TestModelRejectsNonAdminTokenManagementPaths` | ✅ COMPLIANT |

**Compliance summary**: 9/9 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Postgres-backed auth separate from SQLite registry metadata | ✅ Implemented | `cmd/registry/main.go` wires Postgres auth store separately from SQLite metadata and filesystem blob storage. |
| Admin bootstrap and fail-fast startup guard | ✅ Implemented | `newHandler()` and `runTUI()` call `EnsureBootstrapAdmin`; `runBootstrapAdmin()` provides the bootstrap path. |
| Admin CRUD, password reset, grant editing, and token administration flows | ✅ Implemented | `internal/app/auth/service.go` plus `internal/tui/admin_users.go`, `admin_grants.go`, and `admin_tokens.go` implement the operator workflows. |
| Bearer token issuance and verification | ✅ Implemented | `internal/protocol/http/router.go` exposes `/auth/token`; `internal/app/auth/service.go` issues and verifies access tokens. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Postgres stores auth state; SQLite remains registry metadata store | ✅ Yes | Matches the design boundary and avoids metadata migration. |
| Global admin bootstrap prevents auth-enabled startup deadlock | ✅ Yes | Startup fails fast when auth is enabled and no active global admin exists. |
| Two-token model: preissued admin credentials + short-lived bearer access tokens | ✅ Yes | Service layer keeps admin credential tokens distinct from 15-minute access tokens. |
| Principal-aware authorization with scoped Bearer challenges | ✅ Yes | Router and access controller emit verb/repository scopes and enforce repo-aware access. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- This artifact refresh reuses externally verified runtime/manual evidence already established in the session; the smoke scripts were not rerun inside the current verify container because Docker daemon access remains unavailable.
- The requested parallel `sdd/registry-auth-v1/*` artifact mirror was not present in `/workspace`; verification relied on the available OpenSpec artifacts as the authoritative source set.

**SUGGESTION**:
- Add a direct Go-level router test for unauthenticated catalog/tag challenge behavior so this required scenario is not validated only through Docker-dependent smoke coverage.
- Preserve the successful auth-enabled compose smoke outputs alongside the change archive for future review traceability.

### Verdict
PASS WITH WARNINGS
All required scenarios now have passing evidence when the full automated Go results are combined with the already-verified runtime/manual auth evidence from this session.
