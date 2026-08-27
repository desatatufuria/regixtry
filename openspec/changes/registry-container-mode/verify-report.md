```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:ef1c1e251977485af676d13f4e3aeabddf3a65ac11b8c866bc889ddc59d2bb19
verdict: fail
blockers: 1
critical_findings: 1
requirements: 5/8
scenarios: 6/10
test_command: go test ./...
test_exit_code: 0
test_output_hash: sha256:f79c89d19b578fc088559f9a2270dba4ac91065155fc3669885646df7e65bcbf
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: registry-container-mode
**Version**: N/A
**Mode**: Strict TDD (Go code, Phase 1 only) / Standard (Dockerfile, YAML, CI, bash, docs)

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 26 |
| Tasks complete | 26 |
| Tasks incomplete | 0 |

Verified `tasks.md` directly: zero `[ ]` unchecked boxes across all three PR sections (PR #1: 12/12, PR #2: 7/7, PR #3: 7/7).

### Build & Tests Execution
**Build**: PASS
```text
$ go build ./...
(no output)
```

**Tests**: PASS — full suite, all 18 packages
```text
$ go test ./...
ok  	regixtry/cmd/regixtry
ok  	regixtry/internal/app/auth
ok  	regixtry/internal/app/regixtry
ok  	regixtry/internal/app/scanning
ok  	regixtry/internal/domain/auth
ok  	regixtry/internal/domain/regixtry
ok  	regixtry/internal/domain/signing
ok  	regixtry/internal/infra/auth/postgres
ok  	regixtry/internal/infra/cliprogress
ok  	regixtry/internal/infra/install/linux
ok  	regixtry/internal/infra/install/releases
ok  	regixtry/internal/infra/metadata/sqlite
ok  	regixtry/internal/infra/release
ok  	regixtry/internal/infra/scanning/gitleaks
ok  	regixtry/internal/infra/scanning/trivy
ok  	regixtry/internal/infra/storage/fsblob
ok  	regixtry/internal/ports
ok  	regixtry/internal/protocol/http
ok  	regixtry/internal/tui
```

**Static analysis**: PASS
```text
$ go vet ./...
(no output)
$ gofmt -l .
(no output)
```

**Docker build sanity** (real Docker daemon reachable in this environment via `sudo -n docker`, server 29.7.2):
```text
$ sudo -n docker build -t regixtry-verify-dev:test .          → SUCCESS (implicit `dev` target)
$ CGO_ENABLED=0 GOOS=linux go build -o ./regixtry ./cmd/regixtry   → local stub binary at context root
$ sudo -n docker build --target=release -t regixtry-verify-release:test .   → SUCCESS
```
Both builds succeed cleanly. The `release` target's `COPY regixtry /usr/local/bin/regixtry` correctly requires a pre-built binary at the build-context root — exactly the contract GoReleaser's buildx invocation satisfies at release time, so this is expected/correct behavior, not a defect.

**Real end-to-end smoke script execution** (ran the actual shipped `docs/verification/scripts/container-release-smoke.sh` against the locally built release image — not trusted from apply-progress alone):
```text
$ sudo -n bash docs/verification/scripts/container-release-smoke.sh --image regixtry-verify-release:test
container-release-smoke: anonymous scenario passed for regixtry-verify-release:test
(exit 0)

