# Apply Progress: Registry Container Mode

**Scope of this record**: PR #1 (base) only — `feature/registry-container-mode-01-image-publish`, targeting `feature/registry-container-mode`. PR #2 (3a, anonymous smoke script) and PR #3 (3b, Postgres-auth smoke script) are separate, later apply batches; their tasks (Phase 5a, 5b, 6.1b, 6.3a, 6.3b, and CI-verification task 4.4) are **not started** and are out of scope for this record.

## Mode

Strict TDD (Phase 1 only — the sole Go code change in this PR). Standard mode for Dockerfile/YAML/CI/README work (no test runner applicable to those file types).

## Completed Tasks — PR #1 (base)

### Phase 1: Healthcheck Subcommand (Strict TDD)
- [x] 1.1 RED: table-driven tests in `cmd/regixtry/healthcheck_test.go`
- [x] 1.2 GREEN: `healthcheck` subcommand in `cmd/regixtry/main.go`
- [x] 1.3 GREEN: wired into `runWithIO` switch + no-args error string
- [x] 1.4 REFACTOR: `go vet ./...` and `gofmt -l .` clean

### Phase 2: Dockerfile Multi-Stage Rewrite
- [x] 2.1 `runtime-base` stage
- [x] 2.2 `release` stage
- [x] 2.3 `build` + `dev` stages, `dev` last
- [x] 2.4 Local verification: `docker build .` and `docker build --target=release .` both succeed

### Phase 3: GoReleaser Multi-Arch Image Publishing
- [x] 3.1 `dockers` block (amd64/arm64, buildx, `--target=release`, OCI labels)
- [x] 3.2 `docker_manifests` block (`{{ .Tag }}` always-push + 3 floating tags `skip_push: auto`)

