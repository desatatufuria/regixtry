# Design: Registry Installation Modes

This slice keeps `install.sh` as the release-binary entrypoint, then hands Linux bootstrap to the installed `registry` binary. The result is a truthful `--mode daemon-sqlite` path for single-node operators: generate host artifacts, register a systemd service, start it by default, and verify `/v2/` reachability without implying container, Alpine, or Postgres install support.

## Technical Approach

Add a new `registry bootstrap` subcommand that owns mode validation, distro detection, artifact rendering, apply/start, verify, and rollback. `install.sh` remains responsible for download and checksum verification, then optionally invokes `registry bootstrap --mode daemon-sqlite ...` so the CLI contract, docs, and smoke tests share one source of truth.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Bootstrap owner | `install.sh` downloads; `registry bootstrap` plans/applies runtime artifacts. | Bash-only bootstrap; service logic embedded in README steps. | Keeps OS policy in one testable Go path and preserves the current installer contract. |
| Artifact model | Generate `/etc/registry/registry.env`, `/var/lib/registry/{content,metadata.db}`, `/etc/systemd/system/registry.service`, and `/etc/registry/bootstrap-state.json`. | Ad-hoc files only; storing config beside the binary. | A receipt makes rollback/idempotence deterministic and keeps runtime files in standard Linux locations. |
| Service management | Support root-owned systemd only in v1; run `systemctl daemon-reload && enable --now registry.service`. | `systemd --user`; SysV/OpenRC shims. | Debian, Ubuntu, Mint, and RHEL 9/10 all ship systemd; anything broader would be dishonest in slice 1. |
| Distro truthfulness | Succeed only when `uname=Linux`, `/etc/os-release` maps to Debian/Ubuntu/Linux Mint or RHEL-compatible `VERSION_ID` 9/10, and systemd is available. | “Best effort” Linux; Alpine compatibility claims. | Prevents false success on hosts where service startup semantics differ. |
| Startup/rollback contract | Default bootstrap applies artifacts, starts service, then polls `GET <public-url>/v2/` until HTTP `200` or `401`; rollback stops/disables the unit and deletes generated artifacts, but not the installed binary. | Manual start; binary uninstall rollback. | Matches the fixed success definition and preserves the verified release asset. |

## Data Flow

`curl|bash -> install.sh -> verified registry binary -> registry bootstrap --mode daemon-sqlite -> detect host -> write artifacts -> systemctl enable --now -> reachability poll -> success/rollback`

## File Changes

| File | Action | Description |
|---|---|---|
| `install.sh` | Modify | Parse bootstrap flags, keep binary install intact, invoke `registry bootstrap` or rollback. |
| `cmd/registry/main.go` | Modify | Add `bootstrap` subcommand and shared flag parsing. |
| `cmd/registry/main_test.go` | Modify | Cover mode validation, distro detection inputs, receipt/rollback orchestration, and readiness rules. |
| `internal/infra/install/linux/bootstrap.go` | Create | Plan/apply/rollback logic, receipt writing, and verification loop. |
| `internal/infra/install/linux/detect.go` | Create | `/etc/os-release` + systemd capability detection. |
| `internal/infra/install/linux/templates.go` | Create | Render env file and systemd unit from `serve` flags. |
| `docs/verification/scripts/install-release-smoke.sh` | Modify | Add daemon-sqlite success/failure/rollback smoke coverage. |
| `README.md` | Modify | Document truthful supported distros, required root/systemd assumptions, startup verification, and rollback limits. |

## Interfaces / Contracts

```go
type BootstrapConfig struct {
    Mode, PublicURL, StorageRoot, StatePath string
    Addr, ServiceName                       string
    Rollback                                bool
}

type BootstrapReceipt struct {
    Mode string
    Paths []string
    ServiceName string
}
```

The env file mirrors existing `serve` inputs (`REGISTRY_PUBLIC_URL`, storage root, DB path, optional address). The unit executes the installed binary as `registry serve ...` with explicit values derived from that env/receipt pair; no new runtime mode is invented beyond `serve`.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Mode validation, distro gating, receipt generation, rollback path filtering | Table-driven Go tests for `internal/infra/install/linux` |
| Integration | Bootstrap apply/rollback against temp roots and stubbed `systemctl`/HTTP probe | `cmd/registry/main_test.go` with fake command runners and temp dirs |
| E2E | Release installer success, unsupported distro, start failure, rollback | Extend `docs/verification/scripts/install-release-smoke.sh` |

## Threat Matrix

Reference file `references/threat-matrix.md` is absent in this repo, so the required boundaries are applied directly.

| Boundary | Applicability | Safe/failure behavior + planned RED test |
|---|---|---|
| Routing | N/A | No router changes in this slice. |
| Shell commands | Applicable | Quote all paths, reject unsupported flags, fail before partial success; RED: space-containing paths and malformed mode input. |
| Subprocess/process control | Applicable | `systemctl`/probe failures return non-zero and trigger rollback guidance; RED: stub `enable --now` and probe failure cases. |
| VCS/PR automation | N/A | No git/GitHub integration. |
| Executable-file classification | Applicable | Keep checksum/archive validation in `install.sh`; RED: wrong asset, malformed archive, missing binary entry. |
| Process integration | Applicable | Require systemd presence and supported distro before writing success state; RED: missing `/run/systemd/system` and unsupported `os-release`. |

## Migration / Rollout

No data migration required. Roll out behind explicit `--mode daemon-sqlite`; binary-only install remains the default path until docs/tasks promote bootstrap usage.

## Open Questions

- [ ] Should bootstrap default `StorageRoot` to `/var/lib/registry` only, or allow an explicit override in slice 1 while still keeping rollback receipt-driven?
