## Exploration: regixtry-setup-cli

### Current State
`install.sh` is currently a release-first installer that resolves a GitHub release, verifies checksums, installs the `regixtry` binary, and then continues into installer-mode UX itself. Today that UX still lives in shell: it resolves `binary-only` versus `daemon-sqlite`, prompts on a TTY when no mode is provided, and directly invokes `regixtry bootstrap` for the Linux + systemd path. Inside the binary, `cmd/regixtry/main.go` exposes low-level operational commands (`serve`, `tui`, `bootstrap`, `bootstrap-admin`) but no operator-facing `setup` entrypoint yet. The existing Go bootstrap backend in `internal/infra/install/linux/bootstrap.go` is already the truthful runtime installer for `daemon-sqlite`: it validates Linux + systemd support, writes env/unit/runtime artifacts, optionally skips start via `--no-start`, performs local bind preflight, starts the service, and probes `/v2/` reachability. `bootstrap-admin` and Postgres auth runtime seams exist, but they are still separate operator steps rather than part of one setup flow.

### Affected Areas
- `install.sh` — currently owns setup UX, mode prompting, and direct bootstrap delegation; this is the main surface to thin down into downloader-only behavior.
- `cmd/regixtry/main.go` — command dispatch and CLI parsing will need a new operator-facing setup surface while preserving `bootstrap` as the lower-level backend.
- `cmd/regixtry/main_test.go` — existing command/flag tests are the safest place to lock the new setup contract and delegation behavior.
- `internal/infra/install/linux/bootstrap.go` — remains the current truthful backend for `daemon-sqlite`; the first slice should reuse it, not re-implement bootstrap logic.
- `internal/infra/install/linux/templates.go` — defines the env/systemd artifacts that any new setup UX must continue to drive unchanged.
- `internal/infra/install/linux/detect.go` — enforces current distro + systemd support boundaries that setup UX must present truthfully.
- `docs/verification/scripts/install-release-smoke.sh` — currently proves installer-owned mode UX; it will need to shift toward downloader-only verification plus binary-driven setup coverage.
- `openspec/specs/installation-modes/spec.md` — current top-level facts are stale because the repo already has `bootstrap` and `bootstrap-admin`; proposal/spec work must re-baseline before extending behavior.

### Approaches
1. **Add a new `regixtry setup` command over existing backends** — keep `bootstrap` as the implementation backend, but move operator-facing setup selection/prompting into a dedicated high-level CLI command.
   - Pros: Clean separation of concerns; preserves current backend; gives a stable home for future guided setup without bloating shell.
   - Cons: Requires new command wiring and smoke-test reshaping in the same slice.
   - Effort: Medium

2. **Make `bootstrap` itself interactive and let `install.sh` call it** — move prompts into the existing bootstrap command instead of adding a new top-level setup command.
   - Pros: Smaller command surface; less dispatch plumbing.
   - Cons: Blurs backend and UX responsibilities; makes the low-level bootstrap contract harder to keep scriptable and reviewable.
   - Effort: Low/Medium

### Recommendation
Choose **Approach 1**. The safest first binary-driven surface is a new high-level `regixtry setup` entrypoint that only exposes already-truthful flows:

- `binary-only` next-step guidance after install
- guided `daemon-sqlite` setup that delegates to the existing Linux bootstrap backend

Do **not** pull `bootstrap-admin`, Postgres DSN capture, container deployment, or TUI-driven setup into the first slice. Those paths widen product surface and secret-handling scope too early. Keep `bootstrap` as the backend command and position `setup` as the UX layer.

The first reviewable slice boundary should be:
1. make `install.sh` stop after verified binary install and print the next binary-driven step,
2. add `regixtry setup` with mode selection limited to `binary-only` and `daemon-sqlite`,
3. reuse existing `bootstrap` validation/startup behavior unchanged underneath,
4. defer auth/admin orchestration and container/Postgres setup to later changes.

That slice is reviewable because it changes ownership of UX without changing the truthful runtime backend.

### Risks
- Mixing new setup UX with `bootstrap-admin` or Postgres auth would immediately introduce secret input, ordering, and rollback complexity.
- If `install.sh` still owns fallback prompts while `regixtry` also prompts, the product ends up with duplicated setup contracts.
- Installer smoke coverage is currently centered on shell-owned mode behavior, so tests must be re-cut carefully to avoid losing release verification.
- The current `installation-modes` root spec is stale and could mislead proposal/spec work unless re-baselined first.

### Ready for Proposal
Yes — propose `regixtry-setup-cli` as a separation-of-concerns change: `install.sh` becomes downloader/installer only, `regixtry setup` becomes the first operator-facing setup UX, and the initial slice stays strictly limited to `binary-only` guidance plus delegation into the existing `daemon-sqlite` bootstrap backend.
