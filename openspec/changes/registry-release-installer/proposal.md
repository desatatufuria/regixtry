# Proposal: Registry Release Installer

## Intent

Replace the source-build-first install path with a release-first path that feels production-ready for maintainers and early external adopters. This change is needed now because a trustworthy installer requires real Linux release assets, published checksums, and a stable `registry` binary name.

## Scope

### In Scope
- Produce Linux release archives for `registry` in GitHub Releases.
- Publish checksum assets and make installer verification mandatory.
- Update `install.sh` and `README.md` for release-first install with manual fallback guidance.

### Out of Scope
- Repo-wide Go module path renaming.
- Non-Linux targets, package managers, auto-fallback source builds, or broader distribution channels.

## Capabilities

### New Capabilities
- `release-installation`: Release-backed Linux installer flow using GitHub Release assets and checksum verification.

### Modified Capabilities
- None.

## Approach

Add the minimum release pipeline needed to make the installer truthful: create Linux release artifacts plus checksums, optionally stamp version metadata into the binary, and make `install.sh` resolve the Linux asset from GitHub Releases, verify checksums, and install `registry`. On any asset or checksum failure, the installer stops and points users to manual guidance instead of building from source automatically.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `.github/workflows/` | New | Tag-driven GitHub Release publication |
| `.goreleaser.yaml` | New | Linux archives and checksum generation |
| `install.sh` | Modified | Download, verify, install release assets |
| `README.md` | Modified | Release-first install and fallback docs |
| `cmd/registry/main.go` | Modified | Optional version metadata wiring |
| `docs/contributing.md` | Modified | Maintainer release-flow guidance |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Release assets or checksums drift from installer expectations | Med | Standardize naming and verify in CI |
| Linux-only first slice disappoints other adopters | Med | State boundary clearly and defer expansion |
| Release automation fails on tags or permissions | Med | Keep scope minimal and document release prerequisites |

## Rollback Plan

Revert installer/docs to the current source-build path, disable the release workflow, and leave manual/source installation as the supported fallback until release assets are trustworthy again.

## Dependencies

- GitHub Releases as the source of truth for published assets.
- Linux release archive and checksum publishing on tagged versions.

## Success Criteria

- [ ] Tagged releases publish Linux `registry` assets plus checksums to GitHub Releases.
- [ ] `install.sh` installs Linux `registry` only from verified release assets.
- [ ] Failures stop with clear manual guidance instead of silent or source-build fallback.
