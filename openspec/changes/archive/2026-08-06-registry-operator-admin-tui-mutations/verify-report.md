```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:529b457c2407a6540c8b4a6d0028f87fb138e727d342ee51cb210a16d216bdaa
verdict: pass
blockers: 0
critical_findings: 0
requirements: 3/3
scenarios: 7/7
test_command: go test ./internal/tui -count=1 && go test ./... -count=1
test_exit_code: 0
test_output_hash: sha256:1fcca94492dcd782b34ce5acd3d5ceb474f3bccde4af0ee31f7f35c6c9a0a083
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-operator-admin-tui-mutations
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 12 |
| Tasks complete | 12 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Focused runtime evidence — admin client mutations**: ✅ Passed
```text
$ go test ./internal/tui -count=1 -run 'TestHTTPAdminClient'
ok  	registry/internal/tui	0.020s
```

**Focused runtime evidence — users-screen mutation flow**: ✅ Passed
```text
$ go test ./internal/tui -count=1 -run 'TestModel.*Admin|TestModel.*Disable|TestModel.*Enable'
ok  	registry/internal/tui	0.009s
```

**Tests**: ✅ Full TUI package and full Go suite passed
```text
$ go test ./internal/tui -count=1
ok  	registry/internal/tui	0.020s
$ go test ./... -count=1
ok  	registry/cmd/registry	1.136s
ok  	registry/internal/app/auth	0.136s
ok  	registry/internal/app/registry	0.298s
?   	registry/internal/domain/auth	[no test files]
ok  	registry/internal/domain/registry	0.006s
ok  	registry/internal/infra/auth/postgres	0.227s
ok  	registry/internal/infra/metadata/sqlite	0.159s
ok  	registry/internal/infra/storage/fsblob	0.014s
ok  	registry/internal/ports	0.010s
ok  	registry/internal/protocol/http	1.078s
ok  	registry/internal/tui	0.019s
```

**Coverage**: 72.2% / threshold: 0% → ✅ Above
```text
$ go test -cover ./internal/tui -count=1
ok  	registry/internal/tui	0.058s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Confirmed User Enable Disable Actions | Confirmed disable succeeds | `internal/tui/model_test.go > TestModelDisableUserSuccessRefreshesUsersAndPreservesSelectionByID` | ✅ COMPLIANT |
| Confirmed User Enable Disable Actions | Confirmation declined prevents mutation | `internal/tui/model_test.go > TestModelAdminMutationCancelDismissSendsNoRequest` | ✅ COMPLIANT |
| Mutation Failure Feedback | Backend conflict is shown clearly | `internal/tui/model_test.go > TestModelAdminMutationRecoverableFailuresStayOnUsersScreen/conflict`; `internal/tui/admin_client_test.go > TestHTTPAdminClientUserMutations/disable user conflict stays recoverable` | ✅ COMPLIANT |
| Mutation Failure Feedback | Expired session blocks mutation completion | `internal/tui/model_test.go > TestModelAdminMutationExpiredSessionReturnsToLogin`; `internal/tui/admin_client_test.go > TestHTTPAdminClientUserMutations/invalid token maps to expired session`; `internal/tui/admin_client_test.go > TestHTTPAdminClientUserMutations/locally expired session rejects mutation without request` | ✅ COMPLIANT |
| Mutation Failure Feedback | Validation failure remains recoverable | `internal/tui/model_test.go > TestModelAdminMutationRecoverableFailuresStayOnUsersScreen/validation`; `internal/tui/admin_client_test.go > TestHTTPAdminClientUserMutations/validation failure stays recoverable` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Operator browses admin data | `internal/tui/model_test.go > TestModelSuccessfulLoginOpensAdminUsers`; `internal/tui/model_test.go > TestModelReadOnlyAdminBrowsing` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Unauthenticated state blocks admin reads | `internal/tui/model_test.go > TestModelBlocksAdminUntilLogin` | ✅ COMPLIANT |

**Compliance summary**: 7/7 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Confirmed single-user enable/disable only on users screen | ✅ Implemented | `internal/tui/model.go:626-689` limits confirmation and mutation entry to `screenAdminUsers`; `internal/tui/model.go:1049-1097` exposes mutation hints only from the users screen. |
| Backend-authoritative mutation failures stay recoverable | ✅ Implemented | `internal/tui/admin_client.go:145-211` reuses backend POST routes and preserves decoded backend errors; `internal/tui/model.go:377-393` clears local confirmation state and only refreshes after success. |
| Expired or invalid admin sessions force re-authentication | ✅ Implemented | `internal/tui/admin_client.go:158-176,198-211` maps local/session invalidation to `AdminSessionExpiredError`; `internal/tui/model.go:912-922` clears admin state and returns to login. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Keep backend-authoritative writes instead of local optimistic policy | ✅ Yes | `internal/tui/admin_client.go:145-151` sends POST mutations through the backend client, and `internal/tui/model.go:388-393` refreshes users from `ListUsers` after success instead of patching local state optimistically. |
| Use a focused confirmation substate on `screenAdminUsers` | ✅ Yes | `internal/tui/model.go:631-642` handles confirm/cancel inside `adminMutation`, and `internal/tui/model.go:1071-1085` renders the confirmation/in-flight copy inline on the existing users screen. |
| Refresh users after success and preserve selection by user ID | ✅ Yes | `internal/tui/model.go:346-375` restores selection by `SelectedUserID` after reload, and `internal/tui/model_test.go > TestModelDisableUserSuccessRefreshesUsersAndPreservesSelectionByID` proves the reordered refreshed list preserves the mutated selection. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- None.

**SUGGESTION**:
- None.

### Verdict
PASS
All 12 tasks are complete, OpenSpec and Engram now both contain `apply-progress`, build/test/coverage/vet evidence passed, and all 7 spec scenarios are covered by passing runtime tests.
