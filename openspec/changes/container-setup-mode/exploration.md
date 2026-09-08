# Exploration: container-setup-mode

## Current State

This follows directly from `registry-container-mode` (shipped and verified in this same session — real `v0.2.1-rc2` prerelease tag, multi-arch GHCR publish and Postgres-auth smoke both proven in real CI). That change's proposal explicitly deferred "full installer-chooser integration" as Approach 2, to be picked up "once the image is proven in the wild." It now is. All facts below were verified directly against current code and by running real containers in this session, not re-derived from older documents.

1. **The interactive deployment chooser lives in Go, not bash.** `install.sh` is a thin downloader only — it resolves a release, verifies checksum, extracts the binary, and prints two next-step commands (`install.sh:331-332`). The actual TTY-first chooser is `resolveSetupMode` inside `regixtry setup` (`cmd/regixtry/main.go:1785-1820`): it prints `1) binary-only` / `2) daemon-sqlite`, reads a selection, and dispatches. `runSetup` (`main.go:1251-1333`) is a `switch mode` with exactly two real cases plus a `default: unsupported setup mode` error. This is the natural, established extension point for a third `docker` case — not new bash logic in `install.sh`.
2. **What each existing mode actually does**, read directly from `runSetup`:
   - `binary-only` (`main.go:1953-1964`, `printBinaryOnlyGuidance`): prints a message only ("Binary placement is complete, but setup is not yet complete") plus a suggested next command. Does nothing else — no service, no auth.
   - `daemon-sqlite` (`main.go:1271-1329`): optionally bootstraps Postgres-auth if `-auth-postgres-dsn` is supplied (`bootstrapSetupAuth`), generates and starts a real systemd `.service` (`runner.Run`), writes lifecycle provenance used later by `upgrade`/`uninstall`, and confirms `"regixtry is installed, <name>.service is running, and <url> is reachable."` Crucially, `daemon-sqlite` never runs its own Postgres — it only wires auth to a DSN the operator already has running somewhere else.
3. **`docker-compose.yml` is confirmed dev-only and not shippable as-is** (re-read directly, not from a stale prior exploration): the `postgres` service depends on `networks: dtf-netwok: external: true` — a network the operator must pre-create by hand (`docker network create dtf-netwok`, still documented as a manual step in `README.md`) — and hardcodes `POSTGRES_PASSWORD: registry` and the DSN in the `regixtry` service's `environment:` block. The `regixtry` service also `build: {context: ., dockerfile: Dockerfile}`s locally rather than pulling the now-published `ghcr.io/desatatufuria/regixtry` image.
4. **The TUI needs direct filesystem access to SQLite and blobs, confirmed by both reading `runTUI` and running it for real.** `runTUI` (`main.go:2273-2298`) opens `metadata.New(cfg.DatabasePath)` and `fsblob.New(filepath.Join(cfg.StorageRoot, "content"))` — direct local paths, not an HTTP client — for all core screens (repositories, tags, scan config, signing config, gitleaks/trivy settings, GC). Only the auth/ACL admin screens (grants, robots) go over HTTP via `tui.NewHTTPAdminClient(cfg.APIBaseURL, ...)` when `-api-base-url` is set. This was verified live in this session: `docker run -d ghcr.io/desatatufuria/regixtry:v0.2.1-rc2 serve ...` followed by `docker exec <container> regixtry tui -storage-root /var/lib/regixtry -snapshot` rendered a real TUI frame (`Regixtry Console`, exit 0) — the same binary, same in-container filesystem the running `serve` process uses. Running `tui` from the *host* against a container's volume is not recommended (unknown host mount path for named volumes, SQLite file-locking risk across the container boundary) and was not tested as a supported path.
5. **The published image already supports everything a `docker` setup mode needs at the binary level** — no new `serve`/`bootstrap-admin` behavior is required, only new orchestration. `serve` already accepts `-auth-postgres-dsn`/`REGISTRY_AUTH_POSTGRES_DSN`; `bootstrap-admin -password-stdin` already creates the first admin non-interactively; the multi-arch GHCR image already exists and is pullable.

## Affected Areas

