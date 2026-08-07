```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:d427252d0680fcaedca53256dfc976ff7ab73f475817978a8a9eccc234029c3c
verdict: pass
blockers: 0
critical_findings: 0
requirements: 4/4
scenarios: 8/8
test_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:f01dae9be19985274c7af1dba822d6adfa939dd5a9e2c97905cb9c0f114d051c
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-runtime-hardening
**Version**: N/A
**Mode**: Standard

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 12 |
| Tasks complete | 12 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Formatting**: ✅ Passed
```text
$ gofmt -l .
(no output)
```

**Build**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go build ./...
(no output)
```

**Manual runtime evidence**: ✅ Accepted for rerun
```text
Manual runtime evidence reused for this rerun from Engram observation #247 and the aligned apply-progress artifact: `go run ./cmd/registry serve -public-url http://127.0.0.1:5560 -auth-token-realm http://127.0.0.1:5560/auth/token` against host Postgres `telemetry.host:15432` launched successfully; `GET /v2/_catalog` returned `401 Unauthorized` with the canonical Bearer realm; `/auth/token` issued a bearer token; authenticated Docker login, push, pull, and manifest fetch all succeeded against `127.0.0.1:5560`.
```

**Tests**: ✅ Full Go suite passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -count=1 ./...
ok  	registry/cmd/registry	1.704s
ok  	registry/internal/app/auth	0.237s
ok  	registry/internal/app/registry	0.480s
?   	registry/internal/domain/auth	[no test files]
ok  	registry/internal/domain/registry	0.024s
ok  	registry/internal/infra/auth/postgres	0.332s
ok  	registry/internal/infra/metadata/sqlite	0.263s
ok  	registry/internal/infra/storage/fsblob	0.012s
ok  	registry/internal/ports	0.010s
ok  	registry/internal/protocol/http	1.308s
ok  	registry/internal/tui	0.034s
```

**Coverage**: Per-package coverage reported / threshold: 0% → ✅ Above
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -count=1 -cover ./...
ok  	registry/cmd/registry	1.504s	coverage: 75.9% of statements
ok  	registry/internal/app/auth	0.160s	coverage: 30.3% of statements
ok  	registry/internal/app/registry	0.394s	coverage: 60.0% of statements
	registry/internal/domain/auth		coverage: 0.0% of statements
ok  	registry/internal/domain/registry	0.019s	coverage: 64.0% of statements
ok  	registry/internal/infra/auth/postgres	0.309s	coverage: 57.7% of statements
ok  	registry/internal/infra/metadata/sqlite	0.201s	coverage: 58.3% of statements
ok  	registry/internal/infra/storage/fsblob	0.012s	coverage: 61.7% of statements
ok  	registry/internal/ports	0.012s	coverage: 60.0% of statements
ok  	registry/internal/protocol/http	1.161s	coverage: 71.5% of statements
ok  	registry/internal/tui	0.032s	coverage: 72.2% of statements
```

**Static analysis**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...
(no output)
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| TLS Runtime Modes | TLS-enabled startup | `cmd/registry/main_test.go > TestServeStartsAndRespondsToPingOverTLS` | ✅ COMPLIANT |
| TLS Runtime Modes | Explicit local HTTP startup | `cmd/registry/main_test.go > TestNormalizeRuntimeConfig/explicit local http mode stays http without TLS`; `cmd/registry/main_test.go > TestServeStartsAndRespondsToPing`; manual runtime evidence #247 | ✅ COMPLIANT |
| Canonical Public URL and Realm Validation | Matching public URL and realm | `cmd/registry/main_test.go > TestNormalizeRuntimeConfig/https public URL requires TLS pair and derives token realm`; `cmd/registry/main_test.go > TestNewHandlerUsesConfiguredAuthTokenRealmInChallenge`; manual runtime evidence #247 | ✅ COMPLIANT |
| Canonical Public URL and Realm Validation | Mismatched public URL and realm | `cmd/registry/main_test.go > TestNormalizeRuntimeConfig/configured auth realm must match derived token realm` | ✅ COMPLIANT |
| Hardened HTTP Runtime Bounds | Normal bounded serving | `cmd/registry/main_test.go > TestNewHTTPServerAppliesBoundedTimeouts`; `cmd/registry/main_test.go > TestServeStartsAndRespondsToPing` | ✅ COMPLIANT |
| Hardened HTTP Runtime Bounds | Shutdown exceeds the bound | `cmd/registry/main_test.go > TestServeShutdownDeadlineStopsWaitingOnActiveUpload` | ✅ COMPLIANT |
| Bootstrap Admin Secret Guidance | Safer secret entry path used | `cmd/registry/main_test.go > TestParseBootstrapAdminConfigPrefersStdinSecretAndWarnsOnLegacyArgv`; `README.md` and `docs/verification/scripts/docker-push-pull-smoke.sh` prefer `-password-stdin` | ✅ COMPLIANT |
| Bootstrap Admin Secret Guidance | Compatibility secret path used | `cmd/registry/main_test.go > TestParseBootstrapAdminConfigWarnsWhenUsingLegacyPasswordFlag`; `cmd/registry/main_test.go > TestRunBootstrapAdminPrintsLegacyPasswordWarningToStderr` | ✅ COMPLIANT |

**Compliance summary**: 8/8 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| TLS runtime modes | ✅ Implemented | `parseServeConfig` and `normalizeRuntimeConfig` require a canonical `PublicURL`, enforce HTTP-vs-HTTPS mode consistency, and `serveWithRuntimeMode` selects `Serve` vs `ServeTLS`; `docker-compose.yml` and `README.md` document the explicit local HTTP path and HTTPS mode. |
| Canonical public URL and realm validation | ✅ Implemented | `normalizeRuntimeConfig` derives `/auth/token` from `PublicURL`, rejects mismatches against `AuthTokenRealmURL`, and `run()` injects the canonical realm back into serving config before binding traffic. |
| Hardened HTTP runtime bounds | ✅ Implemented | `newHTTPServer` applies bounded header/read/write/idle defaults and `serve()` uses a bounded shutdown context with forced close once the shutdown deadline is exceeded. |
| Bootstrap admin secret guidance | ✅ Implemented | `parseBootstrapAdminConfig` supports `-password-stdin`, prefers stdin over argv when both are present, and emits a discouraging warning for `-password`; README and the smoke script prefer stdin-based examples. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Canonical runtime URL | ✅ Yes | The implementation adds `PublicURL`, derives `/auth/token`, and keeps `AuthTokenRealmURL` only as a compatibility check that must exactly match the derived realm. |
| TLS model | ✅ Yes | `normalizeRuntimeConfig` enforces both-or-neither TLS inputs, requires TLS for `https://` public URLs, and forbids TLS inputs for `http://` public URLs. |
| Secret guidance scope | ✅ Yes | The safer stdin path was added without redesigning secret storage, while legacy argv support remains available with explicit warning text and updated docs/examples. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- None.

**SUGGESTION**:
- None.

### Verdict
PASS
All 12 tasks are complete, automated Go verification passed, and the previously captured manual runtime evidence is sufficient to re-verify all 8 spec scenarios after the planning artifacts were aligned.