### Phase 4: CI Workflow — Permissions, Login, Smoke-Step Wiring
- [x] 4.1 `packages: write`
- [x] 4.2 `docker/login-action@v3`, `docker/setup-qemu-action@v3`, `docker/setup-buildx-action@v3`, login first
- [x] 4.3 Scaffolded "Run container smoke verification" step (expected to fail in isolation on this branch until PR #2 adds the script — matches the chain note)

### Phase 6.1a: README — Anonymous Container Usage
- [x] 6.1a `## Run as a container` section added after `## Install`, anonymous-only

### Phase 6.2/6.4: PR #1 Verification
- [x] 6.2 `go test ./...` — all packages pass
- [x] 6.4 `docker-compose.yml` build verified via equivalent `docker build .` (see Issues Found — `docker compose build` itself failed in this sandbox for an environment reason, not a Dockerfile defect)

## TDD Cycle Evidence (Phase 1)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 1.1–1.3 | `cmd/regixtry/healthcheck_test.go` | Unit (httptest) | ✅ N/A (new file); ran full `cmd/regixtry` package baseline first (`ok`, 2.375s) before any edit | ✅ Written (compile failed: `undefined: runHealthcheck/healthcheckConfig/parseHealthcheckConfig`) | ✅ Passed (`go test -run 'TestRunHealthcheck\|TestParseHealthcheckConfig\|TestRunWithIOHealthcheck\|TestRunWithIONoArgsListsHealthcheckSubcommand' -v` → all PASS, 0.029s) | ✅ 7 test functions / 10 assertions: status-mapping table (200/401/500), connection-refused, timeout, flag defaults, flag overrides, `runWithIO` wiring (200/500), no-args error string | ✅ Clean — `go vet ./...` and `gofmt -l .` both empty after |
| 1.4 | (same) | — | — | — | — | — | ✅ No changes needed beyond the above; confirmed still green with full `go test ./cmd/regixtry/...` (2.613s) and full `go test ./...` (all packages `ok`) |

### Test Summary
- **Total tests written**: 7 test functions (10 sub-case assertions: 3 status-mapping cases + 2 wiring cases + 5 standalone functions)
- **Total tests passing**: 10/10
- **Layers used**: Unit (10), Integration (0), E2E (0)
- **Approval tests**: None — no refactoring of existing behavior, only new code
- **Pure functions created**: 0 (the healthcheck necessarily performs I/O — an HTTP probe — so it is not a candidate for a pure function; kept minimal and directly testable via `httptest`)

**Bug found and fixed during RED→GREEN**: my first draft of `TestRunHealthcheckTimeoutIsUnhealthy` deadlocked — `defer server.Close()` blocked waiting for an outstanding request while the handler was blocked on `<-release`, and `release` was only closed via `t.Cleanup` (which runs *after* deferred calls, not before). Fixed by deferring `close(release)` after `defer server.Close()` so LIFO ordering closes `release` first. Documented inline in the test with a comment explaining the ordering.

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./cmd/regixtry/... -run 'TestRunHealthcheck\|TestParseHealthcheckConfig\|TestRunWithIOHealthcheck\|TestRunWithIONoArgsListsHealthcheckSubcommand' -v` → all PASS (0.029s); full `go test ./cmd/regixtry/...` → `ok` (2.613s); full `go test ./...` → all 18 packages `ok` |
| Runtime harness command/scenario and exact result | `docker build .` (implicit `dev` target) → success; `docker build --target=release .` (locally-built stub `regixtry` binary at context root, removed after) → success. Release image run: `docker exec <cid> id -u` → `65532` (non-root confirmed); `docker exec <cid> regixtry healthcheck -url http://127.0.0.1:5000/v2/` → exit 0; `docker inspect -f '{{ .State.Health.Status }}'` → `healthy`. Host-external reachability (`-p` port-forward and bridge-IP curl) could NOT be verified: a stock `nginx:alpine` control container was equally unreachable from the host in this sandbox, confirming a sandbox-wide network isolation limitation, not a Dockerfile defect. `docker compose build regixtry` failed with `COPY failed: ... stat regixtry: file does not exist` — root-caused to this sandbox's Debian-packaged `docker.io` 20.10.24 client lacking the `docker-buildx-plugin` (Compose prints "Docker Compose requires buildx plugin to be installed" and falls back to a legacy non-DAG-aware builder that executes every stage in file order regardless of the target's actual dependency graph, hitting the `release` stage's `COPY regixtry` even when building the unrelated `dev` target). The functionally-equivalent `docker build .` (exactly what `docker-compose.yml`'s `build: {context: ., dockerfile: Dockerfile}` declares, no target override) succeeds, because the daemon here is a modern BuildKit-default Engine (29.7.2) reached transparently through plain `docker build`. GitHub Actions' `ubuntu-latest` ships buildx by default and Phase 4 already adds `docker/setup-buildx-action@v3`, so the actual release pipeline is unaffected. |
| Rollback boundary | Revert this branch (`feature/registry-container-mode-01-image-publish`) entirely. Touches only `cmd/regixtry/main.go`, `cmd/regixtry/healthcheck_test.go` (new), `Dockerfile`, `.goreleaser.yaml`, `.github/workflows/release.yml`, and the README's new `## Run as a container` section — nothing outside those paths. Each phase landed as its own commit (healthcheck / Dockerfile / goreleaser / CI / README), so a partial revert of any single concern is also possible via `git revert` of the specific commit. |

## Deviations from Design

1. **CMD gained an explicit `-public-url` flag**, not called out verbatim in design.md's File Changes table. `serve` has no built-in default for `-public-url` (only the `REGISTRY_PUBLIC_URL` env var); `normalizeRuntimeConfig` hard-fails with `"public URL is required"` before the listener even opens. Without this flag, the container would refuse to start on a bare `docker run` with no command override, directly violating the `container-release-image` spec's "Default Serve Entrypoint" requirement/scenario ("operator runs the image with no command override ... serve MUST bind and become reachable"). Verified by actually running the release image before and after this fix (see Runtime harness evidence above).
2. **README section omits the `bootstrap-admin -password-stdin` note** that tasks.md's 6.1a wording mentions, even though it doesn't apply to anonymous mode (bootstrap-admin only matters once Postgres-backed auth is configured). Followed the more specific, explicit orchestrator scope instruction for this run ("ONLY the anonymous docker run ... example and the note that install.sh/regixtry setup do not offer a container branch. Do NOT add the Postgres-auth recipe yet") over tasks.md's apparently-carried-over wording; task 6.1b (PR #3) is the one that actually adds `bootstrap-admin`/`-auth-postgres-dsn` content, mirroring the existing "Quick start with authentication and access control" section. Marked in tasks.md 6.1a with a note pointing here.
3. **README's anonymous `docker run` example explicitly passes `-allow-anonymous-pull -allow-anonymous-push`.** Traced `internal/ports/defaults.go`'s `configurableAccessController.Authorize`: when no auth backend is configured, both flags default `false`, so push/pull are rejected with 401 unless explicitly enabled — only the root `/v2/` liveness probe (which the healthcheck itself relies on) is exempt. Without these flags the documented `docker push`/`docker pull` example would 401, which would itself be untruthful. This differs from the (pre-existing, out-of-scope) top-level "Quick start (no auth)" section, which does not pass these flags — that section's accuracy was not part of this PR's scope and was left untouched.
4. **CI smoke step added with no flags** (`--image` only), not `--expect-multiarch`/`--auth-postgres`, per task 4.3's literal wording. `--auth-postgres` wiring into `release.yml` has no explicit owning task anywhere in tasks.md (5a/5b own the script's flag parsing, not the CI invocation), even though design.md's Testing Strategy table implies the final `release.yml` step should pass `--auth-postgres`. Flagged as a **tasks.md gap** — recommend confirming before PR #3 closes whether a task should be added to wire `--auth-postgres` (and possibly `--expect-multiarch`) into the CI step.

