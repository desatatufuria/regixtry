## Exploration: Trivy service runtime for the `trivy` feature

### Current State
`trivy` is already modeled as a built-in feature, but its runtime is still binary-oriented. `serve` wires `trivyinfra.New(...)` directly and seeds `scan_settings` with `binary_path`, `cache_dir`, timeout, schedule, and concurrency defaults. `GetFeatureStatus` probes runtime health with `exec.LookPath(binary_path)`, and scan execution shells out through `Runner.Run(...)` using `settings.BinaryPath image --format json --cache-dir ...`. Admin API, CLI, and TUI surfaces already converge on the same feature authority seam (`scan_settings` via `Service.Get/ConfigureFeature`), which is the main seam to change without renaming the feature.

### Affected Areas
- `internal/app/regixtry/feature_registry.go` — current feature projection hard-codes `trivy` as `builtin`, exposes `binary_path`, and probes health through local binary lookup.
- `internal/app/regixtry/service_scanning.go` — normalization and scan execution assume local binary execution and a local cache directory.
- `internal/infra/scanning/trivy/runner.go` — runtime implementation is a direct `exec.CommandContext(...)` wrapper for `trivy image`.
- `cmd/regixtry/main.go` — `serve` always installs the local Trivy runner and seeds defaults from `--trivy-*` binary-style flags.
- `internal/protocol/http/admin_handlers.go` — feature and scan-settings endpoints already expose the shared seam that should carry service-oriented config.
- `internal/tui/admin_client.go` and `internal/tui/model.go` — admin reads/mutations already consume generic feature endpoints, so they can inherit new fields with minimal surface redesign.
- `internal/app/scanning/scheduler.go` — scheduled scans reuse `GetScanSettings`, so runtime changes must preserve the same enable/schedule contract.
- `internal/ports/regixtry.go` — DTOs currently expose `binary_path` but have no service URL/auth/TLS model.
- `internal/infra/install/linux/bootstrap.go` — lifecycle planning still carries Trivy bootstrap inputs, so compatibility bridging must be defined here too.

### Approaches
1. **Replace the runtime with service-only configuration** — Keep feature identity as `trivy`, move runtime details from local binary execution to a Trivy service endpoint.
   - Pros: Matches the operator requirement, removes host binary dependency, fits container/remote deployments, and keeps one authority seam across CLI/admin/TUI.
   - Cons: Requires DTO/schema changes, health/version probing redesign, and migration from `binary_path` semantics.
   - Effort: Medium

2. **Keep dual runtime modes (`external_binary` and `external_service`)** — Add service support but preserve binary execution as a first-class runtime mode.
   - Pros: Easiest migration path for current installs and lowest immediate disruption.
   - Cons: Keeps two runtime contracts, doubles validation/test matrix, and conflicts with the user's operator model if it remains long-term.
   - Effort: High

3. **Bridge legacy binary inputs only during migration, then converge on service runtime** — Accept old `--trivy-*` binary inputs temporarily, convert them into feature state only when needed, but make the new feature contract service-first.
   - Pros: Minimizes upgrade pain while still landing on the correct architecture.
   - Cons: Requires an explicit deprecation boundary and careful lifecycle truthfulness.
   - Effort: Medium

### Recommendation
Adopt approach 3 with a **service-oriented `trivy` runtime model**. The code already has the RIGHT seam: `scan_settings` is authoritative, and CLI/admin/TUI all project through `Service.ConfigureFeature` and `Service.GetFeatureStatus`. What is wrong is the runtime assumption, not the feature identity. For this operator, a remote or localhost service fits better because Regixtry is a single-binary registry operator, not a host provisioning tool for third-party security binaries. Requiring a host Trivy binary turns feature enablement into machine drift management. A service endpoint instead centralizes DB/cache ownership, works for containerized localhost or remote URLs, and keeps runtime replacement independent from the registry host.

Recommended config model:
- `runtime_kind`: `external_service` for the new target contract; keep `trivy` as the feature name.
- `service_url`: absolute `http` or `https` URL to the Trivy server.
- `auth`: optional bearer token at minimum; structure should allow future header/token variants without changing the feature identity.
- `tls`: `ca_cert_path` or inline CA reference, `insecure_skip_verify` only as an explicit operator opt-in, and optional client cert/key if mTLS is later needed.
- `health/version`: use Trivy server `/healthz` and `/version` instead of `exec.LookPath`; docs confirm both endpoints exist and do not require auth.
- `scan behavior`: keep `enabled`, `schedule_enabled`, `interval`, `timeout`, and `max_concurrency`; drop local `cache_dir` from the Regixtry-owned contract because cache/DB ownership belongs to the service.
- `registry-address handling`: keep explicit registry target construction in Regixtry, but add a service-aware setting for the registry host/address the Trivy service can actually reach. The current `scanTarget()` uses `cfg.PublicURL` host or `registry.local`; that is NOT enough once the scanner runs outside the registry host. The next change should define a dedicated scan-target registry address rather than overloading public UI URLs.

Tradeoff versus binary mode: binary execution is simpler in-process and avoids network auth/TLS complexity, but it makes every operator host responsible for Trivy installation, path management, DB/cache locality, and runtime drift. For this product direction, that is the wrong operational burden. A compatibility bridge SHOULD exist, but only as a migration slice: preserve legacy binary-backed inputs long enough to import or translate existing state, then make service runtime the only recommended steady-state path.

Recommended OpenSpec change name: `trivy-service-runtime`

### Risks
- The current DTOs and docs expose `binary_path` and `cache_dir`; replacing them needs an explicit migration story to avoid ambiguous mixed configs.
- Remote scanning changes the meaning of `scanTarget()` because the Trivy service must reach the registry over a resolvable address, not just the local host assumption.
- Trivy client/server mode still requires careful auth/TLS handling; a weak model here would create insecure defaults.
- If dual runtime modes stay too long, the feature contract will become harder to reason about and test.

### Ready for Proposal
Yes — the codebase already has verified seams for feature authority, runtime probing, scan execution, and operator surfaces. The proposal should define a service-first `trivy` runtime contract, a temporary migration bridge from binary-oriented state, and an explicit registry-address rule for remote scanners.
