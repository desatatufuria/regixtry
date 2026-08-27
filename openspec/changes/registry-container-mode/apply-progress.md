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

12/12 PR #1 tasks complete. 7/7 PR #2 tasks complete (see below). Not started: PR #3 (7 tasks). Ready for `sdd-verify` on PR #1+#2 scope, or a fresh `sdd-apply` batch for PR #3.

---

# PR #2 (3a) — targets PR #1 branch: `feature/registry-container-mode-02-smoke-anonymous`

**Scope of this addendum**: PR #2 (3a) only — smoke script shared helpers + `run_anonymous_scenario`, CI smoke-step wiring completion, anonymous local verification. PR #3 (3b, Postgres-auth) is a separate, later apply batch; its tasks (5b.1–5b.6, 6.1b, 6.3b) are **not started** and out of scope for this addendum.

## Mode

Standard mode — this PR adds no Go code (the task list explicitly says "there shouldn't be any new Go code in this PR," and none was needed). Strict TDD's spirit was applied to the bash script itself per the orchestrator's explicit instruction: every load-bearing assertion (non-root, health-poll timeout, blob persistence) was proven to actually fail before being trusted, using isolated RED-path drivers against the real Docker daemon, not just written and assumed correct.

## Completed Tasks — PR #2 (3a)

### Phase 5a: Container Smoke Script — Shared Helpers + Anonymous Scenario
- [x] 5a.1 Created `docs/verification/scripts/container-release-smoke.sh` with `fail()`, `cleanup()` trap, `wait_healthy()`, `http_status()`. `registry_token()` was **omitted entirely**, not stubbed (see Deviation 1).
- [x] 5a.2 `--image <ref>` (required) and `--expect-multiarch` (optional) flag parsing; `check_multiarch()` gracefully skips with a clear stderr message when `buildx` is unavailable, and separately when `imagetools inspect` fails (e.g. a local single-arch build).
- [x] 5a.3 `run_anonymous_scenario()`: named volume, detached run with `-allow-anonymous-pull -allow-anonymous-push`, non-root assertion, health poll, blob upload (POST start → PUT complete → HEAD verify), destroy, restart on the same volume, HEAD again for persistence proof.
- [x] 5a.4 Cleanup trap: registry container(s) → named volume, in that order, every step `|| true`.
- [x] 5a.5 CLI dispatch always calls `run_anonymous_scenario`; no `run_auth_scenario` dispatch exists yet (PR #3's job).
- [x] 4.4 Verified locally: the exact invocation the CI step now uses (`container-release-smoke.sh --image <ref> --expect-multiarch`) runs successfully end-to-end against a `--target=release` build.

### CI Workflow — Smoke-Step Wiring Completion
- [x] Replaced PR #1's scaffolded/placeholder smoke step in `.github/workflows/release.yml` with the real invocation: `container-release-smoke.sh --image "ghcr.io/desatatufuria/regixtry:${GITHUB_REF_NAME}" --expect-multiarch`. No `--auth-postgres` — that flag does not exist in this script yet (PR #3 adds it); the comment above the step now says so explicitly instead of the old "does not exist yet" placeholder note.

### Phase 6.3a: PR #2 Verification
- [x] 6.3a Ran `container-release-smoke.sh` locally against a `docker build --target=release .` image, twice (once without `--expect-multiarch`, once with) — both passed end-to-end. See Work Unit Evidence below for exact commands/output.

## Technical Decision: HTTP probing via network-namespace sharing, not host port-publishing

design.md's literal auth-scenario command snippet uses `-p 127.0.0.1:0:5000` (host port publish) and implies host-side `curl`. I deviated from that literal mechanism for **every** HTTP interaction in this script (`http_status()`, the blob POST/PUT calls): instead of publishing a port and curling from the host, every HTTP call runs through a short-lived `curlimages/curl:latest` sidecar started with `docker run --rm --network container:<target>`, which joins the target container's network namespace and reaches it via `127.0.0.1:5000` — exactly as the process inside the container sees itself.

**Why**: This sandbox's Docker daemon is real (no `docker compose`/`docker` mock), but host→container port-forwarding is broken sandbox-wide — confirmed independently in this run (curl to a published port returns immediate "connection refused," and curl to the container's bridge IP hangs indefinitely for 2+ minutes even against a stock `nginx:alpine` control container), matching PR #1's exact prior finding. The network-namespace-sharing technique works identically in this constrained sandbox and in normal CI (it never depends on host-to-container routing at all, only on the daemon's own ability to share a network namespace between two containers it manages directly), so it let me run genuine strict-discipline RED/GREEN proof against the real script instead of writing an untestable assumption. It also avoids host port-allocation/conflict concerns entirely on CI runners.