$ sudo -n bash docs/verification/scripts/container-release-smoke.sh --image regixtry-verify-release:test --auth-postgres
container-release-smoke: anonymous scenario passed for regixtry-verify-release:test
bootstrapped global admin "admin"
container-release-smoke: auth scenario passed for regixtry-verify-release:test
(exit 0)
```
Post-run cleanup verified empirically: `docker ps -a` / `docker volume ls` / `docker network ls` filtered on `regixtry-smoke*` returned zero leftover resources after both runs. This independently confirms apply-progress's claims for PR #2 and PR #3 — the anonymous and Postgres-auth scenarios are not merely reported as passing, they were re-executed here from scratch and pass against the real implementation.

**Coverage**: Not separately measured in this pass; `go test ./...` exit 0 across all 18 packages is the load-bearing signal for the one Go unit added by this change (`healthcheck`), which carries 7 dedicated test functions (`TestRunHealthcheckStatusMapping`, `TestRunHealthcheckConnectionRefusedIsUnhealthy`, `TestRunHealthcheckTimeoutIsUnhealthy`, `TestParseHealthcheckConfigDefaults`, `TestParseHealthcheckConfigOverrides`, `TestRunWithIOHealthcheckWiring`, `TestRunWithIONoArgsListsHealthcheckSubcommand`), confirmed present in `cmd/regixtry/healthcheck_test.go`.

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Hardened Non-Root Runtime | Image runs healthy as non-root | `container-release-smoke.sh` `assert_non_root` + `wait_healthy` (re-executed above, exit 0) | COMPLIANT |
| Persistent Storage Volume Convention | Volume-mounted data survives restart | `run_anonymous_scenario()` destroy+restart-on-same-volume + `HEAD` blob (re-executed above, exit 0) | COMPLIANT |
| Default Serve Entrypoint | Image serves with no arguments | `run_anonymous_scenario()` runs the image with default `ENTRYPOINT`/`CMD`, no override; health poll passed | COMPLIANT |
| Multi-Arch Release Publishing | Tagged release publishes both architectures | `.goreleaser.yaml` `dockers`/`docker_manifests` blocks (config-reviewed); no real `v*` tag push occurred in this environment (no GHCR credentials, no buildx multi-arch push runner) | PARTIAL — static evidence only, needs a real CI tag run |
| Release Tagging Contract | Stable release updates floating tags | `docker_manifests` entries with `skip_push: auto` on the three floating tags (config-reviewed) | PARTIAL — static evidence only, needs a real CI tag run |
| Release Tagging Contract | Prerelease never overwrites floating tags | Same `skip_push: auto` mechanism (config-reviewed, GoReleaser-native semantics, not independently re-executed) | PARTIAL — static evidence only |
| Release Container Smoke Verification | Smoke test blocks a broken image | Anonymous scenario is wired as the real, non-placeholder final step of `.github/workflows/release.yml` (`container-release-smoke.sh --image ... --expect-multiarch`); re-executed locally above, exit 0 | COMPLIANT |
| Container Postgres-Backed Auth Smoke Verification | Auth-enabled smoke path succeeds | `run_auth_scenario()` re-executed above, exit 0, real Postgres, real bootstrap-admin, real token exchange, real blob PUT/HEAD — but **`.github/workflows/release.yml`'s actual smoke step never passes `--auth-postgres`** | CRITICAL — implementation is correct and independently proven, but the automated release-verification gate this requirement names never exercises it |
| Container Postgres-Backed Auth Smoke Verification | Anonymous-only image still works when no DSN is supplied | Re-executed above without `--auth-postgres`, exit 0 | COMPLIANT |
| Truthful Container Deployment Boundary | README documents the manual path without overclaiming | `README.md:75-131` — `## Run as a container` + `### Postgres-backed authentication in a container`; explicit sentence: "`install.sh` and `regixtry setup` do not offer a container-selection branch" | COMPLIANT |

