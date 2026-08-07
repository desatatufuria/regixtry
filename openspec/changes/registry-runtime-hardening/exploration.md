## Exploration: registry-runtime-hardening

### Current State
The current runtime is still transport-soft by default. `cmd/registry/main.go` starts the registry over a plain `net.Listener` and `http.Server{Handler: ...}` with no TLS branch, no read/write/idle timeouts, and no bounded shutdown timeout. Auth challenge discovery is wired through `REGISTRY_AUTH_TOKEN_REALM_URL`, and the local compose/smoke flows advertise `http://.../auth/token`, so Docker testing still depends on plain HTTP or manual `insecure-registries` configuration when TLS is not provided elsewhere.

Secrets/config handling is also still operator-heavy. `bootstrap-admin` requires `-password` on the command line, which is easy to leak through shell history and process inspection. The registry and TUI both consume direct DSN/base-URL inputs, but there is no hardened runtime config layer for TLS files, public base URL normalization, or secret-file/env separation. Observability is narrow today: `internal/protocol/http/router.go` logs request method/path/status/duration, but there are no health/ready endpoints, metrics, or structured runtime diagnostics. Container/runtime defaults are only partially hardened: the binary image ships with CA certs, but `Dockerfile` still runs as root.

### Affected Areas
- `cmd/registry/main.go` — primary hardening surface for listener/TLS startup, serve flags/env validation, bounded shutdown, and bootstrap secret handling.
- `internal/protocol/http/router.go` — current request logging surface; likely place to preserve challenge behavior while adding safer runtime logging/diagnostic conventions.
- `internal/tui/admin_client.go` — uses `http.DefaultClient` with no timeout; admin-path operability depends on transport defaults here too.
- `Dockerfile` — runtime image currently executes as root and would need safer user/filesystem defaults.
- `docker-compose.yml` — local runtime advertises `http://.../auth/token`, exposes port `5000`, and uses `sslmode=disable` for auth Postgres.
- `docs/verification/scripts/docker-push-pull-smoke.sh` — current Docker smoke path starts plain HTTP serve mode and validates `http://` endpoints.
- `README.md` — documents the current bootstrap/auth/runtime expectations and will need updated TLS/runtime guidance.
- `openspec/changes/registry-release-installer/*` — installer work is adjacent because release installs will need truthful runtime hardening guidance, but it should not be coupled into the first code slice.

### Approaches
1. **In-process TLS + secure runtime defaults** — add TLS support and core runtime hardening directly to the single-binary registry process.
   - Pros: Preserves the product's single-binary story; fixes the actual HTTP registry boundary; keeps auth challenge URLs, server timeouts, and runtime defaults in one place; easiest foundation for later installer/docs work.
   - Cons: Requires careful local-dev ergonomics for self-signed certs and Docker trust; touches serve/config/docs/smoke paths together.
   - Effort: Medium

2. **External TLS terminator first** — keep the registry HTTP-only internally and document/ship a reverse-proxy front door for TLS.
   - Pros: Smaller changes inside Go runtime; mature proxy tooling can help with certificates and headers.
   - Cons: Adds operational surface and weakens the single-binary runtime story; registry still lacks hardened server defaults unless a second slice follows; installer/operator experience becomes multi-component.
   - Effort: Medium

3. **Non-TLS hardening first** — start with non-root image, timeouts, secret handling, and observability, then add TLS later.
   - Pros: Safest purely internal slice; avoids Docker certificate friction on the first PR.
   - Cons: Leaves the biggest current risk in place: local registry traffic still defaults to HTTP; operators still need insecure-registry workarounds.
   - Effort: Low/Medium

### Recommendation
Use **Approach 1**, but keep the first implementation slice tighter than “all hardening at once.”

Recommended first reviewable slice boundary:
- add optional in-process TLS for `serve` (`--tls-cert-file`, `--tls-key-file`, or equivalent env-backed inputs);
- introduce one canonical public base/realm validation path so `/auth/token` and admin-client URLs can move cleanly from `http` to `https`;
- add bounded `http.Server` hardening defaults (read header/read/write/idle timeouts and shutdown timeout);
- update local compose + smoke verification to support a TLS-enabled path;
- explicitly defer broader observability endpoints, cert automation/rotation, and Docker host trust bootstrapping beyond documented local setup.

That slice attacks the exposed transport boundary FIRST, stays aligned with the single-binary architecture, and should remain reviewable within the 1200-line budget if it is kept to runtime/config/docs/verification only.

### Risks
- Docker daemon trust for self-signed/local certs is operationally tricky; if the slice tries to fully automate host trust, scope can balloon quickly.
- Changing auth realm/public URL handling without tight validation could break Docker bearer challenges even if TLS itself works.
- Leaving bootstrap secrets on argv while adding TLS would harden transport but still leave an operator-secret leak path.
- Mixing observability, secret-store redesign, container hardening, and TLS into one PR would likely exceed the intended review boundary.

### Ready for Proposal
Yes — propose a first slice centered on in-process TLS, canonical runtime URL validation, and hardened HTTP server defaults, while explicitly deferring broader observability and secret-management expansion to follow-up slices.
