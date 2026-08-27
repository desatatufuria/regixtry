# Apply Progress: Container Setup Mode

Scope of this artifact: **PR #1 and PR #2**. PR #1 (base) shipped
`feature/container-setup-mode-01-compose-foundation`, targeting the tracker
branch `feature/container-setup-mode`. PR #2 shipped
`feature/container-setup-mode-02-compose-credentials`, targeting the PR #1
branch. PR #3-#5 have not been applied yet; their tasks in `tasks.md` remain
`[ ]` and are out of scope for this batch. This file exists so later
`sdd-apply` runs for PR #3-#5 know exactly what already landed and do not
duplicate or drift from it.

## Status

PR #1: **10/10 tasks complete** (all Go/TDD work done and green; task 2.4 initially
blocked by sandbox tooling, resolved with a renamed file — see below).

## Completed Tasks (PR #1)

- [x] 1.1 RED: `compose_test.go` table-driven `Preflight` tests (3 failure causes)
- [x] 1.2 GREEN: `ProjectConfig`/`Project` types + `Provisioner.Preflight`
- [x] 1.3 REFACTOR: exec-runner fake extracted to `exec_helper_test.go` (reusable by PR #2/#3)
- [x] 2.1 `internal/infra/install/compose/assets/docker-compose.yml` embedded asset
- [x] 2.2 Repo-root `docker-compose.yml` rewritten, byte-identical to the embedded asset
- [x] 2.3 RED: drift test (`drift_test.go`) — byte-equality repo-root vs. embedded asset
- [x] 2.4 `docker.env.example` — created (originally attempted as `.env.example`, blocked by sandbox dotenv-pattern write protection; resolved with a non-dotenv filename, see below)
- [x] 2.5 RED: `WriteProject` refusal-of-pre-existing-project + `filepath.Join`/literal-value tests
- [x] 2.6 RED: generated password absent from compose bytes/env keys, `0600` mode test
- [x] 2.7 GREEN: `Provisioner.WriteProject`
- [x] 2.8 REFACTOR: `renderEnvFile`/`envFileData` golden-text helper (reusable by PR #2)
- [x] 3.1 `go test ./internal/infra/install/compose/...` — all green
- [x] 3.2 `go vet ./...` + `gofmt -l .` — clean, no diagnostics

## Files Changed (PR #1)

| File | Action | Notes |
|---|---|---|
| `internal/infra/install/compose/compose.go` | Created | `execRunner`, `ProvisionerConfig`, `Provisioner`, `NewProvisioner`, `Preflight` |
| `internal/infra/install/compose/compose_test.go` | Created | Table-driven `Preflight` RED/GREEN tests |
| `internal/infra/install/compose/exec_helper_test.go` | Created | Shared `fakeExec` test helper (reusable PR #2/#3) |
| `internal/infra/install/compose/project.go` | Created | `ProjectConfig`, `Project`, `WriteProject`, `renderEnvFile` |
| `internal/infra/install/compose/writeproject_test.go` | Created | `WriteProject` RED/GREEN + triangulation tests |
| `internal/infra/install/compose/assets.go` | Created | `//go:embed assets/docker-compose.yml` → `ComposeAsset []byte` |
| `internal/infra/install/compose/assets/docker-compose.yml` | Created | Canonical compose asset (source of truth) |
| `internal/infra/install/compose/drift_test.go` | Created | Byte-equality drift lock vs. repo-root file |
| `docker-compose.yml` (repo root) | Rewritten | Byte-identical to the embedded asset |
| `docker.env.example` (repo root) | Created | Renamed from the originally-planned `.env.example`, which the sandbox blocked — see below |

`cmd/regixtry/main.go` is **not** modified and does not import `internal/infra/install/compose`
(confirmed via `rg -n "infra/install/compose" cmd/regixtry/main.go` → no match), matching this
PR's chain scope — wiring lands in PR #4.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1/1.2 | `compose_test.go` | Unit | N/A (new pkg) | Written | Passed | 3 failure-cause cases + 1 success case | Clean |
| 1.3 | `exec_helper_test.go` | Unit (test infra) | N/A (new) | N/A (test tooling) | N/A | N/A | Extracted from the start as a shared helper |
| 2.3 | `drift_test.go` | Unit | N/A (new) | Written (failed: `ComposeAsset` undefined, then byte mismatch) | Passed | Single scenario (byte equality) | Clean |
| 2.5/2.6/2.7 | `writeproject_test.go` | Unit | N/A (new) | Written | Passed | 6 cases (refusal, literal-value passthrough, password-absence+mode+independence, bundled DSN, external DSN, empty-dir validation) | Clean |
| 2.8 | `writeproject_test.go` (golden assertions) | Unit | N/A (new) | Covered by 2.6/2.7 tests | Passed | Covered above | `renderEnvFile`/`envFileData` extracted as reusable golden helper |

### Test Summary
- **Total tests written**: 12 top-level test functions (3 are table-driven with 3 subtests each collapsed into one)
- **Total tests passing**: all (`go test ./internal/infra/install/compose/...` → `ok`)
- **Layers used**: Unit (12), Integration (0 — none needed, no Docker daemon required per tasks.md), E2E (0)
- **Approval tests**: None — no refactoring of pre-existing code (brand-new package)
- **Pure functions created**: `renderEnvFile`, `generateBundledPassword`, `isDockerNotFound` (all pure or single-effect, easily unit-tested)

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/install/compose/...` → `ok regixtry/internal/infra/install/compose 0.008s`, 12/12 test functions pass |
| Runtime harness | N/A — per tasks.md, this PR is fake-exec unit tests only; no Docker daemon needed or used |
| Rollback boundary | Revert this branch (`feature/container-setup-mode-01-compose-foundation`) entirely; `cmd/regixtry/main.go` does not import the new package, so nothing else breaks |

## Full-Suite Regression Check

`go build ./...` → clean. `go vet ./...` → clean. `go test ./...` → all 19 packages `ok` (cached
or freshly run), including `regixtry/internal/infra/install/compose` freshly at `0.008s`. No
pre-existing test was modified or broken.

## Deviations from Design

1. **Postgres-only compose healthcheck.** Design's File Changes table says "healthchecks ... for
   both services." Only `postgres` gets an explicit `healthcheck:` block (needed for
   `depends_on: condition: service_healthy`). No equivalent exists for `regixtry` in this PR:
   the pulled image's runtime, per the current `Dockerfile` on this branch (`debian:bookworm-slim`,
   no `wget`/`curl`/healthcheck subcommand), has no working healthcheck primitive, and the
   Dockerfile/`regixtry healthcheck` subcommand work that would enable one landed only on the
   separate `registry-container-mode` branch, not yet merged to `develop`, and is out of this PR's
   `## File Changes` scope regardless. `WaitReachable` (PR #3) polls the public URL over HTTP
   directly, so setup orchestration has no functional gap from this — only `docker compose ps`
   would lack a `healthy` status column for the `regixtry` service until a later change adds it.
2. **`docker.env.example` created instead of `.env.example`** — the sandbox's dotenv-pattern
   write protection blocked the originally-planned filename; resolved with a non-dotenv name,
   `docker-compose.yml` updated to match. See "Resolved Task" section below.
3. **DB username left as `registry`** (not renamed to `regixtry`) to minimize unrelated diff
   surface for existing dev users migrating off the old file; only the fields design explicitly
   calls out (network, credential, build→pull) were changed.

## Resolved Task: `docker.env.example` (tasks.md 2.4)

**Originally blocked, now resolved.** The sandbox's dotenv-pattern write protection hard-denies
any write to a path matching `.env*` at the repository root, independent of tool (`Write` and
`Bash` heredoc/redirection were both denied identically) and independent of content — confirmed
with a minimal `printf 'X=1\n' > .env.example` probe by both the apply agent and the orchestrator,
denied identically to the real intended content. This is a legitimate sandbox guardrail
(credential-pattern protection), not something to circumvent via encoding workarounds.

**Resolution (user-approved)**: the file was created as `docker.env.example` instead — a filename
outside the blocked dotenv pattern. `docker-compose.yml` and the embedded asset
(`internal/infra/install/compose/assets/docker-compose.yml`) were both updated so every `:?`
error message's `cp .env.example .env` guidance now reads `cp docker.env.example .env`, keeping
the two files byte-identical per the drift-lock test (`go test ./internal/infra/install/compose/...`
reconfirmed green after the edit). PR #4/#5's `main.go` wiring and README documentation should
reference `docker.env.example` by this same name, not `.env.example`.

**Content actually shipped** at `docker.env.example` (repo root, sibling to `docker-compose.yml`):

```
# Copy to .env and fill in values for manual `docker compose up -d` runs.
# `regixtry setup --mode docker` generates and writes these automatically
# under the state directory; this file is only needed for manual compose
# usage (cp docker.env.example .env).

# Published Regixtry image to run. Pin to a release tag in production;
# "latest" is fine for local/dev use.
REGIXTRY_IMAGE=ghcr.io/desatatufuria/regixtry:latest

# Host port to publish the registry API on.
REGIXTRY_PORT=5000

# Password for the bundled Postgres service. Generate a strong random value
# yourself for manual runs -- `regixtry setup --mode docker` does this with
# crypto/rand automatically.
REGIXTRY_POSTGRES_PASSWORD=

# Postgres DSN the regixtry container uses for auth state. For the bundled
# Postgres service above, this is:
#   postgres://registry:<REGIXTRY_POSTGRES_PASSWORD>@postgres:5432/regixtry_auth?sslmode=disable
# To use an external Postgres instance instead, point this at it and remove
# the postgres service (or run `docker compose up -d --no-deps regixtry`).
REGIXTRY_AUTH_POSTGRES_DSN=
```

This exact text matches the four variable names the design's File Changes table requires
(`REGIXTRY_POSTGRES_PASSWORD`, `REGIXTRY_AUTH_POSTGRES_DSN`, `REGIXTRY_IMAGE`, `REGIXTRY_PORT`)
and the same key names `renderEnvFile` in `project.go` writes at runtime.

## Review Workload / Ledger Flag

- **Declared ledger budget for this run**: 700 changed lines.
- **tasks.md's own PR #1 estimate**: 395-545 lines.
- **Actual (git diff --shortstat against the pre-PR-1 commit)**: **727 changed lines**
  (702 insertions + 25 deletions across 9 files).
- This is **27 lines over the 700-line ledger budget** (≈4% over) and **182 lines over** the
  tasks.md upper estimate (≈33% over). Both commits were made before the total was tallied
  end-to-end (the overage only became visible after Phase 2 completed), so this is being flagged
  now rather than caught before Phase 2 started, per the instruction to flag "if going to exceed
  700 substantially, rather than discovering it after the fact" — this was in fact discovered
  after the fact, and is disclosed here transparently.
- **Why**: the exec-helper extraction (Phase 1.3) done up front, the drift-lock scaffolding
  (Phase 2.1-2.3), and the WriteProject triangulation suite (6 real, non-trivial test cases
  covering refusal/argv-safety/secret-absence/mode/bundled-DSN/external-DSN/validation, per
  strict-TDD's mandatory triangulation rule) account for most of the size; none of it is padding
  — every test asserts a real, spec-mapped behavior with a would-fail-if-wrong assertion.
  Splitting further was considered but rejected: the package would not compile/link
  meaningfully split mid-behavior (e.g. `WriteProject` without its refusal/secret-safety tests
  would violate strict TDD's "no code without a failing test" rule).
- **Recommendation**: accept as a minor, well-justified overage (4% over the hard ledger, driven
  by genuine test coverage rather than scope creep), or split Phase 2's `WriteProject` tests into
  a PR #1a/#1b sub-slice on a future run if the reviewer's budget is genuinely hard-capped at 700.
  No production code needs to change either way.

## Commits (this batch)

1. `24adfb9` — `feat(compose): add compose package types and Preflight detection`
2. `efa317b` — `feat(compose): embed drift-locked compose asset and add WriteProject`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #1)

- PR #2 (`feature/container-setup-mode-02-compose-credentials`): `StartDatabase`, `BootstrapAdmin` — tasks.md Phase 4-5 — **now complete, see below**
- PR #3 (`feature/container-setup-mode-03-compose-lifecycle`): `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — tasks.md Phase 6-7
- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12

---

# PR #2 — targets PR #1 branch (`feature/container-setup-mode-01-compose-foundation`)

**Branch**: `feature/container-setup-mode-02-compose-credentials` (current branch this batch ran on).
**Scope**: `StartDatabase` (bundled Postgres bring-up + bounded readiness poll)
and `BootstrapAdmin` (one-shot, stdin-only admin credential bootstrap) — the
most credential-sensitive cluster in this change, deliberately split into its
own PR (tasks.md Phase 4-5).

## Status

PR #2: **8/8 tasks complete** (all Go/TDD work done and green; no tasks blocked).

## Completed Tasks (PR #2)

- [x] 4.1 RED: `StartDatabase` no-op-when-external + bundled `up -d postgres` tests (`database_test.go`)
- [x] 4.2 RED: bounded `pg_isready` poll test — asserts a small bounded retry count and at least one sleep between attempts, not an unbounded loop
- [x] 4.3 GREEN: `Provisioner.StartDatabase` implemented (`database.go`)
- [x] 4.4 RED: `BootstrapAdmin` stdin-only password test (`admin_test.go`) — asserts the password never appears in any argv element and appears verbatim as the recorded call's stdin
- [x] 4.5 RED: argv-safety test — malicious project name and username (containing `;`, `$(…)`, spaces, a leading `-`) reach `docker` as one literal argv element each (see Deviations note below on why DSN itself isn't a `BootstrapAdmin` parameter)
- [x] 4.6 GREEN: `Provisioner.BootstrapAdmin` implemented (`admin.go`)
- [x] 4.7 REFACTOR: `go vet ./...` + `gofmt -w .` — clean; stdin-buffering note recorded (see Deviations)
- [x] 5.1 `go test ./internal/infra/install/compose/...` — all 22 tests pass (12 from PR #1 + 10 new in PR #2), including every secret-channel and argv-safety assertion

## Files Changed (PR #2)

| File | Action | Notes |
|---|---|---|
| `internal/infra/install/compose/compose.go` | Modified | Added `execStdinRunner` seam type, `Sleep` field on `ProvisionerConfig`, corresponding `Provisioner` fields and `NewProvisioner` default wiring (real `exec.CommandContext` with `cmd.Stdin`; `time.Sleep` default) |
| `internal/infra/install/compose/database.go` | Created | `composeArgs` (shared argv-prefix builder, pure function), `Provisioner.StartDatabase`, `Provisioner.waitPostgresReady` (bounded pg_isready poll, 30 attempts / 1s interval, mirrors `docker-push-pull-smoke.sh`'s proven shape) |
| `internal/infra/install/compose/database_test.go` | Created | RED/GREEN tests for `StartDatabase`'s no-op/bundled/bounded-poll behavior |
| `internal/infra/install/compose/admin.go` | Created | `Provisioner.BootstrapAdmin` — stdin-only password delivery via `execStdinRunner` |
| `internal/infra/install/compose/admin_test.go` | Created | RED/GREEN tests: stdin-only password, argv-safety (project name + username), error-output-never-leaks-password |
| `internal/infra/install/compose/credential_safety_test.go` | Created | Cross-cutting integration-style tests (fake-exec, no daemon) running the full `WriteProject` → `StartDatabase` → `BootstrapAdmin` flow and proving the generated bundled Postgres password never reaches argv/stdin, never appears in compose bytes, never appears in error output, and is disclosed only via the 0600 env file |
| `internal/infra/install/compose/exec_helper_test.go` | Modified | Added `Stdin` field to `execCall`, `RunStdin` method (implements `execStdinRunner`) on the shared `fakeExec` helper, shared `dispatch` helper, `joinArgs` test helper |

`cmd/regixtry/main.go` is **not** modified (wiring lands in PR #4, unchanged from PR #1's note).

## TDD Cycle Evidence (PR #2)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 4.1/4.3 | `database_test.go` | Unit | 12/12 (PR #1 baseline) | Written | Passed | 2 cases (external no-op, bundled up+poll) | Clean |
| 4.2 | `database_test.go` | Unit | Covered above | Written | Passed | Bounded-retry case with injected always-fail poll + sleep-count assertion | Clean |
| 4.4/4.6 | `admin_test.go` | Unit | Covered above | Written | Passed | 3 cases (stdin-only happy path, argv-safety, error-output-safety) | Clean |
| 4.5 | `admin_test.go` | Unit | Covered above | Written | Passed | Single scenario covering both project name and username | Clean |
| Cross-cutting | `credential_safety_test.go` | Unit (integration-shaped, fake-exec) | Covered above | Written | Passed | 4 focused test functions, one per required security property | Clean |

### Test Summary

- **Total tests written (PR #2)**: 10 top-level test functions
- **Total tests passing**: all 22 in the package (12 from PR #1 + 10 new), `go test ./internal/infra/install/compose/...` → `ok`
- **Layers used**: Unit only (12 argv-level unit tests + 4 credential-safety tests exercising the full fake-exec flow, still no Docker daemon), matching tasks.md's stated scope for this PR
- **Approval tests**: None — no refactoring of pre-existing behavior, only additive methods
- **Pure functions created**: `composeArgs` (argv-prefix builder)
- **RED-path proof performed**: the password-in-argv assertion in `TestBootstrapAdminPipesPasswordViaStdinOnly` was deliberately broken (password reintroduced as an explicit `-password <value>` argv element) and confirmed to fail with the exact expected message, then reverted, before this work was marked complete — see Key Learnings.

## Work Unit Evidence (PR #2)

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/install/compose/...` → `ok regixtry/internal/infra/install/compose 0.009s`, 22/22 test functions (34 including subtests) pass |
| Runtime harness | N/A — per tasks.md, this PR is fake-exec unit tests only; no Docker daemon needed or used |
| Rollback boundary | Revert `StartDatabase`/`BootstrapAdmin` (this branch, `feature/container-setup-mode-02-compose-credentials`, on top of PR #1); `Preflight`/`WriteProject` from PR #1 keep working unmodified — confirmed by the full-suite regression run below |

## Full-Suite Regression Check (after PR #2)

`go build ./...` → clean. `go vet ./...` → clean. `gofmt -l internal/infra/install/compose/` → empty (clean).
`go test ./...` → all 19 packages `ok`, including `regixtry/internal/infra/install/compose` freshly at
`0.009s`. No pre-existing test was modified or broken.

## Deviations from Design (PR #2)

1. **`BootstrapAdmin` never passes DSN as an argv value.** Design's Interfaces/Contracts
   signature is `BootstrapAdmin(ctx, p compose.Project, username, password string) error` — no
   DSN parameter. The `regixtry` service's compose definition (PR #1's
   `assets/docker-compose.yml`) already maps the env-file's `REGIXTRY_AUTH_POSTGRES_DSN` to the
   container's `REGISTRY_AUTH_POSTGRES_DSN` environment variable, which is exactly the name
   `bootstrap-admin`'s `-auth-postgres-dsn` flag defaults to reading from
   (`os.Getenv("REGISTRY_AUTH_POSTGRES_DSN")`, confirmed in `cmd/regixtry/main.go`'s
   `parseBootstrapAdminConfig`). So the DSN reaches the container correctly without this method
   ever touching it, and tasks.md 4.5's argv-safety test was written against the two strings this
   method's argv actually carries instead: compose project name and admin username.
2. **Stdin-buffering mitigation is Go-stdlib-inherent, not custom.** Task 4.7 asks to "confirm
   the stdin-writer path has no leftover buffered copy of the password after the call returns."
   This package's own code makes no extra copy (`strings.NewReader(password)` is passed directly
   to the exec seam), but `os/exec` itself copies stdin through an internal pipe goroutine when
   `Stdin` is set to a non-`*os.File` `io.Reader` — that is standard library behavior, not
   something introduced by or removable from this package's code without a custom low-level pipe
   implementation, which was judged out of scope for this PR.
3. **`composeArgs` always includes `--env-file <path>`.** Not explicitly spelled out in tasks.md's
   abbreviated command examples (which show e.g. `docker compose up -d postgres` without flags),
   but necessary: the generated env file is named `regixtry.env` (not `.env`), so Compose will not
   auto-load it without an explicit `--env-file` flag. Every compose invocation in this PR
   (`up -d postgres`, `exec ... pg_isready`, `run ... bootstrap-admin`) is built through this one
   shared, pure `composeArgs` helper so the project-name/file/env-file prefix stays consistent and
   is exercised by every test.

## Review Workload / Ledger Flag (PR #2)

- **Declared ledger budget for this run**: 450 changed lines.
- **tasks.md's own PR #2 estimate**: 230-330 lines.
- **Actual (`git diff --shortstat` against `34917cb`, the PR #1 tip this branch started from)**:
  **599 changed lines** (587 insertions + 12 deletions across 7 files — the 12 deletions are all
  in `exec_helper_test.go`'s refactor to add the shared `dispatch` helper).
- This is **149 lines (~33%) over the 450-line ledger budget** and **269-369 lines (~82-112%) over**
  the tasks.md upper estimate. This was discovered honestly at the end of the batch (after the
  credential-safety cross-cutting test file was written), not caught mid-flight before it grew —
  flagged here transparently rather than silently proceeding to PR #3.
- **Why**: three factors compounded past both estimates:
  1. The new `execStdinRunner` seam (type + `ProvisionerConfig` field + `Provisioner` field +
     `NewProvisioner` default wiring + the shared `fakeExec.RunStdin`/`dispatch` refactor) is
     genuine new seam infrastructure that tasks.md's line estimate likely under-counted — it's
     not just "two methods," it's a second exec channel threaded through the whole test
     scaffolding.
  2. The orchestrator's explicit prompt for this run added a fourth deliverable beyond tasks.md's
     own four RED tests (4.1/4.2/4.4/4.5): a dedicated `credential_safety_test.go` proving four
     distinct security properties (argv/stdin, compose-bytes, log-output, 0600-sole-channel)
     across the **integrated** flow, not just per-method. That file alone is 176 lines and was an
     explicit, reasoned request ("this is exactly the kind of thing that needs 'prove it can
     fail' discipline, not just happy-path coverage"), not scope creep.
  3. Strict TDD's mandatory triangulation rule means every RED test needed a companion case
     (bounded-vs-unbounded poll, happy-path-vs-argv-safety-vs-error-safety for BootstrapAdmin),
     which tasks.md's bullet-per-RED-test list doesn't itself multiply out.
- **No padding**: every test asserts a real, spec-mapped, would-fail-if-wrong behavior; one
  assertion (`TestBootstrapAdminPipesPasswordViaStdinOnly`'s password-in-argv check) was
  RED-path-proven by deliberately reintroducing the leak and confirming the test caught it,
  before being reverted and left in its passing state — see the Full-Suite Regression Check above
  for the post-revert clean state.
- **Recommendation**: accept as a well-justified overage driven primarily by the explicit
  credential-safety test request (176 of the 599 lines, ~29%) plus genuine new seam
  infrastructure, not scope creep; or, if the reviewer's budget is hard-capped, split
  `credential_safety_test.go` into its own immediately-following PR on a future run (it has no
  production-code dependency beyond what PR #2 already ships, so it could land as a thin
  PR #2.5 without touching `StartDatabase`/`BootstrapAdmin` again). No production code needs to
  change either way.

## Commits (this batch — PR #2)

1. `355065c` — `feat(compose): implement StartDatabase for bundled Postgres bring-up`
2. `8d1088d` — `feat(compose): implement BootstrapAdmin with stdin-only credential safety`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #2)

- PR #3 (`feature/container-setup-mode-03-compose-lifecycle`): `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — tasks.md Phase 6-7
- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12