**Compliance summary**: 6/10 scenarios fully COMPLIANT with runtime evidence, 3/10 PARTIAL (structurally correct, only verifiable by a real tagged CI run), 1/10 CRITICAL gap (implementation proven correct, but not wired into the automated release-verification gate the requirement names).

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Hardened Non-Root Runtime | Implemented | `Dockerfile:8-32` — `USER 65532:65532`, exec-form `HEALTHCHECK CMD ["/usr/local/bin/regixtry", "healthcheck"]` |
| Persistent Storage Volume Convention | Implemented | `Dockerfile:13,15` — `install -d ... /var/lib/regixtry`, `VOLUME ["/var/lib/regixtry"]`; matches `serve`'s default `-storage-root` |
| Default Serve Entrypoint | Implemented | `Dockerfile:27-32` — `ENTRYPOINT`/`CMD` includes an explicit `-public-url` (a documented deviation from design, needed because `serve` hard-fails without one — see Issues) |
| Multi-Arch Release Publishing | Implemented (config) | `.goreleaser.yaml:40-77` — two `dockers` entries, `use: buildx`, `--target=release`, `--platform=linux/{amd64,arm64}` |
| Release Tagging Contract | Implemented (config) | `.goreleaser.yaml:78-97` — `{{ .Tag }}` always-pushed, `v{{ .Major }}.{{ .Minor }}`/`v{{ .Major }}`/`latest` each `skip_push: auto` |
| Release Container Smoke Verification | Implemented | `.github/workflows/release.yml:86-88` — real invocation, not a placeholder |
| Container Postgres-Backed Auth Smoke Verification | Implemented, not CI-wired | `container-release-smoke.sh:300-380` (`run_auth_scenario`) is complete and correct; `release.yml:83-88`'s comment still says the flag "lands in PR #3, which stacks on top of this branch" — PR #3 has since landed (all 26 tasks complete) and the flag was never added to the CI invocation |
| Truthful Container Deployment Boundary | Implemented | `README.md:77-79` |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Copy the GoReleaser-built binary instead of compiling in Docker | Yes | `Dockerfile:37-39` (`release` stage is `COPY regixtry` only) |
| One `Dockerfile`, stages `runtime-base → release → build → dev` (dev last) | Yes | Verified stage order and names directly in `Dockerfile`; both `docker build .` and `docker build --target=release .` re-built successfully in this pass |
| Go `healthcheck` subcommand instead of curl/wget | Yes | `cmd/regixtry/main.go:510-553`; exec-form `HEALTHCHECK`, zero extra image bytes |
| 200 or 401 = healthy | Yes | `main.go:548` — exact `StatusOK || StatusUnauthorized` check, matching the documented rationale |
| Floating tags via separate `docker_manifests` entries with `skip_push: auto` | Yes | `.goreleaser.yaml:83-97` |
| GHCR login before GoReleaser | Yes | `.github/workflows/release.yml:29-47` — login/QEMU/buildx precede "Run GoReleaser" |
| Sibling `postgres:17-alpine` on an ephemeral network for the auth scenario | Yes | `container-release-smoke.sh:300-380` |
| `bootstrap-admin` before `serve` starts (ordering is load-bearing) | Yes | Confirmed both by static read and by apply-progress's documented RED-path proof (serve exits non-zero with `BOOTSTRAP_REQUIRED` when started first) |
| Basic → `/auth/token` → Bearer for the authenticated probe (not raw Basic on `/v2/`) | Yes | `container-release-smoke.sh` `registry_token()` implements exactly this exchange |
| One smoke script, two scenario functions, shared helpers (no second script) | Yes | `container-release-smoke.sh` — `run_anonymous_scenario`/`run_auth_scenario` share `fail`/`cleanup`/`wait_healthy`/`http_status` |
| Design's literal `-p 127.0.0.1:0:5000` host-port-publish HTTP-reaching mechanism | No — documented deviation | Script uses `docker run --network container:<target>` sidecar sharing instead, for every HTTP call in both scenarios (confirmed by direct inspection: no `-p` flag reaches any HTTP-probing call). This is a permanent design choice shipped to CI as well, not a sandbox-only workaround — it does not depend on host→container port-forwarding at all. Functionally equivalent (same HTTP requests, same status/header assertions); acceptable deviation, does not violate any spec requirement, which only mandates that `/v2/` gets probed and that auth actually gates access, not a specific client-side mechanism |
| `--auth-postgres` wired into `release.yml`'s smoke step (design's Testing Strategy table implies this for the E2E-auth row) | No — undocumented-as-final gap | See CRITICAL finding below |

