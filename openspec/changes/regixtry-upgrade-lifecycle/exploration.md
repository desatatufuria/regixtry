## Exploration: regixtry-upgrade-lifecycle

### Current State
`regixtry setup` is the binary-owned lifecycle entrypoint and `regixtry uninstall` is provenance-driven cleanup, but `regixtry upgrade` still hard-fails as deferred in `cmd/regixtry/main.go`. The truthful Linux lifecycle backend lives in `internal/infra/install/linux/bootstrap.go`: it validates Linux + systemd, resolves runtime TLS mode, derives a `BootstrapPlan`, writes `regixtry.env`, writes the systemd unit, creates the SQLite metadata file, writes the bootstrap receipt, starts the service, and probes `/v2/` until it returns `200` or `401`. Lifecycle provenance in `internal/infra/install/linux/provenance.go` is intentionally small today: version, mode, installed binary path, service name, lifecycle state path, and managed paths. Installed-runtime detection for `regixtry tui` does not decode structured lifecycle state; it only checks that lifecycle provenance exists and then loads `/etc/regixtry/regixtry.env`. Release selection and binary placement currently exist only in `install.sh`, which supports latest-or-`--ref` GitHub release download plus checksum verification.

### Affected Areas
- `cmd/regixtry/main.go` — currently defers `upgrade`; owns lifecycle CLI UX, prompt behavior, permission guidance, and setup/uninstall orchestration.
- `cmd/regixtry/main_test.go` — already locks setup/uninstall/deferred-upgrade behavior and should become the main CLI contract suite for the real upgrade flow.
- `internal/infra/install/linux/bootstrap.go` — current artifact writer/startup checker is the closest backend, but it cannot be reused blindly for upgrade because `writeArtifacts()` recreates `metadata.db` and would risk clobbering state.
- `internal/infra/install/linux/provenance.go` — current lifecycle provenance is too small for safe in-place upgrade and needs backward-compatible evolution.
- `internal/infra/install/linux/templates.go` — generated env and unit files define the runtime intent that upgrade must preserve and regenerate.
- `internal/infra/install/linux/bootstrap_test.go` — covers lifecycle planning, env/unit generation, TLS-aware readiness probes, and rollback-sensitive setup behavior that upgrade should mirror.
- `internal/infra/install/linux/provenance_test.go` — proves current cleanup ordering/reporting and is the natural place for provenance-version and rollback-safety tests.
- `install.sh` — current source of release lookup, version selection, checksum verification, and binary replacement semantics; upgrade must either absorb or reuse this logic.
- `openspec/specs/lifecycle-cli/spec.md` — currently says upgrade MAY remain reserved/deferred; this change will need to replace that contract.
- `openspec/specs/installation-modes/spec.md` — currently defines truthful lifecycle success, scoped rollback, and Linux + systemd constraints that upgrade must preserve.

### Approaches
1. **Binary-owned staged upgrade** — add a real `regixtry upgrade` flow that resolves the target release, downloads and verifies a replacement binary, reads existing lifecycle/env intent, regenerates managed artifacts with the new binary, runs migrations/health checks, and rolls back automatically on failure.
   - Pros: Keeps lifecycle UX in one place; preserves installed intent without asking operators to re-enter config; enables silent patch upgrades; can reuse current readiness and permission-guidance patterns.
   - Cons: Requires new Go-side release-download/checksum code or a shared downloader package; needs provenance/env parsing plus migration/rollback design.
   - Effort: High

2. **Shell-assisted upgrade** — keep `regixtry upgrade` as a thin wrapper around `install.sh --ref ...` plus a follow-up lifecycle refresh.
   - Pros: Lower initial implementation cost because release resolution already exists in shell.
   - Cons: Splits lifecycle ownership again, makes rollback/error handling harder to keep truthful, and complicates non-interactive upgrade safety.
   - Effort: Medium

### Recommendation
Choose **Binary-owned staged upgrade**.

The core design should be: detect an installed lifecycle-managed runtime, load current intent from lifecycle provenance plus `regixtry.env`, resolve a target version (`latest` by default, optional explicit `--ref`/version), download and checksum-verify a staged binary, stop the service only once the staged artifact is ready, atomically replace the installed binary, regenerate env/unit from preserved intent, run any backward-compatible migrations, restart, and prove health with the same `/v2/` readiness semantics used by setup. If restart, migration, or health checks fail, rollback should restore the previous binary and previous generated artifacts.

That recommendation matters because the existing bootstrap backend is NOT upgrade-safe as-is: `writeArtifacts()` recreates `metadata.db`, and lifecycle provenance v1 does not record enough structured configuration to rebuild intent safely without also reading the env file. So upgrade should be a sibling lifecycle path, not just `setup` rerun under a different name.

### Risks
- Reusing `Bootstrapper.Run()` directly would be dangerous because it writes an empty SQLite database file at `DatabasePath`.
- Lifecycle provenance v1 does not record public URL, listen address, TLS mode/files, auth DSN, or the installed version/ref, so upgrade needs a compatibility story that can reconstruct intent from env for existing installs.
- `regixtry tui` installed-runtime detection depends on the current env file location and keys, so env regeneration must stay backward-compatible.
- Remote and local Postgres auth must both remain opaque DSN preservation cases; upgrade must never “normalize” by assuming a local database topology.
- Binary replacement without staged backup/restore could leave operators with a stopped service and no known-good executable.
- Prompting during routine upgrades would make automation brittle; but silent upgrades without a major-version or destructive-change warning path could surprise operators.

### Ready for Proposal
Yes — propose a Linux + systemd lifecycle upgrade that is silent by default for same-shape upgrades, preserves env/unit/runtime intent and data, supports latest-or-explicit version selection, extends provenance compatibly, performs staged binary replacement plus health-checked restart, prompts only when required intent is missing or a risky transition needs confirmation, and includes automatic rollback to the prior binary/artifacts on failure.
