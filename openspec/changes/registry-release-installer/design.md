# Design: Registry Release Installer

## Technical Approach

Switch `install.sh` from clone-and-build to release download for Linux only, backed by the minimum truthful release pipeline: tag-triggered GitHub Release publication, Linux archives, mandatory checksum verification, and README/contributor updates. This implements the proposal and satisfies the spec by making GitHub Releases the source of truth, preserving the stable `registry` binary name, and failing hard instead of rebuilding from source.

## Architecture Decisions

| Decision | Options | Choice / Rationale |
|---|---|---|
| Release asset contract | Raw binary, `.tar.gz`, or package-manager artifact | Publish `registry_<version>_linux_<arch>.tar.gz` archives plus `registry_<version>_checksums.txt`. Archives keep one stable inner binary name (`registry`), scale to later Linux arches, and match common Go release tooling. |
| Installer selector | Keep source-style refs, add `--version`, or latest-only | Keep `--ref` but narrow it to Git tags / release names; empty means latest release. This preserves the current script surface while removing branch/commit ambiguity that does not fit release-backed installs. |
| Release publication path | Manual assets, custom scripts, or GoReleaser + GitHub Actions | Use a small `.goreleaser.yaml` plus one tag-triggered GitHub Actions workflow. The repo currently has no release automation, so this is the smallest repeatable path that prevents installer/asset drift. |

## Data Flow

1. Maintainer pushes tag `vX.Y.Z`.
2. GitHub Actions runs GoReleaser, building Linux archives, checksum file, and GitHub Release assets.
3. User runs `install.sh`.
4. Script validates Linux support, resolves the release (`--ref` tag or latest), downloads the archive and checksums, verifies the selected asset hash, extracts `registry`, and installs it.
5. Any release-resolution, asset, checksum, or extraction failure exits with manual guidance; no source-build fallback runs.

```text
tag -> GitHub Actions -> GoReleaser -> GitHub Release assets
user -> install.sh -> resolve release -> download archive + checksums
     -> verify sha256 -> extract registry -> install registry
     -> failure -> stop + manual release/source guidance
```

## File Changes

| File | Action | Description |
|---|---|---|
| `.github/workflows/release.yml` | Create | Tag-triggered release job with permissions to publish GitHub Release assets. |
| `.goreleaser.yaml` | Create | Define Linux archives, checksum output, and optional ldflags metadata. |
| `install.sh` | Modify | Resolve GitHub Release assets, verify checksums, extract the archive, and install `registry`. |
| `README.md` | Modify | Make release install the default path and document manual/source fallback explicitly. |
| `docs/contributing.md` | Modify | Add the maintainer release path and checksum expectation. |
| `cmd/registry/main.go` | Modify | Add optional build metadata vars used by release builds without renaming the module path. |
| `cmd/registry/main_test.go` | Modify | Cover metadata defaults / formatting if surfaced in runtime output. |

## Interfaces / Contracts

```text
GitHub Release assets:
- registry_<version>_linux_amd64.tar.gz
- registry_<version>_linux_arm64.tar.gz (prepared by contract; implementation may ship amd64 first if tasks scope it)
- registry_<version>_checksums.txt

Archive contents:
- registry

Checksum file line:
<sha256><two spaces><asset filename>
```

Installer contract:
- Supported OS in this slice: Linux only.
- Supported version selector: `--ref <tag>` or latest release when omitted.
- Verification is mandatory before extraction/install.
- Manual fallback = printed guidance to release page and documented source install steps only.
- Out of scope: package managers, non-Linux automation, branch/commit install targets, automatic source-build fallback, module-path renaming.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Release URL/name resolution and checksum line matching | Add focused shell-safe helpers with table coverage where practical; keep parsing logic deterministic. |
| Integration | Installer success/failure against mocked release assets | Add a script harness using a local HTTP fixture to prove success, missing asset, missing checksum, and mismatch failure paths. |
| E2E | Release automation contract | Verify the workflow builds archives/checksums on tags; local repo verification stays `go test ./...` plus focused installer harness runs. |

## Threat Matrix

Repository note: `references/threat-matrix.md` is absent, so this design applies the required boundary review inline.

| Boundary | Applicability | Safe / Failure Behavior | Planned RED Test |
|---|---|---|---|
| Routing | N/A | No new product HTTP route. | None. |
| Shell commands | Applicable | Quote paths, require tools explicitly, stop on curl/tar/install failures. | Missing `curl`/`tar`/`sha256sum` exits non-zero with guidance. |
| Subprocesses | Applicable | Only trusted local tools run; no unchecked interpolation into command positions. | Install dir containing spaces still succeeds; malformed tag input is rejected. |
| VCS/PR automation | Applicable | Release job runs only on tags and fails if assets/checksums cannot be published. | Workflow fixture/config check fails when checksum asset is absent. |
| Executable-file classification | Applicable | Installer extracts only the expected archive entry and installs it as `registry`. | Archive missing `registry` or containing unexpected path fails hard. |
| Process integration | Applicable | Installer trusts GitHub Releases only after checksum verification. | Mocked checksum mismatch prevents install and leaves no binary behind. |

## Migration / Rollout

No data migration required. Rollout is release-first: merge the code, create the first tagged Linux release, then switch README install guidance after the assets exist.

## Open Questions

- [ ] Whether the first shipped asset set is amd64-only or amd64+arm64 should be locked in tasks, but the naming contract supports both.