| Area | Impact | Description |
|------|--------|--------------|
| `docker-compose.yml` | Rewritten | Drop the external-network requirement, drop hardcoded credentials (generate/prompt instead), pull `ghcr.io/desatatufuria/regixtry:<tag>` instead of building locally, make the bundled `postgres` service optional |
| `cmd/regixtry/main.go` (`resolveSetupMode`, `runSetup`) | Modified | Add a third `docker` case, mirroring `daemon-sqlite`'s structure: optional bundled-vs-external Postgres choice, shell out to `docker compose up -d` (or equivalent), then run `bootstrap-admin` non-interactively against whichever Postgres was selected |
| `install.sh` | Modified | Add `docker` as a third guidance line alongside the existing `binary-only`/`daemon-sqlite` next-step commands (the chooser prompt itself lives in `setup`, not here — see item 1 above) |
| `README.md` | Modified | Document the new one-shot `docker` setup path; document `docker exec -it <container> regixtry tui` as the supported way to reach the TUI against a container deployment; remove the now-unnecessary manual `docker network create dtf-netwok` step |
| `docs/verification/scripts/*` | New/Modified | A setup-mode-docker smoke path analogous to `container-release-smoke.sh`, proving `regixtry setup --mode docker` actually produces a healthy, reachable, (optionally) auth-enabled stack from a clean checkout |

## Approaches

### 1. Bundled-Postgres-by-default, external-DSN opt-out
`docker` mode's compose stack always includes a `postgres` service by default; an operator who already runs their own Postgres passes an existing DSN (mirroring `daemon-sqlite`'s `-auth-postgres-dsn`) and the bundled service is skipped entirely.
- **Pros**: Closest to genuinely "one command, everything works" — the exact gap the user identified (today's `docker-compose.yml` needs a pre-created network and has fixed credentials; `daemon-sqlite` needs a pre-existing Postgres). Matches the confirmed product decision from this conversation: bundle by default, allow pointing at an external instance.
- **Cons**: Compose-managed Postgres is not a production-grade managed database — needs the same honesty framing `registry-deployment-installer` already established for other modes ("this is the easy path, not the only path").
- **Effort**: Medium-High (new Go orchestration branch + compose rewrite + non-interactive credential/admin flow)

### 2. Anonymous-only `docker` mode, auth as a documented manual follow-up
`docker` mode always starts anonymous; enabling Postgres-auth afterward stays a manual step (edit compose, restart, run `bootstrap-admin`).
- **Pros**: Much smaller: no bundled-vs-external decision, no non-interactive secret flow to design.
- **Cons**: Directly contradicts what was just confirmed to matter most in this repo's actual production use — Postgres-auth parity. Rejected for that reason; noted only for completeness.
- **Effort**: Low

## Recommendation

Approach 1, matching the explicit product decision already made in this conversation: `docker` mode bundles Postgres by default with an external-DSN opt-out, added as a third case in `resolveSetupMode`/`runSetup` (not new bash), backed by a rewritten shippable `docker-compose.yml`, and documented together with the confirmed `docker exec -it <container> regixtry tui` path for TUI access.

## Risks

- Non-interactive secret handling inside `setup --mode docker` (bundled Postgres password, admin password) needs a design as careful as `daemon-sqlite`'s existing prompt/flag handling — get this wrong and it either prints a secret to a log or silently uses a weak default.
- Detecting Docker/Compose availability and failing with a clear, truthful error (not a confusing panic) on a host without Docker installed is new error-handling surface.
- `docker-compose.yml`'s existing external-network requirement is a real, already-documented behavior change for any current dev users of that file — needs a clear migration note, not a silent rewrite.
- Strict TDD applies to any Go-level `setup --mode docker` changes; the compose/shell-orchestration parts need the same "prove each assertion can fail" discipline the `container-release-smoke.sh` work already established in `registry-container-mode`.

## Ready for Proposal

Yes — propose `container-setup-mode` scoped to Approach 1: a shippable `docker-compose.yml` (no external network, no hardcoded creds, pulls the published GHCR image, optional bundled Postgres), a new `docker` case in `regixtry setup`'s existing chooser, and documentation of the confirmed TUI-via-`docker exec` path.