**This is a deviation from design.md's literal snippet**, flagged for reviewer awareness before PR #3 (which will extend this same file with the auth scenario and should use the same mechanism for consistency, not design's literal `-p` snippet, unless a maintainer overrides this decision).

## Bug found and fixed during script development: curl `-X HEAD` hangs against this server

While proving the blob-persistence assertion's RED path, `http_status()`'s original implementation (`curl -X HEAD ...`) **hung indefinitely** (reproduced, confirmed via `ps aux` showing a live `docker run ... -X HEAD ...` process over a minute old with no progress). Root cause: this server's `handleBlobRead` sets a real `Content-Length` header on HEAD responses (matching what the equivalent GET would send) and correctly writes no body, per RFC 7231 — but curl's `-X HEAD` (as opposed to `--head`/`-I`) does not tell curl's internal state machine to skip body-reading; curl still expects the server to send `Content-Length` bytes and blocks waiting for them, which a compliant server never sends for a HEAD request. Fixed by special-casing `http_status()`: when `method == "HEAD"`, use curl's `--head` flag instead of `-X HEAD`. Also added `--max-time 20` to every curl invocation in the script as defense-in-depth against any future hang blocking CI indefinitely. Verified the fix: re-ran the full anonymous scenario twice after the fix (once plain, once with `--expect-multiarch`), both passed end-to-end with no hang.

## Deviations from Design

