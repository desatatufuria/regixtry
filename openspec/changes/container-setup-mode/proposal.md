# Proposal: Container Setup Mode

## Intent

`registry-container-mode` shipped a published, verified image (`ghcr.io/desatatufuria/regixtry`, proven at `v0.2.1-rc2`) but deliberately deferred installer integration. So the chooser in `resolveSetupMode` (`cmd/regixtry/main.go:1785-1820`) still offers only `1) binary-only` / `2) daemon-sqlite`, and the only container path is hand-assembly. `docker-compose.yml` cannot close that gap: it demands a pre-created external `dtf-netwok` network, hardcodes `POSTGRES_PASSWORD: registry`, and builds locally instead of pulling the published image. Operators who want containers get no one-shot install, and `daemon-sqlite` still requires a Postgres they already run. This adds a real third installer outcome.

## Scope

### In Scope
- Third `docker` case in `resolveSetupMode` / `runSetup`, mirroring `daemon-sqlite`'s structure (choice, orchestration, provenance, truthful success message).
- Bundled Postgres by default, with an external-DSN opt-out via the existing `-auth-postgres-dsn`.
- Rewritten shippable `docker-compose.yml`: no external network, no literal credentials, pulls the published GHCR image.
- Non-interactive first admin via existing `bootstrap-admin -password-stdin`.
- `install.sh` guidance line for the third option; README for the `docker` path and `docker exec -it <container> regixtry tui -storage-root /var/lib/regixtry`.
- Setup-mode-docker smoke script, analogous to `container-release-smoke.sh`.

### Out of Scope
- Any behavior change to `binary-only` or `daemon-sqlite`.
- Kubernetes, Helm, Swarm, or any orchestrator beyond plain Docker Compose.
- Production-grade or HA bundled Postgres; it is explicitly the easy path.
- Self-hosted dogfooding of published images.
- New TUI code — `docker exec` already works and is a docs task.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `installation-modes`: `docker` becomes a second supported automated mode, so "Truthful Mode Contract" (today: `daemon-sqlite` SHALL remain the only supported automated install mode) would otherwise be false. Its bootstrap-artifact, success/reachability, and provenance-rollback requirements extend to compose artifacts. `registry-container-mode` correctly left this spec alone — a bare image has no installer story — but this change adds an installer outcome to the same chooser, so splitting it into a second capability would fragment one installer contract across two specs.

## Approach

`docker` mode generates a compose project plus a `0600` env file under the state dir, then runs `docker compose up -d`, waits for reachability, and bootstraps the first admin. Credential rules: the bundled Postgres password is generated per install with `crypto/rand`; compose carries only `${...}` interpolation, never a literal; setup prints the env-file path, never the secret; admin password arrives on stdin, never argv or env. Postgres selection: `-auth-postgres-dsn` supplied means external and the bundled service is skipped; absent on a TTY prompts bundled-vs-external; absent non-interactively means bundled. That last default deliberately differs from `daemon-sqlite` (where absent means anonymous) and must be documented. Missing Docker or Compose fails with a clear unsupported result, not a panic. Compose artifacts are recorded in lifecycle provenance so `uninstall` tears the stack down.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` (`resolveSetupMode`, `runSetup`) | Modified | Third `docker` case, Postgres choice, orchestration, provenance |
| `docker-compose.yml` | Rewritten | Published image, no external network, interpolated credentials |
| `install.sh` | Modified | Third guidance line only; chooser stays in `setup` |
| `README.md` | Modified | `docker` setup path, TUI via `docker exec`, drop manual network step |
| `docs/verification/scripts/setup-docker-smoke.sh` | New | Clean-checkout stack smoke |
| `openspec/specs/installation-modes/spec.md` | Modified | Delta spec |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Generated secret leaks to logs or provenance | Med | `0600` env file, print path only, never argv/env |
| Bundled default differs from `daemon-sqlite` semantics | Med | Explicit prompt on TTY; documented non-interactive default |
| Existing dev users depend on `dtf-netwok` compose | Med | Migration note in README, not a silent rewrite |
| Docker/Compose absent or too old on host | Med | Preflight detection with a truthful unsupported error |
| Bundled Postgres read as production-grade | Med | Same honesty framing already used for other modes |

## Rollback Plan

The `docker` case is additive: removing it restores the two-option chooser with `binary-only`/`daemon-sqlite` untouched. `docker-compose.yml` reverts by git history for dev users. Any stack created by this mode is torn down through recorded provenance (`docker compose down`, volume and env-file removal), so no host state persists outside the state dir.

## Dependencies

- Published `ghcr.io/desatatufuria/regixtry` image from `registry-container-mode`.
- Docker Engine with the Compose v2 plugin on the target host.
- Existing `serve -auth-postgres-dsn` and `bootstrap-admin -password-stdin` primitives (no new binary behavior).
- Strict TDD: the new `docker` case in `resolveSetupMode`/`runSetup` lands with `go test ./...` coverage, each assertion proven able to fail before it passes.

## Success Criteria

- [ ] `regixtry setup` offers a third `docker` option and dispatches it.
- [ ] `setup -mode docker` with no DSN produces a healthy stack with bundled Postgres and a generated password absent from logs and compose.
- [ ] `setup -mode docker -auth-postgres-dsn ...` skips the bundled service and authenticates against the external instance.
- [ ] `docker-compose.yml` runs from a clean checkout with no manual `docker network create` and no hardcoded credentials.
- [ ] Smoke script proves reachability plus first-admin creation from a clean checkout.
- [ ] `docker exec -it <container> regixtry tui` is documented as the supported TUI path.
- [ ] `binary-only` and `daemon-sqlite` behavior is unchanged.
