# Tasks: Registry Runtime Hardening

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 650-900 |
| 1200-line budget risk | Low |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 runtime config/tests -> PR 2 serve/admin client -> PR 3 compose/smoke/docs |
| Delivery strategy | exception-ok |
| Chain strategy | stacked-to-main |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Runtime config normalization + RED/GREEN tests | PR 1 | `go test ./cmd/registry -run 'TestParseServeConfig|TestNormalizeRuntimeConfig|TestParseBootstrapAdminConfig'` | `go test ./cmd/registry -run TestServe` | `cmd/registry/main.go`, `cmd/registry/main_test.go` config paths |
| 2 | Bounded serving + admin client timeout | PR 2 | `go test ./cmd/registry ./internal/tui -run 'TestServe|TestNewHTTPAdminClient'` | `go test ./cmd/registry -run TestServe` | `cmd/registry/main.go`, `internal/tui/admin_client*.go` |
| 3 | Compose, smoke, README operator flow | PR 3 | `go test ./...` | `docs/verification/scripts/docker-push-pull-smoke.sh` | `docker-compose.yml`, `docs/verification/scripts/docker-push-pull-smoke.sh`, `README.md` |

## Phase 1: Benchmark and Foundation

- [x] 1.1 Benchmark Go `http.Server` timeout defaults and small-registry TLS guidance; record target values for `cmd/registry/main.go` and `README.md`.
- [x] 1.2 Add RED tests in `cmd/registry/main_test.go` for `PublicURL`, TLS cert/key pairing, HTTPS-vs-HTTP mode mismatch, and derived `/auth/token` realm validation.
- [x] 1.3 Add RED tests in `cmd/registry/main_test.go` for safer `bootstrap-admin` secret precedence and discouraged `-password` compatibility messaging.

## Phase 2: Runtime Hardening Implementation

- [x] 2.1 Extend `serveConfig` and bootstrap config parsing in `cmd/registry/main.go` with `PublicURL`, TLS inputs, bounded shutdown/read-header settings, and safer secret input flags.
- [x] 2.2 Implement `normalizeRuntimeConfig` in `cmd/registry/main.go` to derive the canonical token realm and fail fast on scheme/host/port/path or TLS-mode mismatches.
- [x] 2.3 Update `serve` in `cmd/registry/main.go` to build bounded `http.Server` settings, choose `Serve` vs `ServeTLS`, and enforce finite shutdown behavior.
- [x] 2.4 Replace the nil-client fallback in `internal/tui/admin_client.go` with a bounded default timeout; keep injected clients untouched.

## Phase 3: Integration and Verification

- [x] 3.1 Add GREEN/integration tests in `cmd/registry/main_test.go` for HTTPS startup, explicit local HTTP startup, bounded shutdown, and bootstrap guidance output.
- [x] 3.2 Add `internal/tui/admin_client_test.go` coverage for bounded default timeout and injected-client preservation.
- [x] 3.3 Update `docker-compose.yml` and `docs/verification/scripts/docker-push-pull-smoke.sh` for HTTPS-first runtime plus explicit HTTP local/dev verification.

## Phase 4: Documentation and Cleanup

- [x] 4.1 Update `README.md` with canonical `PublicURL`, TLS setup, startup failure cases, and preferred bootstrap secret entry guidance.
- [x] 4.2 Remove insecure/default examples superseded by the hardened flow and verify all operator-facing commands stay consistent across docs and compose.