## Issues Found

1. `docker compose build` fails in this specific sandbox (missing `docker-buildx-plugin`); root-caused to a local tooling gap, not the Dockerfile. See Runtime harness evidence above for full detail and the `docker build .` equivalent-proof.
2. Host-external container network reachability (port-forward and bridge-IP) could not be verified in this sandbox at all, even for a stock control image — sandbox-wide limitation, unrelated to this change.

## Line Count vs. Estimate

tasks.md's per-PR estimate for PR #1 was **180-230 lines**. Actual: **377 insertions + 13 deletions = 390 changed lines** across `Dockerfile` (+43/-12), `.goreleaser.yaml` (+59), `.github/workflows/release.yml` (+23), `README.md` (+32), `cmd/regixtry/main.go` (+55/-1), `cmd/regixtry/healthcheck_test.go` (+165, new file) — **materially over estimate, roughly 1.7x**, driven mostly by the healthcheck test file (165 lines for full strict-TDD coverage of 5 required scenarios plus flag defaults/overrides plus wiring plus the no-args string, per the exact task 1.1 requirements) and by comment-heavy Dockerfile/GoReleaser documentation of load-bearing ordering decisions. This approaches/exceeds the skill's general 400-line default budget even though tasks.md rated this PR's own risk as "Low." Recommend the orchestrator note this for reviewer expectations; I did not trim test coverage or explanatory comments to hit the estimate, since the task list explicitly required the full scenario matrix and the design's stage-ordering rationale is genuinely load-bearing (misordering silently breaks `docker build .`/`docker-compose.yml`).

## Commits (this branch, in order)

1. `ae9309f` — feat(cmd): add regixtry healthcheck subcommand
2. `7e59459` — feat(docker): rewrite Dockerfile as multi-stage, non-root, healthchecked
3. `7cc02ea` — feat(release): publish multi-arch GHCR images via GoReleaser buildx
4. `1e1cf31` — ci(release): add GHCR login, buildx/QEMU setup, and smoke-step scaffold
5. `8275c31` — docs(readme): document docker run as a first-class container path

## Remaining Tasks (out of scope for this run — future PR #2/#3 batches)

- [ ] 5a.1–5a.5 — `container-release-smoke.sh` shared helpers + anonymous scenario (PR #2)
- [ ] 4.4 — verify the PR #1 CI smoke step resolves once the script exists (PR #2)
- [ ] 6.3a — local anonymous smoke verification (PR #2)
- [ ] 5b.1–5b.6 — `run_auth_scenario`, `--auth-postgres` flag (PR #3)
- [ ] 6.1b — Postgres-auth README recipe (PR #3)
- [ ] 6.3b — local auth smoke verification (PR #3)

## Status

12/12 PR #1 tasks complete. Not started: PR #2 (6 tasks + 1 shared verification) and PR #3 (7 tasks). Ready for `sdd-verify` on PR #1's scope, or a fresh `sdd-apply` batch for PR #2.
