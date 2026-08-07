# Design: Registry Deployment Installer

## Technical Approach

Keep `install.sh` as the release entrypoint and add an installer-level mode resolver ahead of `run_bootstrap()`. In interactive runs, the resolver opens `/dev/tty`, requires an explicit choice, and maps friendly choices to truthful automation paths. In non-interactive runs, flags/env stay authoritative. The service branch continues to call `registry bootstrap --mode daemon-sqlite` unchanged; the binary-only branch stops after verified install and prints manual next steps. This implements the proposal and the `installation-modes` delta without widening product scope.

## Architecture Decisions

| Decision | Options | Tradeoff | Choice |
|---|---|---|---|
| Installer mode boundary | Reuse only `daemon-sqlite`; add installer-facing `binary-only` | Friendly UX needs a mode separate from bootstrap internals | Add installer-level `binary-only` and keep `daemon-sqlite` as the delegated bootstrap mode |
| Interactive input source | Read from stdin; read from `/dev/tty` | `curl | bash` consumes stdin, so stdin prompts are unreliable | Detect a controlling TTY and prompt through `/dev/tty` |
| Bootstrap ownership | Reimplement service install in shell; delegate to existing Go bootstrap | Reimplementation would duplicate host validation, artifact writing, readiness, and rollback rules | Reuse `registry bootstrap --mode daemon-sqlite` unchanged |

Rationale: this preserves current automation seams, avoids false support claims, and minimizes new behavior to chooser/orchestration logic.

## Data Flow

```text
release metadata/download/verify
        ↓
install.sh installs registry binary
        ↓
resolve installer mode
  ├─ interactive + no explicit mode → chooser via /dev/tty
  ├─ explicit flag/env              → validate mode
  └─ no TTY + no explicit mode      → fail with truthful guidance
        ↓
  binary-only ─────────────→ print next steps only
  daemon-sqlite ───────────→ registry bootstrap --mode daemon-sqlite
                                   ↓
                         existing Linux/systemd detection, artifact generation,
                         activation, readiness probe, rollback semantics
```

## File Changes

| File | Action | Description |
|---|---|---|
| `install.sh` | Modify | Add TTY-aware chooser, installer-mode validation, binary-only success path, truthful deferred guidance, and mode-to-bootstrap delegation |
| `README.md` | Modify | Reframe install docs around `binary only` vs `binary + daemon/service`, Linux+systemd-only automation, and manual-today deferred paths |
| `docs/verification/scripts/install-release-smoke.sh` | Modify | Add chooser coverage, `/dev/tty` interactive fixture coverage, binary-only success assertions, and unsupported-mode/non-TTY failures |

## Interfaces / Contracts

```bash
# installer-level contract
--mode <binary-only|daemon-sqlite>
REGISTRY_INSTALL_MODE=<binary-only|daemon-sqlite>

# chooser labels shown to humans
1) binary only
2) binary + daemon/service (Linux + systemd only)

# deferred guidance only; never accepted as automated modes
Postgres-auth: manual today, automated later
Container deployment: manual today, automated later
```

If `--mode` or `REGISTRY_INSTALL_MODE` is set, the installer MUST skip prompting. If no explicit mode is set and a controlling TTY is unavailable, the installer MUST fail with guidance instead of silently defaulting to service installation.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit-ish shell behavior | Mode resolution and validation | Extend `install-release-smoke.sh` with scripted env/arg scenarios |
| Integration | Service branch composition | Keep stub `registry bootstrap` tarball and assert unchanged `daemon-sqlite` arguments and rollback behavior |
| E2E | Interactive chooser and binary-only UX | Add `/dev/tty`-driven smoke cases that assert explicit choice, binary-only no-bootstrap behavior, and non-TTY failure without mode |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | N/A — chooser does not classify arbitrary files for execution | None | None |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A — installer does not select repos | None | None |
| Commit state | staged, `commit -a`, empty index | N/A — no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A — no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A — no PR/VCS command composition | None | None |

## Migration / Rollout

No migration required. Roll out by updating the installer and docs together so the published contract matches shipped behavior immediately.

## Open Questions

- [ ] None.
