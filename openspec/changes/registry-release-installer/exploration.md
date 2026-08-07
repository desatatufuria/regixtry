## Exploration: Registry release installer

### Current State
The repository currently ships a source-build installer only. `install.sh` clones `https://github.com/desatatufuria/workspace.git`, requires both `git` and `go`, runs `go build -o <tmp>/registry ./cmd/registry`, and installs the resulting `registry` binary locally. `README.md` documents that exact source-build flow and explicitly says it is not a release pipeline.

There is no release automation in the repo today: no `.goreleaser*`, no `.github/workflows/*`, no checksum artifact flow, and no version metadata wired into `cmd/registry/main.go`. The Go module still uses `module registry`, and that path is imported broadly across the codebase, so renaming it is a separate repo-wide change rather than a prerequisite for the installer itself.

### Affected Areas
- `install.sh` — must change from clone-and-build behavior to download-and-install release assets.
- `README.md` — install docs must switch from source prerequisites (`git`, `go`) to release-first usage and fallback guidance.
- `go.mod` — current `module registry` is a release/distribution assumption to evaluate, but not required for the first installer slice.
- `cmd/registry/main.go` — likely place to add optional version/commit/date metadata injection for released binaries.
- `.github/workflows/` — currently absent; needed if releases should be produced automatically from tags.
- `.goreleaser.yaml` (or equivalent) — currently absent; recommended if the project wants consistent multi-platform archives and checksums.
- `docs/contributing.md` — may need a short release-flow update once tagging/release steps become part of the contributor workflow.
- `openspec/changes/registry-release-installer/exploration.md` — persisted exploration artifact for proposal/design follow-up.

### Approaches
1. **Manual release assets + thin downloader** — add a release-oriented installer that fetches platform archives/checksums from GitHub Releases, while release artifacts are created manually at first.
   - Pros: Smallest implementation boundary; proves the release-first install path quickly; avoids mixing release automation and installer logic in one jump.
   - Cons: Manual releases are easy to drift; repeatability and trust are weaker; maintainers carry more operational burden.
   - Effort: Medium

2. **Automated release pipeline with GoReleaser** — add GoReleaser plus tag-triggered GitHub Actions to build archives, publish release assets, generate checksums, and let the installer consume those assets.
   - Pros: Safest sustainable path; standardizes asset naming; makes checksums first-class; supports multi-platform binaries cleanly.
   - Cons: Larger first slice; introduces release automation, secrets/permissions, and versioning conventions at the same time.
   - Effort: Medium

### Recommendation
Use a **staged version of Approach 2** as the first approved boundary: deliver a release-first installer only together with the minimum release production path needed to make that installer trustworthy.

Safest first implementation boundary:
- Keep the binary name as `registry`.
- Keep `module registry` unchanged for this change.
- Add release asset production for the supported OS/arch matrix.
- Generate and publish checksums.
- Update `install.sh` to resolve OS/arch, download the matching archive plus checksum, verify it, and install the binary.
- Update `README.md` to make releases the primary install path and keep source build as an explicit fallback/manual path.

This avoids a dangerous half-step where the installer claims to be release-first but there is no reliable artifact pipeline behind it.

### Risks
- Changing the installer before release assets/checksums exist would create a broken or unsafe `curl | bash` path.
- Renaming the module path now would expand scope across many Go imports and distract from the installer objective.
- Missing stable asset naming/version metadata would make debugging installed binaries harder.
- GitHub Actions release automation needs correct tag, permission, and fetch-depth behavior or releases will fail in CI.

### Ready for Proposal
Yes — propose a narrow change that introduces release artifact production, checksum-backed installer downloads, and documentation updates, while explicitly deferring module-path renaming and broader packaging targets.
