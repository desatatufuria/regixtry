## Exploration: Registry installation modes

### Current State
The repository now has a truthful **binary installation** path, not a truthful **deployment mode** installer. `install.sh` installs a verified Linux `registry` binary from GitHub Releases, while the actual runtime/deployment choices still live in manual operator steps. The binary exposes `serve`, `tui`, and `bootstrap-admin` entrypoints in `cmd/registry/main.go`, with SQLite metadata always local to the daemon and Postgres auth enabled only when `REGISTRY_AUTH_POSTGRES_DSN` is configured.

The only repo-level deployment helper today is `docker-compose.yml`, but it is explicitly documented in `README.md` as a local helper runtime for manual checks, not a primary external installation contract. That helper also depends on source-tree Docker build context, an external Docker network (`dtf-netwok`), manual bootstrap-admin ordering, and a locally built image rather than a published release image. So the repo currently supports real runtime combinations, but it does not yet package them as operator-safe installation modes.

### Affected Areas
- `install.sh` — current installer surface only knows how to place the release binary; mode selection/bootstrap would start here unless moved to a dedicated companion flow.
- `README.md` — install docs currently separate binary installation from the local Compose helper and would need a new deployment-mode contract.
- `cmd/registry/main.go` — current runtime flags/envs (`-public-url`, `-db`, `-storage-root`, `-auth-postgres-dsn`, TLS inputs, `bootstrap-admin`) define what any mode generator can safely render.
- `cmd/registry/main_test.go` — covers runtime parsing/normalization and is the natural place for new deployment bootstrap parsing coverage.
- `docker-compose.yml` — current container helper encodes the only existing multi-service runtime, but it is dev-oriented and not yet release-oriented.
- `Dockerfile` — builds from repository source; all-in-container installation is not truthful until a published image contract exists.
- `.github/workflows/release.yml` — publishes binary release assets only; no container image publication exists today.
- `.goreleaser.yaml` — currently defines Linux tarballs/checksums only, which blocks a truthful container-install mode.
- `docs/verification/scripts/install-release-smoke.sh` — validates binary installation; new mode selection/bootstrap needs its own deterministic harness.
- `docs/contributing.md` — release installer rules currently cover binary asset naming/checksums only.

### Approaches
1. **Menu-first installer** — extend installation with an interactive selector that asks the operator which deployment mode to bootstrap.
   - Pros: Best discoverability for humans; maps cleanly to the product goal of “choose how to run it.”
   - Cons: Harder to automate, test, and document; adds UX logic before the deployment contract is stable; `curl | bash` interactivity is brittle in CI and provisioning flows.
   - Effort: Medium

2. **Flag-first installer** — keep the installer non-interactive and expose explicit mode flags such as `--mode daemon-sqlite` with mode-specific inputs.
   - Pros: Safest with the current shell-based installer; deterministic; automation-friendly; easy to test in smoke harnesses; lets the contract stabilize before adding UX sugar.
   - Cons: Less discoverable; requires stronger docs/help output; the first human experience is more operator-oriented than guided.
   - Effort: Low

3. **Hybrid contract** — implement flags as the source of truth, then add an optional interactive prompt that fills those flags when stdin is a TTY.
   - Pros: Keeps automation and testability while allowing a friendlier human flow later; avoids duplicating backend behavior.
   - Cons: Larger first slice if done immediately; prompt behavior can distract review from the real bootstrap contract.
   - Effort: Medium

### Recommendation
Start with **Approach 2 now, designed to grow into Approach 3 later**.

Safest first installation/deployment surfaces to expose:
- **First-class now: `daemon + SQLite`** — this matches the current single-binary/runtime architecture, avoids external service orchestration, and can be bootstrapped from already-shipped release assets plus generated config/service files.
- **Next, not first slice: `daemon + SQLite + Postgres`** — viable after the bootstrap contract clearly handles Postgres DSN wiring, bootstrap-admin sequencing, and auth-enabled startup expectations.
- **Defer: `all-in-containers`** — not truthful yet because the repo does not publish a release image, and the current Compose helper is explicitly dev-local, source-build-oriented, and requires a pre-created external network.

Recommended first reviewable slice boundary:
- keep the existing release-binary installer intact,
- add a **flag-driven deployment bootstrap layer** for `--mode daemon-sqlite`,
- generate only the minimum operator artifacts needed to run that mode (for example: env/config file, data directory layout, and a Linux service unit or equivalent bootstrap script),
- add a new smoke harness for mode rendering/apply behavior,
- explicitly leave Postgres-backed auth mode and container mode for follow-up slices.

This is the smallest truthful step from “install the binary” to “install how I want to run it” without pretending the project already has a production-ready Compose/image distribution story.

### Risks
- Treating `docker-compose.yml` as a release-grade install surface today would overstate reality: it still depends on local source build context, manual network creation, and helper-runtime assumptions.
- Adding menu UX in the first slice would increase review scope before the underlying deployment contracts are proven.
- `daemon + SQLite + Postgres` introduces ordering and secrets concerns (`bootstrap-admin`, DSN handling, auth startup gating) that are meaningfully riskier than plain daemon + SQLite.
- Cross-platform service management may expand quickly; the safest first slice should stay Linux-focused like the current release installer.

### Ready for Proposal
Yes — propose a narrow change that adds a flag-driven `daemon + SQLite` bootstrap contract on top of the release installer, explicitly defers menu UX to a follow-up or optional wrapper, and treats Postgres-backed and all-container modes as later slices once their release/distribution contracts are truthful.
