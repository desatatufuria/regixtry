```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:e064588345599e679c2ba30b2dc74381ae3b9562cdca457b24686d7269bfc68f
verdict: fail
blockers: 1
critical_findings: 1
requirements: 4/5
scenarios: 9/10
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:f154278f2d984333f3b542c749157b3afbc3cb63a20be702fedff035a4a50740
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: tui-admin-console-premium
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 13 |
| Tasks complete | 13 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Focused package checks**: ✅ Passed
```text
$ go test -count=1 ./internal/tui -run TestHTTPAdminClient|TestModel
ok  	regixtry/internal/tui	0.046s

$ go test -count=1 ./cmd/regixtry -run TestRunTUI
ok  	regixtry/cmd/regixtry	0.344s
```

**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Tests**: ✅ Full Go suite passed
```text
$ go test -count=1 ./...
ok  	regixtry/cmd/regixtry	1.677s
ok  	regixtry/internal/app/auth	0.137s
ok  	regixtry/internal/app/regixtry	0.313s
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	0.017s
ok  	regixtry/internal/infra/auth/postgres	0.246s
ok  	regixtry/internal/infra/install/linux	0.052s
ok  	regixtry/internal/infra/install/releases	0.030s
ok  	regixtry/internal/infra/metadata/sqlite	0.179s
ok  	regixtry/internal/infra/storage/fsblob	0.013s
ok  	regixtry/internal/ports	0.019s
ok  	regixtry/internal/protocol/http	0.972s
ok  	regixtry/internal/tui	0.070s
```

**Coverage**: Informational only → ✅ Captured
```text
$ go test -count=1 -cover ./...
ok  	regixtry/cmd/regixtry	1.694s	coverage: 80.2% of statements
ok  	regixtry/internal/app/auth	0.142s	coverage: 30.3% of statements
ok  	regixtry/internal/app/regixtry	0.326s	coverage: 60.0% of statements
	regixtry/internal/domain/auth		coverage: 0.0% of statements
ok  	regixtry/internal/domain/regixtry	0.022s	coverage: 64.0% of statements
ok  	regixtry/internal/infra/auth/postgres	0.268s	coverage: 57.7% of statements
ok  	regixtry/internal/infra/install/linux	0.073s	coverage: 72.1% of statements
ok  	regixtry/internal/infra/install/releases	0.024s	coverage: 58.0% of statements
ok  	regixtry/internal/infra/metadata/sqlite	0.176s	coverage: 58.3% of statements
ok  	regixtry/internal/infra/storage/fsblob	0.014s	coverage: 61.7% of statements
ok  	regixtry/internal/ports	0.014s	coverage: 60.0% of statements
ok  	regixtry/internal/protocol/http	1.083s	coverage: 71.5% of statements
ok  	regixtry/internal/tui	0.099s	coverage: 67.5% of statements
```

**Static analysis**: ✅ Passed
```text
$ go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Premium Workspace | Shell opens | `internal/tui/model_test.go > TestModelSuccessfulLoginRendersPremiumWorkspace` | ✅ COMPLIANT |
| Premium Workspace | Failures stay recoverable | `internal/tui/model_test.go > TestModelResetPasswordFailureKeepsFormVisible` | ✅ COMPLIANT |
| User Provisioning And Reset | Operator creates an admin user | `internal/tui/model_test.go > TestModelCreateAdminUserRefreshesUsers`; `internal/tui/admin_client_test.go > TestHTTPAdminClientMutationRoutes/create user` | ✅ COMPLIANT |
| User Provisioning And Reset | Unsupported admin edit is not offered | (none found) | ❌ UNTESTED |
| Selected Grants | Operator changes a selected user's grant | `internal/tui/model_test.go > TestModelGrantSaveAndRemoveStayContextualizedToSelectedUser`; `internal/tui/admin_client_test.go > TestHTTPAdminClientMutationRoutes/put grant` | ✅ COMPLIANT |
| Selected Grants | No selected user blocks grant writes | `internal/tui/model_test.go > TestModelNoSelectedUserBlocksGrantWrites` | ✅ COMPLIANT |
| Selected Tokens | Token secret is revealed once | `internal/tui/model_test.go > TestModelTokenCreateRevealOnceAndRevokeConfirmation`; `internal/tui/admin_client_test.go > TestHTTPAdminClientMutationRoutes/create admin token` | ✅ COMPLIANT |
| Selected Tokens | Revoke uses explicit confirmation | `internal/tui/model_test.go > TestModelTokenCreateRevealOnceAndRevokeConfirmation`; `internal/tui/admin_client_test.go > TestHTTPAdminClientMutationRoutes/revoke token` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Operator browses and mutates admin data in-context | `internal/tui/model_test.go > TestModelCreateAdminUserRefreshesUsers`; `internal/tui/model_test.go > TestModelGrantSaveAndRemoveStayContextualizedToSelectedUser`; `internal/tui/model_test.go > TestModelTokenCreateRevealOnceAndRevokeConfirmation` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Unauthenticated state blocks admin workspace access | `internal/tui/model_test.go > TestModelBlocksAdminUntilLogin` | ✅ COMPLIANT |

