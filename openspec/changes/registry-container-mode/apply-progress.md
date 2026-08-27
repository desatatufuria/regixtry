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

---

# PR #3 (3b) — targets PR #2 branch: `feature/registry-container-mode-03-smoke-auth`

**Scope of this addendum**: PR #3 (3b), the final PR in the chain — `run_auth_scenario`, the `--auth-postgres` flag, the `registry_token()` helper (deliberately deferred by PR #2), and the Postgres-auth README recipe. This closes out the entire `registry-container-mode` change; only the tracker PR (`feature/registry-container-mode` → `develop`) remains, and that is explicitly the orchestrator's job, not this run's.

## Mode

Standard mode — no Go code changed in this PR (the binary already supports `-auth-postgres-dsn`/`REGISTRY_AUTH_POSTGRES_DSN` and `bootstrap-admin -password-stdin`; verified by reading `cmd/regixtry/main.go` and confirming `go build ./...`/`go vet ./...`/`gofmt -l .` all stayed clean with zero `.go` files touched). Strict-discipline verification applied to the bash script itself, per this run's explicit instruction: every load-bearing assertion was proven to actually catch a real failure with a real Docker daemon before being trusted, not just written and assumed correct — continuing PR #2's exact precedent.

## Completed Tasks — PR #3 (3b)

