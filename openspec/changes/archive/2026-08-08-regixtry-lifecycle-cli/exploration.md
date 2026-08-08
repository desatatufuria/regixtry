## Exploration: regixtry-lifecycle-cli

### Current State
`install.sh` is still the release-first entrypoint: it resolves GitHub release metadata, verifies checksums, installs the `regixtry` binary, then owns the deployment chooser itself. That chooser currently offers `binary-only` and `daemon-sqlite`, prompts on a real TTY when `--mode` is omitted, and directly invokes `regixtry bootstrap` for the truthful automated path. Inside the binary, `cmd/regixtry/main.go` still exposes only low-level operational commands (`serve`, `tui`, `bootstrap`, `bootstrap-admin`); there is no lifecycle-oriented operator surface yet. The truthful backend is `internal/infra/install/linux/bootstrap.go`, which supports only `daemon-sqlite`, writes env/systemd/runtime artifacts, stores a bootstrap receipt, optionally skips start, performs local-bind preflight, starts the service, and probes `/v2/` until HTTP `200` or `401`. The current rollback contract is intentionally narrow: it replays the bootstrap receipt to stop/disable the service and remove generated runtime artifacts, but it explicitly leaves the installed binary in place. There is no real uninstall flow, no install provenance for binary location, and no upgrade command yet.

### Affected Areas
- `install.sh` — currently owns the interactive deployment chooser and still couples release install with bootstrap/rollback orchestration.
- `cmd/regixtry/main.go` — needs a new lifecycle command surface (`setup`, `uninstall`, later `upgrade`) while preserving `bootstrap` as the lower-level backend.
- `cmd/regixtry/main_test.go` — should lock the new lifecycle command contract, delegation boundaries, and subcommand parsing.
- `internal/infra/install/linux/bootstrap.go` — remains the truthful `daemon-sqlite` backend; its receipt/rollback model is the starting point but is too narrow for full uninstall.
- `internal/infra/install/linux/templates.go` — defines the exact env and systemd artifacts that setup/uninstall must continue to create/remove consistently.
- `internal/infra/install/linux/detect.go` — enforces the supported Linux + systemd host boundary that setup must present truthfully.
- `internal/tui/model.go` — proves Bubble Tea is already in the binary, which makes a premium binary-owned UX realistic once lifecycle command boundaries are in place.
- `docs/verification/scripts/install-release-smoke.sh` — currently proves shell-owned mode selection and bootstrap delegation; it will need to shift toward downloader-only install checks plus binary lifecycle coverage.
- `README.md` — currently documents rollback as artifact cleanup only, not full uninstall, so lifecycle semantics must be re-baselined.
- `openspec/specs/installation-modes/spec.md` — current canonical language still centers installer/bootstrap mode truthfulness and explicitly preserves the installed binary on rollback.

### Approaches
1. **Add a top-level lifecycle command family** — introduce a binary-owned lifecycle surface (`regixtry setup`, `regixtry uninstall`, reserve `regixtry upgrade`) that delegates to existing truthful backends where possible and adds install provenance for full cleanup.
   - Pros: Clean UX ownership in the binary; keeps `bootstrap` scriptable; gives uninstall a proper home; creates a stable namespace for the immediate follow-up upgrade phase.
   - Cons: Requires a new lifecycle state/provenance model because the current bootstrap receipt cannot describe full uninstall scope.
   - Effort: Medium

2. **Keep shell as the lifecycle orchestrator and only enrich `bootstrap`** — leave `install.sh` as the primary UX surface and add more backend flags for rollback/uninstall behavior.
   - Pros: Lower initial wiring cost.
   - Cons: Duplicates long-term UX in shell, keeps uninstall dependent on prior parameters or shell conventions, and weakens the binary-first product direction.
   - Effort: Low/Medium

### Recommendation
Choose **Approach 1**.

The safest first lifecycle surfaces to expose from the binary are:

1. `regixtry setup` as the high-level operator entrypoint for only already-truthful flows:
   - binary-only install confirmation + next-step guidance
   - guided `daemon-sqlite` setup delegating to the existing bootstrap backend
2. `regixtry uninstall` as a real cleanup entrypoint driven by recorded lifecycle state rather than remembered bootstrap flags
3. reserve `regixtry upgrade` in the lifecycle namespace, but implement it in the immediately following phase after setup validation

The first reviewable slice boundary should be:

1. make `install.sh` downloader/installer-only after verified binary placement,
2. add `regixtry setup` that owns selection between `binary-only` and `daemon-sqlite`,
3. introduce lifecycle provenance sufficient for later uninstall (at minimum install dir/binary path, chosen mode, service/unit/env/state paths),
4. add `regixtry uninstall` that can remove a setup performed by the new lifecycle flow without requiring the operator to remember old parameters,
5. explicitly defer Postgres-backed install/upgrade orchestration to the next change immediately after setup validation.

This change should **supersede** the earlier `regixtry-setup-cli` direction, not run beside it. That earlier exploration was correct about moving setup UX out of `install.sh`, but it is now incomplete because it did not include a first-class uninstall contract or a lifecycle namespace for the immediate follow-up upgrade phase. Proposal/spec/design work should fold the valid parts of `regixtry-setup-cli` into `regixtry-lifecycle-cli` and treat the older change name as replaced planning, not as a parallel roadmap item.

### Risks
- The current bootstrap receipt only stores generated artifact paths plus service name; it does not record binary install provenance, so uninstall cannot be truthful until lifecycle state expands.
- If `install.sh` keeps its own chooser while `regixtry setup` also prompts, UX ownership will stay split and drift will be inevitable.
- Reusing rollback semantics as uninstall semantics would be misleading, because rollback explicitly preserves the installed binary today.
- Pulling Postgres-backed setup into the first slice would widen secret handling, dependency validation, and rollback scope too early.
- Verification must be re-cut carefully so release-install confidence is preserved while shell-owned interaction moves into the binary.

### Ready for Proposal
Yes — propose `regixtry-lifecycle-cli` as the replacement for `regixtry-setup-cli`: the binary becomes the lifecycle UX owner for `setup` and `uninstall`, `install.sh` becomes downloader-only, uninstall becomes provenance-driven instead of parameter-driven, and `upgrade` follows immediately after installation validation as the next bounded slice.
