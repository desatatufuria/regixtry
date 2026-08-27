# Apply Progress: Container Setup Mode

Scope of this artifact: **PR #1 (base) only** — `feature/container-setup-mode-01-compose-foundation`,
targeting the tracker branch `feature/container-setup-mode`. PR #2-#5 have not
been applied yet; their tasks in `tasks.md` remain `[ ]` and are out of scope
for this batch. This file exists so later `sdd-apply` runs for PR #2-#5 know
exactly what already landed and do not duplicate or drift from it.

## Status

PR #1: **9/10 tasks complete** (all Go/TDD work done and green; 1 task blocked
by sandbox tooling, not by design or code — see below).

## Completed Tasks (PR #1)

- [x] 1.1 RED: `compose_test.go` table-driven `Preflight` tests (3 failure causes)
- [x] 1.2 GREEN: `ProjectConfig`/`Project` types + `Provisioner.Preflight`
- [x] 1.3 REFACTOR: exec-runner fake extracted to `exec_helper_test.go` (reusable by PR #2/#3)
- [x] 2.1 `internal/infra/install/compose/assets/docker-compose.yml` embedded asset
- [x] 2.2 Repo-root `docker-compose.yml` rewritten, byte-identical to the embedded asset
- [x] 2.3 RED: drift test (`drift_test.go`) — byte-equality repo-root vs. embedded asset
- [ ] 2.4 `.env.example` — **BLOCKED** (sandbox dotenv-pattern write protection; see below)
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
| `.env.example` (repo root) | **Not created** | Blocked — see below; content recorded here |

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
2. **`.env.example` not created — sandbox blocker, not a design or code issue.** See "Blocked
   Task" section below for the full explanation and the exact intended content.
3. **DB username left as `registry`** (not renamed to `regixtry`) to minimize unrelated diff
   surface for existing dev users migrating off the old file; only the fields design explicitly
   calls out (network, credential, build→pull) were changed.

## Blocked Task: `.env.example` (tasks.md 2.4)

**Not a design or code problem.** The sandbox's dotenv-pattern write protection hard-denies any
write to a path matching `.env*` at the repository root, independent of tool (`Write` and `Bash`
heredoc/redirection were both denied identically) and independent of content — confirmed with a
minimal `printf 'X=1\n' > .env.example` probe, which was denied exactly like the real intended
content. This is a legitimate sandbox guardrail (likely blanket dotenv/credential-pattern
protection) that should not be circumvented via renaming tricks or encoding workarounds.

**Exact intended content**, for a human or a differently-configured session to create in one
command (`.env.example` at the repo root, sibling to `docker-compose.yml`):

```
# Copy to .env and fill in values for manual `docker compose up -d` runs.
# `regixtry setup --mode docker` generates and writes these automatically
# under the state directory; this file is only needed for manual compose
# usage (cp .env.example .env).

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

## Remaining Tasks (other PRs — NOT this batch's scope)

- PR #2 (`feature/container-setup-mode-02-compose-credentials`): `StartDatabase`, `BootstrapAdmin` — tasks.md Phase 4-5
- PR #3 (`feature/container-setup-mode-03-compose-lifecycle`): `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — tasks.md Phase 6-7
- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12
