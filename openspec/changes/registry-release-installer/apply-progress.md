# Apply Progress: Registry Release Installer

## Change
- Name: `registry-release-installer`
- Mode: Standard
- Delivery: `exception-ok` / `size:exception`
- Current slice: Work Unit 2 — verified release installer and docs

## Completed Tasks
- [x] 1.1 Create `.goreleaser.yaml` for Linux `amd64` + `arm64` archives named `registry_<version>_linux_<arch>.tar.gz` and `registry_<version>_checksums.txt`.
- [x] 1.2 Create `.github/workflows/release.yml` to run only on tags, publish GitHub Release assets, and fail when archive/checksum outputs are missing.
- [x] 1.3 Modify `cmd/registry/main.go` to add release metadata vars for GoReleaser ldflags without changing the module path or binary name `registry`.
- [x] 1.4 Add `cmd/registry/main_test.go` coverage for metadata defaults or formatter helpers introduced in 1.3.
- [x] 2.1 Add failing installer harness cases under `docs/verification/scripts/install-release-smoke.sh` for missing `curl`, `tar`, or `sha256sum` to prove hard-fail guidance.
- [x] 2.2 Add failing fixture cases for install dir paths with spaces and malformed `--ref` tags to prove safe subprocess argument handling.
- [x] 2.3 Add failing release-fixture checks for absent checksum assets and archives missing the `registry` entry or containing unexpected paths.
- [x] 2.4 Add failing checksum-mismatch and missing-release-asset cases that assert no binary is installed and manual guidance is printed.
- [x] 3.1 Refactor `install.sh` to resolve latest or `--ref <tag>` releases from GitHub Releases, Linux-only, with `amd64`/`arm64` asset selection.
- [x] 3.2 Implement mandatory download, checksum lookup, sha256 verification, safe archive extraction, and installation of the inner `registry` binary.
- [x] 3.3 Remove clone/build fallback behavior from `install.sh`; keep explicit manual guidance for release page and documented source install steps.
- [x] 4.1 Make `docs/verification/scripts/install-release-smoke.sh` pass against local fixture assets for success, missing asset, missing checksum, mismatch, and bad archive scenarios.
- [x] 4.2 Update `README.md` to make release install the default path, document Linux-only scope, `--ref <tag>`, checksum requirement, and manual/source fallback.
- [x] 4.3 Update `docs/contributing.md` with the tag-driven release flow, checksum expectations, and same-slice doc/task update duties.

## Files Changed
| File | Action | Notes |
|---|---|---|
| `.goreleaser.yaml` | Created | Added Linux amd64/arm64 builds, tar.gz archives, checksum asset, and ldflags metadata wiring. |
| `.github/workflows/release.yml` | Created | Added tag-only release workflow, focused package test, GoReleaser publish step, and artifact presence checks. |
| `cmd/registry/main.go` | Modified | Added release metadata vars plus default formatting helpers for ldflags injection. |
| `cmd/registry/main_test.go` | Modified | Added table-driven coverage for metadata defaults and overrides. |
| `install.sh` | Modified | Switched from clone/build to release download, Linux-only asset resolution, checksum verification, safe extraction, and manual fallback guidance. |
| `docs/verification/scripts/install-release-smoke.sh` | Created | Added local fixture harness for success plus hard-fail installer scenarios. |
| `README.md` | Modified | Documented release-first install flow, Linux/arch scope, `--ref <tag>`, and manual fallback. |
| `docs/contributing.md` | Modified | Documented installer asset/checksum contract and same-slice update expectations. |
| `openspec/changes/registry-release-installer/tasks.md` | Modified | Marked Work Unit 2 tasks complete. |
| `openspec/changes/registry-release-installer/apply-progress.md` | Created | Recorded cumulative apply progress across Work Units 1 and 2. |

## Work Unit Evidence
| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| 1 | `go test ./cmd/registry` → `ok  registry/cmd/registry  0.741s`; broader regression check `go test ./...` → all packages passed | `N/A` — this slice only establishes CI release automation and metadata wiring; release assets are produced by GitHub Actions rather than a local runtime harness | `.goreleaser.yaml`, `.github/workflows/release.yml`, `cmd/registry/main.go`, `cmd/registry/main_test.go` |
| 2 | `go test ./...` → `ok registry/cmd/registry (cached)`, `ok registry/internal/app/auth (cached)`, `ok registry/internal/app/registry (cached)`, `? registry/internal/domain/auth [no test files]`, `ok registry/internal/domain/registry (cached)`, `ok registry/internal/infra/auth/postgres (cached)`, `ok registry/internal/infra/metadata/sqlite (cached)`, `ok registry/internal/infra/storage/fsblob (cached)`, `ok registry/internal/ports (cached)`, `ok registry/internal/protocol/http (cached)`, `ok registry/internal/tui (cached)` | `bash docs/verification/scripts/install-release-smoke.sh` → `Installer release smoke scenarios passed. Root: /tmp/registry-install-smoke.pw8MfC` | `install.sh`, `docs/verification/scripts/install-release-smoke.sh`, `README.md`, `docs/contributing.md` |

## Remaining Tasks
- None.

## Status
14/14 tasks complete. Ready for verify.
