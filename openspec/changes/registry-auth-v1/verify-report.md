## Verification Report

**Change**: registry-auth-v1
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 13 |
| Tasks complete | 11 |
| Tasks incomplete | 2 |

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
ok   registry/internal/app/auth               (cached)
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
ok   registry/cmd/registry                    (cached) coverage: 66.1% of statements
ok   registry/internal/app/auth               (cached) coverage: 22.6% of statements
ok   registry/internal/app/registry           0.410s  coverage: 60.0% of statements
registry/internal/domain/auth                         coverage: 0.0% of statements
ok   registry/internal/domain/registry        0.010s  coverage: 64.0% of statements
ok   registry/internal/infra/auth/postgres    0.289s  coverage: 57.4% of statements
ok   registry/internal/infra/metadata/sqlite  0.219s  coverage: 58.3% of statements
ok   registry/internal/infra/storage/fsblob   0.012s  coverage: 61.7% of statements
ok   registry/internal/ports                  0.009s  coverage: 60.0% of statements
ok   registry/internal/protocol/http          0.545s  coverage: 75.0% of statements
ok   registry/internal/tui                    0.011s  coverage: 52.1% of statements
```

**Runtime / manual evidence**: ✅ Supplemental evidence from established session verification
```text
- Auth-enabled local Compose helper runtime started successfully with Postgres and bootstrap-admin.
- Unauthenticated GET /v2/_catalog returned 401 Unauthorized with a Bearer challenge.
- Docker-compatible GET /auth/token?... returned 200 after the /v2/ ping challenge fix.
- Authenticated docker push to 127.0.0.1:5517/registry-foundation/smoke:latest succeeded end-to-end.
- Registry logs showed the expected flow: unauthenticated /v2/ challenge, /auth/token 200, successful blob HEAD/POST/PATCH/PUT, and successful manifest PUT.
- Manual bearer-authenticated blob upload start, PATCH, and final PUT also succeeded.
- Auth-enabled TUI snapshot continued to render repository inspection output and an explicit notice that local admin actions are disabled until a real operator login flow exists.
```

**Local smoke rerun in this verify environment**: ⚠️ Not rerun here
```text
Current verify container still lacks Docker daemon access, so this refresh relies on the already-established session runtime evidence above rather than a fresh local smoke-script execution.
The primary automated verification contract for this artifact remains the Go build/test evidence above; the Compose helper runtime evidence is supportive, not canonical on its own.
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
| Auth-enabled TUI remains inspection-oriented | Operator can still inspect repositories in auth-enabled mode | `cmd/registry/main_test.go > TestRunTUIRendersRepositorySnapshot` and `cmd/registry/main_test.go > TestRunTUIDisablesAuthAdminShortcutWhenAuthIsEnabled` | ✅ COMPLIANT |
| Auth-enabled TUI remains inspection-oriented | Auth-backed admin shortcuts stay unavailable | `cmd/registry/main_test.go > TestRunTUIDisablesAuthAdminShortcutWhenAuthIsEnabled` | ✅ COMPLIANT |
| Auth-enabled TUI shows an explicit security notice | Security notice explains the deferred admin path | `cmd/registry/main_test.go > TestRunTUIDisablesAuthAdminShortcutWhenAuthIsEnabled` and `docs/verification/scripts/tui-smoke.sh` | ✅ COMPLIANT |
| Safe operator login and admin UI remain deferred | Deferred operator admin workflow is not claimed as shipped | Spec/task/apply/README artifact alignment in this refresh | ✅ COMPLIANT |

**Compliance summary**: 10/10 shipped-behavior scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Postgres-backed auth separate from SQLite registry metadata | ✅ Implemented | `cmd/registry/main.go` wires Postgres auth store separately from SQLite metadata and filesystem blob storage. |
| Admin bootstrap and fail-fast startup guard | ✅ Implemented | `newHandler()` and `runTUI()` call `EnsureBootstrapAdmin`; `runBootstrapAdmin()` provides the bootstrap path. |
| Auth-enabled TUI inspection plus explicit security notice | ✅ Implemented | `cmd/registry/main.go` keeps inspection available and adds a notice instead of enabling local admin mutations. |
| Bearer token issuance and verification | ✅ Implemented | `internal/protocol/http/router.go` exposes `/auth/token`; `internal/app/auth/service.go` issues and verifies access tokens. |
| Safe authenticated operator admin UI for local TUI mutations | ⚠️ Deferred | Shipped behavior intentionally leaves this pending to avoid a backend auth bypass. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Postgres stores auth state; SQLite remains registry metadata store | ✅ Yes | Matches the design boundary and avoids metadata migration. |
| Global admin bootstrap prevents auth-enabled startup deadlock | ✅ Yes | Startup fails fast when auth is enabled and no active global admin exists. |
| Two-token model: preissued admin credentials + short-lived bearer access tokens | ✅ Yes | Service layer keeps admin credential tokens distinct from 15-minute access tokens. |
| Principal-aware authorization with scoped Bearer challenges | ✅ Yes | Router and access controller emit verb/repository scopes and enforce repo-aware access. |
| Auth-enabled local TUI admin mutations | ❌ No | The originally planned TUI admin workflow was intentionally disabled before ship; current behavior is inspection plus a security notice until a real operator login flow exists. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- This artifact refresh reuses previously established runtime/manual evidence from the local Compose helper runtime; the smoke scripts were not rerun inside the current verify container because Docker daemon access remains unavailable, so Compose behavior is not re-proven here as a standalone automated contract.
- The requested parallel `sdd/registry-auth-v1/*` artifact mirror was not present in `/workspace`; verification relied on the available OpenSpec artifacts as the authoritative source set.
- The auth-enabled TUI admin UI originally described in the change design/tasks is not part of the shipped behavior; verification for this refresh is intentionally limited to the hardened inspection-only path.

**SUGGESTION**:
- Add a direct Go-level router test for unauthenticated catalog/tag challenge behavior so this required scenario is not validated only through Docker-dependent smoke coverage.
- Preserve the successful auth-enabled compose smoke outputs alongside the change archive for future review traceability.
- Create a follow-up change for authenticated operator login plus safe admin UI before re-enabling any local auth-backed mutations.

### Verdict
PARTIAL / SHIPPED SCOPE VERIFIED
The shipped auth runtime behavior is verified, including the hardened inspection-only TUI path and explicit security notice. Safe local operator admin mutations remain deferred and are not claimed as delivered in this report.