### Deviation Review (apply-progress's 7 documented deviations)
1. **Explicit `-public-url` flag added to `CMD`** — necessary correction, not a shortcut: without it `serve` hard-fails per `normalizeRuntimeConfig`, which would have violated the "Default Serve Entrypoint" requirement outright. Reasonable and spec-preserving.
2. **README 6.1a omits `bootstrap-admin` note** — explicit orchestrator scope instruction for that run, and anonymous mode genuinely has no bootstrap step; the Postgres recipe (6.1b) correctly carries it. No spec violation.
3. **Anonymous README example adds `-allow-anonymous-pull -allow-anonymous-push`** — traced against `internal/ports/defaults.go`'s deny-by-default behavior; without the flags the documented `docker push`/`pull` example would 401, which would itself be untruthful. Correctly reasoned, improves compliance with the "Truthful Container Deployment Boundary" requirement rather than weakening it.
4. **CI smoke step's flags evolved across PRs** (`4.3` scaffold with no flags → PR #2 added `--expect-multiarch` → PR #3 never added `--auth-postgres`) — the first two hops are reasonable incremental construction; the final gap is the CRITICAL finding below, not a reasonable terminal state.
5. **Sidecar network-namespace HTTP mechanism instead of design's literal `-p` snippet** — reasonable, sandbox-environment-driven discovery that also generalizes correctly to real CI (host↔container port publishing has no bearing on a same-daemon `--network container:<id>` share). Confirmed by reading the shipped script that this is the actual, permanent mechanism, not a temporary local-only shim.
6. **`bootstrap-admin` uses the `-auth-postgres-dsn` CLI flag while `serve` uses the `REGISTRY_AUTH_POSTGRES_DSN` env var** — preserves an asymmetry that already exists verbatim in `design.md`'s own snippet; not a new inconsistency introduced by apply.
7. **`registry_token()` parses JSON via `grep -o`+`sed` instead of `jq`** — `curlimages/curl:latest` ships no `jq`; apply-progress documents verifying the pattern does not false-match `access_token`. Reasonable, low-risk, single-field extraction against a response shape the same repository controls.

None of the 7 deviations violate a spec requirement; several (1 and 3) actively fix would-be spec violations design.md's literal wording would have produced.

### Sandbox-Limitation Review
Both apply-progress-documented sandbox limitations were independently re-confirmed as genuinely environmental, not implementation defects, and — critically — neither leaked into the shipped artifacts as a workaround:
- **No `buildx` in the sandbox Docker client**: `docker version` in this verify pass showed a `20.10.24+dfsg1` client without the buildx plugin talking to a modern `29.7.2` daemon; `.goreleaser.yaml`'s `use: buildx` and `.github/workflows/release.yml`'s `docker/setup-buildx-action@v3` are unaffected — GitHub's `ubuntu-latest` runners ship buildx natively. `check_multiarch()`'s "buildx plugin unavailable" skip branch in the smoke script is exactly the honest, non-fabricating response to this — it does not silently report a pass.
- **Broken host→container port-forwarding**: this drove the sidecar-mechanism deviation (item 5 above), which is now the script's real, permanent mechanism — not a local-only patch removed before shipping. Confirmed by direct inspection that zero `docker run ... -p` flags exist anywhere in the HTTP-probing paths of the shipped script.

