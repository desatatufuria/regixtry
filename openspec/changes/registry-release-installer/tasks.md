# Tasks: Registry Release Installer

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 700-950 |
| 1200-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR with 2 work-unit commits |
| Delivery strategy | exception-ok |
| Chain strategy | size-exception |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
1200-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Ship release automation, asset naming, and metadata wiring | Single PR / commit 1 | `go test ./cmd/registry` | `N/A - release assets are produced by CI, not local runtime` | `.goreleaser.yaml`, `.github/workflows/release.yml`, `cmd/registry/main*.go` |
| 2 | Switch installer to verified GitHub Releases and update docs | Single PR / commit 2 | `go test ./...` | `docs/verification/scripts/install-release-smoke.sh` against local fixture assets | `install.sh`, `README.md`, `docs/contributing.md`, installer fixture files |

## Phase 1: Foundation

- [x] 1.1 Create `.goreleaser.yaml` for Linux `amd64` + `arm64` archives named `registry_<version>_linux_<arch>.tar.gz` and `registry_<version>_checksums.txt`.
- [x] 1.2 Create `.github/workflows/release.yml` to run only on tags, publish GitHub Release assets, and fail when archive/checksum outputs are missing.
- [x] 1.3 Modify `cmd/registry/main.go` to add release metadata vars for GoReleaser ldflags without changing the module path or binary name `registry`.
- [x] 1.4 Add `cmd/registry/main_test.go` coverage for metadata defaults or formatter helpers introduced in 1.3.

## Phase 2: RED safety tests

- [x] 2.1 Add failing installer harness cases under `docs/verification/scripts/install-release-smoke.sh` for missing `curl`, `tar`, or `sha256sum` to prove hard-fail guidance.
- [x] 2.2 Add failing fixture cases for install dir paths with spaces and malformed `--ref` tags to prove safe subprocess argument handling.
- [x] 2.3 Add failing release-fixture checks for absent checksum assets and archives missing the `registry` entry or containing unexpected paths.
- [x] 2.4 Add failing checksum-mismatch and missing-release-asset cases that assert no binary is installed and manual guidance is printed.

## Phase 3: Core implementation

- [x] 3.1 Refactor `install.sh` to resolve latest or `--ref <tag>` releases from GitHub Releases, Linux-only, with `amd64`/`arm64` asset selection.
- [x] 3.2 Implement mandatory download, checksum lookup, sha256 verification, safe archive extraction, and installation of the inner `registry` binary.
- [x] 3.3 Remove clone/build fallback behavior from `install.sh`; keep explicit manual guidance for release page and documented source install steps.

## Phase 4: Verification and docs

- [x] 4.1 Make `docs/verification/scripts/install-release-smoke.sh` pass against local fixture assets for success, missing asset, missing checksum, mismatch, and bad archive scenarios.
- [x] 4.2 Update `README.md` to make release install the default path, document Linux-only scope, `--ref <tag>`, checksum requirement, and manual/source fallback.
- [x] 4.3 Update `docs/contributing.md` with the tag-driven release flow, checksum expectations, and same-slice doc/task update duties.
