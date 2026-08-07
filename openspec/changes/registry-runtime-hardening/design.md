# Design: Registry Runtime Hardening

## Technical Approach

Harden `serve` at startup, not in downstream handlers. The runtime will accept an explicit canonical public URL plus optional in-process TLS inputs, normalize them once, validate the derived `/auth/token` realm before binding traffic, and build a bounded `http.Server`. This keeps transport, advertised auth URLs, and shutdown behavior inside `cmd/registry/main.go` while preserving the existing single-binary flow.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Canonical runtime URL | Add explicit `PublicURL` config and derive the expected token realm as `PublicURL + /auth/token`. Keep `AuthTokenRealmURL` only as a compatibility input that must exactly match the derived value when provided. | Keep only `-auth-token-realm`; infer public URL from listen address/headers. | Listen address is not the external surface. A single canonical URL gives one source of truth and lets startup reject mismatches early. |
| TLS model | Add optional `TLSCertFile` + `TLSKeyFile`; both-or-neither. HTTPS `PublicURL` requires TLS inputs; HTTP `PublicURL` forbids them. | Force TLS always; rely on reverse proxy only. | Proposal fixes optional-but-supported TLS for local/dev while eliminating ambiguous secure defaults. |
| Secret guidance scope | Add stdin/env-file style bootstrap input and keep `-password` as discouraged compatibility with an explicit warning. | Redesign secret storage or remove argv immediately. | The slice requires safer entry guidance now without expanding into broader secret redesign. |

## Data Flow

```text
CLI/env flags
   -> parseServeConfig / parseBootstrapAdminConfig
   -> normalizeRuntimeConfig
   -> validate public URL, TLS mode, and token realm
   -> build http.Server timeouts + handler
   -> Serve / ServeTLS
```

For auth-enabled startup:

```text
PublicURL -> expected /auth/token realm
          -> access challenge config
          -> WWW-Authenticate header
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `cmd/registry/main.go` | Modify | Add canonical public URL + TLS config parsing, startup normalization/validation, hardened server defaults, bounded shutdown, and safer bootstrap secret inputs/warnings. |
| `cmd/registry/main_test.go` | Modify | Add RED/GREEN coverage for TLS mode validation, realm mismatch failures, timeout/shutdown wiring, and bootstrap secret compatibility guidance. |
| `internal/tui/admin_client.go` | Modify | Replace `http.DefaultClient` fallback with a bounded client timeout so local operator flows do not inherit unbounded defaults. |
| `internal/tui/admin_client_test.go` | Modify | Assert default timeout behavior stays bounded while caller-injected clients still work. |
| `docker-compose.yml` | Modify | Add the supported local TLS-aware path and align env wiring with `PublicURL`. |
| `docs/verification/scripts/docker-push-pull-smoke.sh` | Modify | Exercise HTTPS runtime when TLS fixtures are present and keep explicit HTTP local/dev coverage. |
| `README.md` | Modify | Document preferred TLS/public URL setup, startup failure cases, and minimal bootstrap secret guidance. |

## Interfaces / Contracts

```go
type serveConfig struct {
    Address           string
    PublicURL         string
    TLSCertFile       string
    TLSKeyFile        string
    AuthTokenRealmURL string // optional compatibility override
    ShutdownTimeout   time.Duration
    ReadHeaderTimeout time.Duration
}
```

`normalizeRuntimeConfig` returns a validated runtime struct with:
- normalized `publicURL`
- derived `tokenRealmURL`
- `tlsEnabled`
- concrete server timeout values

Failure contract: startup returns an error before serving traffic when URL scheme/host/port/path mismatch, when TLS inputs are incomplete, or when HTTPS/HTTP mode disagrees with `PublicURL`.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Config normalization, TLS pair rules, public URL/realm mismatch detection, bootstrap secret input precedence | Table-driven tests in `cmd/registry/main_test.go` |
| Integration | `ServeTLS` vs `Serve`, challenge header realm generation, bounded shutdown path | Listener/httptest-style runtime tests in `cmd/registry/main_test.go` and router tests |
| E2E | Local HTTP explicit mode, HTTPS smoke path, bootstrap guidance in compose/docs flow | `docs/verification/scripts/docker-push-pull-smoke.sh` plus compose-assisted manual path |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary is being expanded in this slice; the change hardens startup config and transport mode within the existing binary.

## Migration / Rollout

No data migration required. Rollout is config-first: introduce `PublicURL` and optional TLS inputs, keep `AuthTokenRealmURL` temporarily for compatibility validation, update compose/docs, then remove insecure examples from default guidance.

## Out of Scope

- Certificate automation, trust bootstrapping, or rotation workflows.
- Secret-store redesign, encrypted at-rest secrets, or full non-interactive secret orchestration.
- Broad observability or unrelated auth/TUI feature expansion.

## Open Questions

- [ ] Should the compatibility warning for `-password` print only on interactive terminals, or always when argv carries the secret?