### Line Count / Commit History Sanity Check
`git log --oneline develop..HEAD` shows exactly the 14 commits apply-progress's ledger implies (1 SDD-planning commit + 5 PR #1 commits + 3 PR #2 commits + 3 PR #3 commits, plus one extra `docs(sdd)` commit recording PR #3 hashes). `git diff --stat develop...HEAD` totals **1682 insertions / 13 deletions across 13 files**. Implementation-file-only line counts (excluding `openspec/changes/**` SDD tracking) match apply-progress's own per-file/per-PR figures closely:
- `Dockerfile` +43/-12 (55 changed) — matches apply-progress exactly.
- `.goreleaser.yaml` +59 — matches exactly.
- `cmd/regixtry/healthcheck_test.go` +165 (new) — matches exactly.
- `cmd/regixtry/main.go` +56 net (claimed +55/-1) — matches.
- `docs/verification/scripts/container-release-smoke.sh` 429 lines total = PR #2's 247 (new file) + PR #3's net +182 (189 insertions − 7 deletions) — matches exactly (247 + 182 = 429).
- `README.md` +64 net vs. claimed 32 (PR #1) + 34 (PR #3, +33/-1) ≈ 66 — close, within rounding of how git computes a combined diff across intervening edits.
- `.github/workflows/release.yml` +21 net vs. the sum of per-PR deltas (+23 then +4/-6) — expected to differ because PR #2 rewrote lines PR #1 had just added; the net final diff against `develop`, not the arithmetic sum of intermediate PR diffs, is the correct comparison, and it is consistent with a placeholder step being replaced once.

No hallucination found: every file apply-progress claims was created or modified exists with content matching the claimed shape, and the reported line counts reconcile with the actual `git diff` against `develop`.

### Issues Found

**CRITICAL**:
1. **The Postgres-auth requirement is not exercised by the actual, automated release-verification gate.** `.github/workflows/release.yml`'s "Run container smoke verification" step (line 86-88) invokes `container-release-smoke.sh --image ... --expect-multiarch` with no `--auth-postgres`. The comment directly above it (line 83-85) still reads "the auth scenario and its flag land in PR #3 ... which stacks on top of this branch and extends this same step" — but PR #3 has landed (all 26 tasks marked complete, `run_auth_scenario`/`--auth-postgres` fully implemented and independently re-verified as passing in this very report) and the CI step was never updated to add the flag. The spec's `Container Postgres-Backed Auth Smoke Verification` requirement text is explicit: *"Release verification MUST also exercise the published image with Postgres-backed authentication enabled."* As shipped, no tagged release will ever run that scenario automatically — the only proof any of it works is the one-off local run performed during `sdd-apply` (and this verify pass), which will not repeat on the next release. This directly undermines the stated purpose of adding this requirement mid-cycle: the user explicitly flagged that production runs Postgres-backed auth and called container/production parity "totalmente necesario." An unwired CI gate means a future auth regression in the container image would ship to a tagged GHCR release undetected. **This is a one-line fix** (add `--auth-postgres` to the `release.yml` invocation) but it is a real, currently-shipped gap, not a hypothetical one — my judgment is this blocks a clean PASS. It does not mean the underlying implementation is broken; it means the safety net the requirement asked for is not actually in place for future releases.

**WARNING**:
1. Multi-Arch Release Publishing, Release Tagging Contract (both scenarios) are verified only via static config review (`.goreleaser.yaml`'s `dockers`/`docker_manifests` blocks) plus this sandbox's successful `--target=release` build; no real `v*` tag push, GHCR credential exchange, or floating-tag move was exercised anywhere in this SDD cycle (sandbox has no buildx and no GHCR credentials). This is inherent to any release-triggered pipeline and cannot be fully closed before a real tag push — flagged as residual risk to confirm on the first actual release, not as an implementation defect.
2. The stale CI comment (`release.yml:83-85`) referencing PR #3 as future/pending is now factually wrong given PR #3 has landed; left uncorrected it will mislead the next reader into thinking the gap is still "on the roadmap" rather than a currently-shipped omission.
3. `docker compose build regixtry` could not be verified directly in this environment (this sandbox's client lacks the `docker-buildx-plugin` needed by Compose's BuildKit-aware path); the functionally-equivalent `docker build .` (exactly what `docker-compose.yml`'s `build: {context: ., dockerfile: Dockerfile}` declares) was verified instead, in this pass, with a real successful build.

**SUGGESTION**:
1. Once `--auth-postgres` is wired into `release.yml` (resolving the CRITICAL above), also correct the now-stale comment in the same edit rather than leaving two separate follow-ups.
2. Consider whether `check_multiarch()`'s "imagetools inspect failed" skip branch (as opposed to "buildx unavailable") should get a dedicated test once a CI environment with `buildx` but a deliberately single-arch image is available — apply-progress notes this branch is code-reviewed but has never actually executed anywhere yet.

### Verdict
FAIL
Implementation quality is high and directly re-verified end-to-end in this pass (real Docker builds, real anonymous smoke run, real Postgres-auth smoke run, all passing, zero hallucinated files, line counts reconcile with `git diff`), but the CI-facing release-verification pipeline does not actually invoke the Postgres-auth scenario the mid-cycle spec amendment required — a currently-shipped, one-line-fixable gap that leaves the very parity guarantee the user asked for unenforced on future releases. This is a CRITICAL blocker under the SDD gate and must be closed (wire `--auth-postgres` into `.github/workflows/release.yml`'s smoke step, and fix the now-stale comment) before this change is archive-ready.