**Compliance summary**: 9/10 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Premium Workspace | ✅ Implemented | `internal/tui/admin_theme.go` hard-codes the premium palette, `internal/tui/admin_views.go` renders the sidebar and short modal layout, and `internal/tui/model.go` routes authenticated operators into the workspace. |
| User Provisioning And Reset | ⚠️ Implemented with verification gap | `internal/tui/model.go` only exposes create-user, password-reset, and enable/disable flows; `internal/tui/admin_client.go` uses existing `/admin/v1` routes. The runtime suite does not contain an explicit assertion that post-creation admin editing and delete-user controls are absent. |
| Selected Grants | ✅ Implemented | `switchAdminPanel`, `updateAdminGrantsKey`, and `putAdminGrantCmd/deleteAdminGrantCmd` gate writes on `SelectedUserID` and reload backend state after each mutation. |
| Selected Tokens | ✅ Implemented | `updateAdminTokensKey` blocks writes without selection, `adminTokenCreatedMsg` reveals the secret once, and token secret/accessor state is cleared when leaving the tokens panel or after revoke. |
| Read-Only Admin Browsing | ✅ Implemented | `openAdmin` forces login before admin mode, local inspection views stay available outside admin screens, and successful writes refresh users/grants/tokens from backend responses instead of local optimistic state. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Extend the existing Bubble Tea model | ✅ Yes | `cmd/regixtry/main.go` still wires a single `tui.NewModel(...)` instance and only injects `WithAdminClient` when `APIBaseURL` is configured. |
| Centralize premium styling in helpers | ✅ Yes | The palette lives in `internal/tui/admin_theme.go` and render composition lives in `internal/tui/admin_views.go`, keeping `model.go` focused on state transitions. |
| Refresh from backend after every write | ✅ Yes | Create, reset, grant, revoke, and enable/disable message handlers all route through refresh commands instead of mutating local shadow copies. |
| Typed HTTP admin client over existing routes | ✅ Yes | `internal/tui/admin_client.go` adds create/reset/grant/token methods over `/auth/token` and `/admin/v1` with `201/204` handling, matching the design contract. |

### Issues Found
**CRITICAL**:
- The spec scenario `User Provisioning And Reset / Unsupported admin edit is not offered` has no passing covering runtime test. Static inspection suggests the controls are absent, but the verify gate requires a runtime assertion for compliance.

**WARNING**:
- None.

**SUGGESTION**:
- Add a focused runtime test that renders the authenticated users panel for an existing user and asserts the view omits post-creation `is_admin` editing and delete-user controls.

### Verdict
FAIL
Implementation and runtime checks are broadly healthy, but final verification cannot pass while one required spec scenario remains unproven by a passing runtime test.
