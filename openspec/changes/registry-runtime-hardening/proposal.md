# Proposal: Registry Runtime Hardening

## Intent

Harden the registry’s first-run runtime posture by adding optional in-process TLS, strict public URL/realm validation, and safer HTTP server defaults. This reduces insecure-by-default operation for local operators and small production deployments without breaking the single-binary model.

## Scope

### In Scope
- Add optional in-process TLS inputs for `serve` and define the supported local/dev path.
- Fail startup when canonical public URL and auth realm configuration are inconsistent.
- Add hardened `http.Server` defaults and bounded shutdown behavior.
- Update compose, smoke verification, and docs for HTTPS-first runtime guidance.
- Add minimal safer guidance for `bootstrap-admin` secret entry without redesigning secret storage.

### Out of Scope
- Certificate automation, trust bootstrapping, or rotation workflows.
- Broad observability, health/metrics expansion, and secret-store redesign.

## Capabilities

### New Capabilities
- `registry-runtime-hardening`: TLS-enabled serve mode, canonical public URL/realm validation, and hardened runtime defaults.

### Modified Capabilities
- None.

## Approach

Keep hardening in-process so transport, auth challenge URLs, and server defaults stay in one runtime boundary. Use optional TLS for dev/local compatibility, but prefer secure defaults and fail fast on misconfigured public URL/realm settings. Benchmark proposal details against common hardened registry behavior and Go `net/http` production defaults before implementation.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/registry/main.go` | Modified | TLS startup path, config validation, shutdown/timeouts, bootstrap guidance |
| `internal/tui/admin_client.go` | Modified | Align admin client URL/timeout behavior with hardened runtime expectations |
| `docker-compose.yml` | Modified | Add supported TLS-oriented local flow |
| `docs/verification/scripts/docker-push-pull-smoke.sh` | Modified | Verify HTTPS runtime path |
| `README.md` | Modified | Document runtime hardening expectations and operator guidance |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Docker auth challenge regressions | Med | Fail-fast validation and smoke coverage for `/auth/token` realm behavior |
| Local TLS setup friction | Med | Keep TLS optional for dev and document the supported path clearly |
| Scope growth into broader hardening | High | Explicitly defer observability, trust automation, and secret redesign |

## Rollback Plan

Revert TLS/runtime-hardening changes behind the serve/config path, restore prior HTTP defaults, and keep documentation/smoke flows on the previous HTTP-based local path if compatibility issues appear.

## Dependencies

- Existing exploration artifact for `registry-runtime-hardening`
- Benchmark references from common OCI registry/runtime hardening practices

## Success Criteria

- [ ] Registry can run with optional in-process TLS while preserving the single-binary workflow.
- [ ] Startup fails on invalid canonical public URL / realm combinations.
- [ ] Hardened HTTP defaults and shutdown bounds are documented and verified in the runtime path.