1. **`registry_token()` omitted entirely, not stubbed.** The orchestrator's instructions explicitly left this as my call ("leave the function present but unused/minimal ... or omit it entirely if that reads cleaner"). I omitted it: an empty/unused function is dead code with no test coverage until PR #3 actually needs it, and PR #3's own task 5b.4 (Bearer token exchange) is the natural place to design its real signature (it will need to parse a JSON `token` field from an HTTP response, a shape driven entirely by that PR's own `run_auth_scenario` requirements, not guessable usefully today).
2. **HTTP probing mechanism**: network-namespace-sharing curl sidecar instead of design.md's literal `-p` host-port-publish snippet. See "Technical Decision" section above for full rationale. Functionally equivalent from the registry's point of view (same HTTP requests, same assertions); differs only in how the smoke script's own process reaches the container.
3. **`--max-time 20` added to every curl call**, not explicitly requested by any task, added as defense-in-depth after discovering the `-X HEAD` hang — a hang anywhere in this script would otherwise block a CI job indefinitely with no timeout.
4. **CI step's smoke-verification comment rewritten**, not just the `run:` line: PR #1's placeholder comment ("container-release-smoke.sh does not exist yet ... expected to fail until PR #2") is no longer true on this branch, so it was replaced with a comment scoping the *next* gap (`--auth-postgres` wiring belongs to PR #3), keeping the "why is this step shaped this way" trail intact for the next PR's reader.

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go test ./...` → unaffected, all 18 packages `ok` (no Go code touched by this PR). `go build ./...`, `go vet ./...`, `gofmt -l .` → all clean. |
| Runtime harness command/scenario and exact result | Built `docker build --target=release -t regixtry-smoke-local:test .` locally (succeeded, using a real amd64 binary built via `go build`). Ran `bash docs/verification/scripts/container-release-smoke.sh --image regixtry-smoke-local:test` → **PASS**: `container-release-smoke: anonymous scenario passed for regixtry-smoke-local:test`, exit 0. Re-ran with `--expect-multiarch` → **PASS**, plus the expected graceful skip: `container-release-smoke: skipping --expect-multiarch check (buildx plugin unavailable)` (this sandbox's Docker client lacks the buildx plugin — see PR #1's identical finding — so this exercises the "buildx unavailable" skip branch; the "buildx present but image is single-arch" skip branch is code-reviewed but could not be executed in this sandbox since buildx itself isn't installed here). Confirmed the trap cleanup removed every container and volume after both successful runs (`docker ps -a` / `docker volume ls` empty of `regixtry-smoke-*` names). |
| RED-path proof (strict-discipline verification requested by the orchestrator) | **Non-root assertion**: ran a plain root `debian:bookworm-slim` container and called `assert_non_root` against it → correctly failed: `expected Config.User 65532:65532 for red-nonroot-test, got ''`. **Health-poll timeout (unhealthy branch)**: container with `--health-cmd="exit 1"` → correctly failed in ~1s: `red-health-test reported unhealthy before becoming healthy`. **Health-poll timeout (never-reports branch)**: container with `--no-healthcheck` (Health.Status stays empty forever) and a 3s budget → correctly failed at ~3s: `red-health-timeout-test did not become healthy within 3s (last status: '')`. **Blob persistence**: ran the real upload flow against volume A, then deliberately restarted on a *different*, empty volume B instead of A → correctly failed: `blob HEAD after restart on the same volume returned 404, expected 200 (persistence proof)`. All four RED runs used the shipped script's actual functions (sourced from the real file, minus only the trailing `main "$@"` call), not reimplementations. |
| Environment limitation encountered (documented, not worked around silently) | Host→container port-forwarding is broken sandbox-wide in this environment (independently reproduced: `curl` to a `-p`-published port returns immediate connection-refused; `curl` to the container's bridge IP hangs 2+ minutes even for a stock `nginx:alpine` control image) — this is the same limitation PR #1's agent already found and documented. Rather than fabricate a pass around it, I changed the script's actual HTTP-reaching mechanism (network-namespace sharing, see Technical Decision above) to one that is unaffected by this limitation and equally valid in real CI, so the *shipped* script's real assertions could be genuinely exercised end-to-end here rather than only asserted to work in theory. Docker itself required `sudo -n docker` (passwordless sudo confirmed available) rather than direct socket access for this user; a local `PATH`-shim wrapper (`docker` → `sudo -n docker "$@"`) was used only for local verification and is not part of the shipped script or CI wiring, since GitHub Actions' `ubuntu-latest` runners give the job direct socket access. |
| Rollback boundary | Revert `docs/verification/scripts/container-release-smoke.sh` (script did not exist before this PR — a clean delete fully reverts it) and revert the CI wiring hunk in `.github/workflows/release.yml` (restores PR #1's placeholder step). Nothing outside those two files is touched. |

## Line Count vs. Estimate

tasks.md's per-PR estimate for PR #2 was **150-200 lines**. Actual: **`docs/verification/scripts/container-release-smoke.sh`** is a new file, 247 lines; **`.github/workflows/release.yml`** changed +4/-6 (10 changed lines) — **257 changed lines total**, moderately over the tasks.md estimate (~1.3x) but well under this run's declared 350-line ledger budget (73% of budget used). The overrun versus tasks.md's own estimate is driven by: (a) generous inline comments documenting two non-obvious decisions (the network-namespace-sharing HTTP mechanism, and the `-X HEAD` hang root cause) that a future PR #3 reader will need, matching PR #1's own precedent of not trimming load-bearing explanatory comments to hit an estimate; (b) the `--max-time 20` defense-in-depth addition; (c) `check_multiarch()`'s two distinct graceful-skip branches (buildx-unavailable vs. imagetools-inspect-failed) each needing their own clear message per task 5a.2's explicit requirement.

## Commits (this branch, in order)

1. `feat(scripts): add container-release-smoke.sh anonymous scenario` — shared helpers + `run_anonymous_scenario`
2. `ci(release): wire real container-release-smoke.sh into release workflow` — replaces PR #1's placeholder step
3. `docs(sdd): record PR #2 apply-progress for registry-container-mode` — this addendum

(Exact hashes recorded in the final response after commit.)

## Remaining Tasks (out of scope for this run — future PR #3 batch)

- [ ] 5b.1–5b.6 — `run_auth_scenario`, `--auth-postgres` flag (PR #3)
- [ ] 6.1b — Postgres-auth README recipe (PR #3)
- [ ] 6.3b — local auth smoke verification (PR #3)

## Status (PR #2 addendum)

7/7 PR #2 tasks complete (5a.1, 5a.2, 5a.3, 5a.4, 5a.5, 4.4, 6.3a) plus the CI-wiring completion (the `release.yml` step edit itself, tracked as part of Phase 5a's scope, not a separately numbered tasks.md item). Ready for `sdd-verify` on PR #1+PR #2 combined scope, or a fresh `sdd-apply` batch for PR #3.