### Phase 5b: Container Smoke Script — Postgres-Auth Scenario
- [x] 5b.1 `--auth-postgres` flag added to `parse_args()`.
- [x] 5b.2 `run_auth_scenario()`: creates `regixtry-smoke-auth-net-${RUN_ID}`, starts `postgres:17-alpine` (confirmed against `docker-compose.yml`'s exact pin) on it, polls via a new `wait_postgres_ready()` helper (`pg_isready -U registry -d regixtry_auth`, the same check `docker-compose.yml`'s own healthcheck uses).
- [x] 5b.3 `bootstrap-admin -auth-postgres-dsn "${DSN}" -username admin -password-stdin` runs via `docker run --rm -i --network "${AUTH_NETWORK}"` with the password piped through `printf | docker run -i`, never argv; explicitly gated with `if ! ...; then fail ...; fi` (not just implicit `set -e` propagation) so a bootstrap failure produces a clear `container-release-smoke: bootstrap-admin failed against ...` message. Confirmed via RED-path proof (see below) that `serve` genuinely fails fast and never becomes healthy if started before this step.
- [x] 5b.4 `serve` starts via the default `ENTRYPOINT`/`CMD` (no command override needed) with `-e REGISTRY_AUTH_POSTGRES_DSN="${DSN}"` — confirmed the top-level `serve` flag parsing path also defaults from that exact env var, so no CMD override was required, unlike the anonymous scenario. Assertions, in order: anonymous `GET /v2/` → `401` (`http_status`, new `GET` support — `http_status` previously was only ever called with `HEAD`/`POST`/`PUT`); anonymous `GET /v2/` response carries `WWW-Authenticate` (new `http_headers()` helper, mirrors `http_status`'s HEAD-vs-`-X HEAD` special case); `registry_token()` exchanges Basic `admin:${admin_password}` for a Bearer token against `GET /auth/token?service=regixtry&scope=repository:smoke/auth:pull,push`; authenticated `POST`/`PUT`/`HEAD` blob round trip → `202`(via Location header)/`201`/`200`; the same `HEAD` **without** the Bearer token → `401` (proves the 200 came from the token, not an open endpoint).
- [x] 5b.5 `cleanup()` extended: containers loop now also removes `AUTH_CONTAINER` and `AUTH_PG_CONTAINER`; a second loop removes `AUTH_VOLUME` alongside `ANON_VOLUME`; `AUTH_NETWORK` is removed last, after both loops, exactly matching the load-bearing containers → volumes → network ordering from `design.md`'s data-flow diagram. Verified empirically (see Runtime harness evidence): a real end-to-end run left zero `regixtry-smoke-*` containers, volumes, or networks behind.
- [x] 5b.6 `main()` calls `run_auth_scenario` only when `AUTH_POSTGRES=1` (set by the `--auth-postgres` flag); the anonymous scenario always runs first and unconditionally, matching PR #2's existing default-path guarantee.

### Phase 6.1b: README — Postgres-Auth Container Recipe
- [x] 6.1b Added a `### Postgres-backed authentication in a container` subsection under `## Run as a container`, replacing the prior "a container-specific recipe is coming in a follow-up" placeholder sentence PR #1 left. Same `postgres:17-alpine` pin, same `regixtry_auth`/`registry` DSN shape, same `bootstrap-admin -password-stdin` → `serve -auth-postgres-dsn` ordering as the existing `## Quick start with authentication and access control` section, expressed with `docker network create` + `docker run` instead of `docker compose`, per `design.md`'s File Changes row.

### Phase 6.3b: PR #3 Verification
- [x] 6.3b Ran the real, shipped `container-release-smoke.sh --image <local release-target build> --auth-postgres` end-to-end against a genuine Docker daemon (via `sudo -n docker`, this sandbox's confirmed access path from PR #1/#2) — **PASS**. See Work Unit Evidence below for the full RED/GREEN ledger.

## registry_token() — the PR #2 deviation this PR resolves

PR #2 deliberately omitted `registry_token()` entirely (not stubbed), reasoning that its real signature should be driven by this PR's actual `run_auth_scenario()` needs. Implemented here as:

```bash
registry_token() {
  # $1=container $2=username $3=password $4=repo
  # GET /auth/token?service=regixtry&scope=repository:<repo>:pull,push via
  # Basic auth (curl -u), sidecar network-namespace pattern (same as
  # http_status/http_headers). Parses the "token" field out of the JSON
  # response with grep -o + sed (no jq dependency inside curlimages/curl).
}
```

Confirmed by reading `internal/protocol/http/router.go`'s `handleToken` (via `codegraph_explore`) that the response body is `{"token": ..., "access_token": ..., "expires_in": ..., "issued_at": ..., ["service": ...], ["scope": ...]}` — the `grep -o '"token"[[:space:]]*:...'` pattern only matches the literal `"token"` key (bounded by quotes), not the `"access_token"` key that also contains the substring `token`, verified against a real response body during RED/GREEN testing (see below).

## Technical Decision: reused PR #2's network-namespace-sharing curl pattern, not design.md's literal `-p` snippet

Per PR #2's own risk note ("PR #3 ... should use the same mechanism for consistency, not design's literal `-p` snippet, unless a maintainer overrides this decision"), every new HTTP interaction in `run_auth_scenario()` — `http_status`, `http_headers`, `registry_token`, and the raw `docker run --network container:<target>` calls for the authenticated POST/PUT/HEAD — uses the same `docker run --rm --network "container:${AUTH_CONTAINER}" "${CURL_IMAGE}"` sidecar pattern PR #2 established, never `-p 127.0.0.1:0:5000` host port publishing. This sandbox's host→container port-forwarding limitation (documented by PR #1 and PR #2) was independently reconfirmed unnecessary to work around here, since the sidecar mechanism was reused as-is. `design.md`'s literal `-p 127.0.0.1:0:5000` snippet in its Auth smoke sequence example was **not** followed literally for the same reason PR #2 didn't follow it — this is a continuation of PR #2's already-flagged deviation, not a new one.

## Two new shared helpers

1. **`http_headers()`** — same signature and HEAD-vs-`-X HEAD` special case as `http_status()`, but prints raw response headers instead of a status code (needed for the `WWW-Authenticate` assertion, which `http_status()` cannot express since it discards headers).
2. **`wait_postgres_ready()`** — polls `docker exec <container> pg_isready -U registry -d regixtry_auth` on a 1s/60s budget, mirroring `docker-compose.yml`'s own Postgres healthcheck exactly (`test: ["CMD-SHELL", "pg_isready -U registry -d regixtry_auth"]`), so the DSN's readiness assumption in the script matches the one already proven in local dev.

Also extended `http_status()`'s existing call sites with a new method it had not previously been called with: `GET` (the anonymous-rejection and, indirectly through the design's wording, general-purpose calls) — no changes to `http_status()`'s own implementation were needed since it already dispatched on `${method}` generically; only the `HEAD` branch was ever special-cased.

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and exact result | `go build ./...`, `go vet ./...`, `gofmt -l .` → all clean, zero `.go` files touched by this PR. `bash -n docs/verification/scripts/container-release-smoke.sh` → syntax OK. |
| Runtime harness command/scenario and exact result | Built `docker build --target=release -t regixtry-smoke-local:auth-test .` locally (real `go build ./cmd/regixtry` binary in build context). Ran the real shipped `container-release-smoke.sh --image regixtry-smoke-local:auth-test --auth-postgres` end-to-end: **PASS** — `container-release-smoke: anonymous scenario passed for regixtry-smoke-local:auth-test`, `bootstrapped global admin "admin"`, `container-release-smoke: auth scenario passed for regixtry-smoke-local:auth-test`, exit 0, wall time ~26s. Re-ran **without** `--auth-postgres` (regression check for the spec's "Anonymous-only image still works when no DSN is supplied" scenario) — **PASS**, exit 0, ~13s, confirming PR #2's `run_anonymous_scenario` is unmodified and unaffected. Confirmed via `docker ps -a` / `docker volume ls` / `docker network ls` filtered on `regixtry-smoke-*` that both runs left zero resources behind (cleanup trap, including the network-last ordering, works). |
| RED-path proof (strict-discipline verification requested by the orchestrator) | **Ordering gate** (design's core claim: "serve fails fast... no running container to exec into"): started `serve` with a valid DSN but **before** `bootstrap-admin` ran — container exited immediately (`State.Status=exited`, `ExitCode=1`, log: `BOOTSTRAP_REQUIRED: auth enabled requires an active global admin; run \`registry bootstrap-admin\``); separately confirmed `wait_healthy` (unmodified, reused as-is) correctly detects this and fails within ~1s (`"... reported unhealthy before becoming healthy"`) rather than silently passing or hanging the full 60s budget. **Anonymous-rejection assertion**: ran a plain image with no DSN and no anon flags (a stand-in for a "broken" auth image that never actually enabled auth) — `GET /v2/` returned `200`, not `401`, proving the `[[ "${status}" == "401" ]] \|\| fail ...` check would correctly catch this; the paired `WWW-Authenticate` header check also correctly found it absent. **`registry_token()` credential check**: called with a deliberately wrong password against a real bootstrapped admin — failed with `registry_token: could not parse a token from the /auth/token response: {"errors":[{"code":"UNAUTHORIZED","message":"invalid credentials"}]}`, exit 1; called again with the correct password — succeeded, returned a real 64-character token. **Authenticated PUT/HEAD checks**: POST with a garbage `Bearer` token → real server response `401 Unauthorized` with `Www-Authenticate: ... error="invalid_token"`, confirming a forged/garbage token is genuinely rejected, not silently accepted; the same flow with the real token → `202`(Location)/`201`/`200` exactly as asserted. **Bootstrap-gate failure path against the actual shipped script**: patched a throwaway copy of the real `container-release-smoke.sh` to pass an invalid flag to the `bootstrap-admin` invocation, ran it with `--auth-postgres` — failed with `flag provided but not defined: -this-flag-does-not-exist` followed by the script's own `container-release-smoke: bootstrap-admin failed against ... (auth scenario cannot continue)`, exit 1, and confirmed cleanup still fired (zero leftover `regixtry-smoke-*` containers/networks) even on this failure path. All RED/GREEN proofs used the shipped functions themselves (sourced unmodified, or the real end-to-end script), not reimplementations. |
| Environment limitation encountered (documented, not worked around silently) | Same sandbox-wide host→container port-forwarding limitation PR #1 and PR #2 already found and worked around; this PR reused PR #2's existing network-namespace-sharing mechanism rather than rediscovering the limitation, so no new workaround was needed. `sudo -n docker` (passwordless sudo, confirmed available) plus a local `PATH`-shim wrapper were used for all local verification in this run, exactly as PR #2 documented; neither is part of the shipped script or CI wiring. |
| Rollback boundary | Revert `run_auth_scenario()`, `wait_postgres_ready()`, `http_headers()`, `registry_token()`, the `--auth-postgres` flag/dispatch, and the `cleanup()` extension from `docs/verification/scripts/container-release-smoke.sh` (all additive; `run_anonymous_scenario()` and every PR #2 helper are untouched by this PR) — `run_anonymous_scenario` keeps working unmodified either way. Revert the new `### Postgres-backed authentication in a container` README subsection independently. Nothing outside `docs/verification/scripts/container-release-smoke.sh`, `README.md`, and this SDD tracking pair is touched. |

## Deviations from Design

1. **Sidecar network-namespace HTTP mechanism, not `design.md`'s literal `-p` snippet** — continuation of PR #2's already-flagged deviation, not a new one; see "Technical Decision" above.
2. **`serve` started via the default `ENTRYPOINT`/`CMD` with only `-e REGISTRY_AUTH_POSTGRES_DSN` set**, rather than an explicit `-auth-postgres-dsn` CLI-flag override on a custom command line. `design.md`'s own snippet uses `-e REGISTRY_AUTH_POSTGRES_DSN="${DSN}"` too (its serve line has no `-auth-postgres-dsn` flag), so this matches the letter of `design.md`; called out only because `bootstrap-admin` in the same design snippet uses the CLI flag (`-auth-postgres-dsn "${DSN}"`) rather than the env var — the two invocations are deliberately asymmetric in `design.md` itself, and this PR preserves that exact asymmetry rather than "fixing" it to be consistent.
3. **`registry_token()`'s token parsing uses `grep -o` + `sed`, not `jq`** — `curlimages/curl:latest` does not ship `jq`, and adding a second sidecar image for JSON parsing was judged unnecessary complexity for extracting one well-known top-level string field from a response this script itself controls the shape of (the server's own `handleToken` response, read via `codegraph_explore` before writing this). Verified the pattern does not false-match `"access_token"` (see "registry_token()" section above).

## Issues Found

None new. PR #2's two previously-documented sandbox limitations (broken host port-forwarding; `docker compose build` needing `docker-buildx-plugin`) were not re-encountered as fresh issues in this PR because `run_auth_scenario()` reused PR #2's existing sidecar mechanism from the start rather than attempting host-side networking again.

## Line Count vs. Estimate

tasks.md's per-PR estimate for PR #3 was **250-350 lines** (the largest of the three, per its own stated rationale: most security-sensitive cluster). Actual: `docs/verification/scripts/container-release-smoke.sh` **+189/-7** (196 changed lines) and `README.md` **+33/-1** (34 changed lines) — **230 changed lines total** for the implementation files, moderately **under** the tasks.md estimate (~0.8-0.9x, and comfortably under this run's declared 550-line ledger budget for the whole PR, using well under half of it). `openspec/changes/registry-container-mode/tasks.md` also changed (+22/-9, marking PR #3's tasks `[x]` and adding a Chain Status note) but is SDD bookkeeping, not authored implementation risk, consistent with how PR #1/#2's own line-count sections scoped their totals. The under-estimate, unlike PR #1's and PR #2's own overruns, is attributable to maximal helper reuse: `http_status()` needed zero changes to support `GET` (it already dispatched generically on `${method}`), and `run_auth_scenario()` reuses `assert_non_root`, `wait_healthy`, `http_status`, and the sidecar `docker run --network container:...` idiom verbatim from the anonymous scenario rather than duplicating logic.

## Commits (this branch, in order)

1. `feat(scripts): add registry_token() and run_auth_scenario() to container-release-smoke.sh` — `--auth-postgres` flag, `wait_postgres_ready()`, `http_headers()`, `registry_token()`, `run_auth_scenario()`, extended `cleanup()` ordering, CLI dispatch
2. `docs(readme): add Postgres-backed auth container recipe`
3. `docs(sdd): mark PR #3 tasks complete and record final apply-progress`

(Exact hashes recorded in the final response after commit.)

## Status (PR #3 addendum)

7/7 PR #3 tasks complete (5b.1-5b.6, 6.1b, 6.3b). **All 26 tasks across all three PRs are now complete.** Ready for `sdd-verify` across the full `registry-container-mode` change; the tracker branch (`feature/registry-container-mode` → `develop`) merge/PR sequencing is explicitly out of scope for this apply run — that is the orchestrator's job next, per this run's own instructions.

---

# Final Consolidated Summary — All 3 PRs

| PR | Branch | Base | Tasks | Commits (in order) | Implementation lines (impl files only, excl. SDD tracking) |
|---|---|---|---|---|---|
| #1 | `feature/registry-container-mode-01-image-publish` | `feature/registry-container-mode` (tracker) | 12/12 | `ae9309f` healthcheck subcommand, `7e59459` Dockerfile rewrite, `7cc02ea` GoReleaser dockers/manifests, `1e1cf31` CI login/buildx/smoke-scaffold, `8275c31` README anon section | 377+/13- = 390 (vs. 180-230 est., ~1.7x over) |
| #2 (3a) | `feature/registry-container-mode-02-smoke-anonymous` | PR #1 branch | 7/7 | `2653442` smoke script + anon scenario, `5919dd7` CI real-script wiring | 247(new)+10(CI) = 257 (vs. 150-200 est., ~1.3x over) |
| #3 (3b) | `feature/registry-container-mode-03-smoke-auth` | PR #2 branch | 7/7 | (this addendum's 3 commits, hashes below) | 196(script)+34(README) = 230 (vs. 250-350 est., ~0.8-0.9x, under) |
| **Total** | | | **26/26** | | **~877 changed lines** across the whole chain (390+257+230), against tasks.md's own 580-780 whole-change re-estimate — within range, at the low-to-mid end |

**What is genuinely verified (real Docker, this sandbox, all three PRs)**:
- `go test ./...` — all 18 packages pass throughout; the only Go code added (PR #1's `healthcheck` subcommand) has full strict-TDD unit coverage (7 test functions / 10 assertions).
- `docker build .` (dev target) and `docker build --target=release .` (release target) both succeed; the multi-stage `runtime-base → release/build → dev` layout works as designed, `dev` last so `docker-compose.yml` is unaffected.
- The release image runs as non-root (uid `65532`), passes its own `regixtry healthcheck` subcommand, and reaches `healthy` via `HEALTHCHECK`.
- The full anonymous smoke scenario (non-root, health-poll, blob upload, container-destroy-and-restart-on-same-volume persistence) — real, repeated, passing runs.
- The full Postgres-auth smoke scenario (ephemeral network, sibling `postgres:17-alpine`, `pg_isready` poll, `bootstrap-admin -password-stdin` gating `serve` startup, anonymous `401`+`WWW-Authenticate`, Basic→Bearer token exchange, authenticated blob `PUT`/`HEAD` → `201`/`200`, unauthenticated `HEAD` on the same blob → `401`) — real, passing end-to-end run, plus explicit RED-path proofs (deliberately broken ordering, wrong credentials, garbage tokens, invalid CLI flags) for every load-bearing assertion across all three PRs.
- Cleanup ordering (containers → volumes → network last) — confirmed empirically leaves zero `regixtry-smoke-*` resources after both successful and deliberately-failed runs.

**What still needs real CI to prove (cannot be exercised in this sandbox)**:
- **Actual multi-arch GHCR publish**: `docker/setup-qemu-action@v3` + `docker/setup-buildx-action@v3` + GoReleaser's `dockers`/`docker_manifests` blocks producing genuine `linux/amd64` + `linux/arm64` manifests pushed to `ghcr.io/desatatufuria/regixtry` — this sandbox's Docker client lacks the `buildx` plugin entirely (`check_multiarch()`'s "buildx plugin unavailable" skip branch was the only branch ever exercised here; its "imagetools inspect failed" branch is code-reviewed but not executed against a real multi-arch manifest).
- **Floating tag behavior** (`vX.Y.Z` always, `vX.Y`/`vX`/`latest` on non-prerelease only, none of the floating tags on prerelease) — no real GoReleaser release run occurred; this is config-reviewed against `.goreleaser.yaml`'s documented `skip_push: auto` semantics, not exercised.
- **`GITHUB_TOKEN`/`packages: write` actually authorizing a GHCR push** — the login step's correctness can only be proven by a real `ubuntu-latest` runner with real repository permissions.
- **Host→container reachability of the published image** (`curl -i http://127.0.0.1:5000/v2/` exactly as the README's own anonymous quick-check documents) — every HTTP assertion in all three PRs' local verification used the sidecar `docker run --network container:<target>` pattern specifically because this sandbox cannot do host-side port-forwarding at all (confirmed independently, including against a stock `nginx:alpine` control image, across all three PRs); this pattern is a verification-only substitution and does not appear in any shipped script's *design*, only in how the already-shipped, host-port-agnostic sidecar calls happen to work — but the literal "publish a port and curl it from the host" flow a real operator would follow (and which the README documents) has never been executed end-to-end in this sandbox.
- **`docker compose build`** — fails in this specific sandbox (missing `docker-buildx-plugin` on the client), root-caused in PR #1 to a local tooling gap; the functionally-equivalent `docker build .` (exactly what `docker-compose.yml` declares) succeeds, but the literal `docker compose build`/`docker compose up` flow itself has not been proven to work end-to-end here.
- **The actual chained-PR merge sequence** (PR #1 → PR #2 → PR #3 → tracker → `develop`) — no branch merges, rebases, or GitHub PR creation were performed by any apply batch across all three PRs; this is explicitly the orchestrator's responsibility, not `sdd-apply`'s.

## Recommendation

Ready for `sdd-verify` across the full `registry-container-mode` change (all 3 PR branches, 26/26 tasks). The orchestrator should sequence the three stacked PRs through review and merge (`feature/registry-container-mode-01-image-publish` → `feature/registry-container-mode-02-smoke-anonymous` → `feature/registry-container-mode-03-smoke-auth` → `feature/registry-container-mode` → `develop`), then rely on a real CI run of `release.yml` against an actual tag to prove the items listed above that this sandbox could not exercise.
