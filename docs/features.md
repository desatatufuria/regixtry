# Built-in Features

Regixtry ships three built-in features, each registered in a single feature
registry: **Trivy** (vulnerability scanning), **Gitleaks** (secret scanning),
and **Signing** (cosign-based image signature verification). All three are
discoverable and controllable through the same generic surface — CLI
(`regixtry feature ...`), admin HTTP API (`/admin/v1/features/...`), and the
TUI's Features screen — but they differ in what they gate, how they trigger,
and what admin surface they expose beyond that generic shell.

## The feature registry

The registry (`internal/app/regixtry/feature_registry.go`) is a fixed list of
three `featureDescriptor`s:

| Name | Kind | `managedRuntime` |
| --- | --- | --- |
| `trivy` | `builtin` | `true` |
| `gitleaks` | `builtin` | `true` |
| `signing` | `builtin` | `false` |

`Kind` is one of `FeatureKindBuiltin`, `FeatureKindExternalBinary`, or
`FeatureKindExternalService` (`internal/ports/regixtry.go`); only `builtin`
is populated today. `managedRuntime` tells the registry whether a feature has
a `FeatureRuntimeManager` and therefore an install/upgrade/rollback binary
lifecycle. Trivy and Gitleaks each manage a downloaded binary (their own
version directory, an `active` symlink, and an install receipt, under
`<storage-root>/features/<name>/`). Signing has no such binary: signature
verification runs in-process (Go's `crypto/ecdsa`), so it has no download,
no version, and no install/upgrade/rollback lifecycle at all.

Every feature — regardless of `managedRuntime` — supports the generic
lifecycle surface:

```bash
regixtry feature list
regixtry feature show <name>
regixtry feature status <name>
regixtry feature configure <name> [flags...]
regixtry feature enable <name>
regixtry feature disable <name>
```

`install`, `upgrade`, and `rollback` are also generic commands, but only do
something for a `managedRuntime: true` feature:

```bash
regixtry feature install trivy -version 0.57.1
regixtry feature upgrade trivy -version 0.58.0
regixtry feature rollback trivy
```

**Known, expected behavior**: `regixtry feature install signing`, `feature
upgrade signing`, and `feature rollback signing` all fail with a typed
validation error — `feature "signing" has no managed runtime` — because
`signing`'s registry entry sets `managedRuntime: false`. This is enforced in
one place (`featureRuntimeManager` in
`internal/app/regixtry/feature_runtime.go`), used by both the CLI path and
the admin API's `install-runtime`/`upgrade-runtime`/`rollback-runtime`
actions, so the two surfaces can never disagree. This is current, correct
behavior, not a doc gap — do not try to install a runtime for signing.

For the full CLI flag reference (including every `feature configure` flag:
`-enabled`, `-schedule-enabled`, `-interval`, `-timeout`, `-service-url`,
`-registry-reachable-url`, `-auth-token`, `-tls-ca-cert-path`,
`-tls-insecure-skip-verify`, `-max-concurrency`), see [`cli.md`](cli.md).
This document gives feature-specific usage examples and each feature's own
quirks instead of repeating that table.

## Trivy (vulnerability scanning)

Trivy scans an image's manifest/blobs for known vulnerabilities and can
optionally **block pulls** of images that violate a configured severity
policy.

### Enable and configure

```bash
regixtry feature install trivy -version 0.57.1
regixtry feature configure trivy \
  -enabled \
  -schedule-enabled \
  -interval 6h \
  -timeout 20m \
  -registry-reachable-url https://registry.internal:5443 \
  -max-concurrency 2
regixtry feature status trivy
```

Or via the admin API:

```bash
curl -X PUT https://registry.example.com/admin/v1/scan-settings \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"enabled":true,"schedule_enabled":true,"interval":"6h","timeout":"20m","registry_reachable_url":"https://registry.internal:5443","max_concurrency":2}'
```

`registry_reachable_url` matters: Trivy scans by pulling the image from the
registry over HTTP(S), so it needs a URL the scanner process can actually
reach — if the registry's own public URL is loopback-only, this field is
required or scans fail closed with a validation error.

### Triggers

A Trivy scan can start from three triggers, all deduplicated per-digest (an
already-queued or already-running scan for the same digest is never
duplicated):

- **push** — a scan is queued automatically right after a successful push,
  if Trivy is enabled (independent of the schedule toggle).
- **scheduled** — the periodic sweep (`ScheduleEnabled` + `Interval`) rescans
  every repository's `latest` tag.
- **manual** — `POST /admin/v1/scan-runs` (or the TUI's Repository Alerts
  actions) queues a scan on demand for a given repository/reference.

### Findings, runs, and the pull-blocking policy gate

- `GET/POST /admin/v1/scan-runs` — list or queue scan runs (filterable by
  `repository`, with a `limit`).
- `GET /admin/v1/scan-runs/{id}` — a single run's detail, including per-CVE
  findings, plus reference/DB freshness metadata.
- `GET/PUT /admin/v1/scan-policy` — the **pull-blocking** policy gate,
  separate from `scan-settings`. `{"enabled": true, "severity_threshold":
  "critical"|"critical_high"}`. When enabled, `OpenManifest` (pull path)
  checks the latest completed scan run for the requested digest and blocks
  the pull (`403`, policy violation) if it meets or exceeds the threshold.
  **Trivy's policy gate is fail-open**: a digest with no completed scan run
  yet — or any scan-state uncertainty — allows the pull rather than blocking
  it. This is the opposite default from Signing's gate (see below).

Per-repository overrides for schedule/timeout/ignore-file/ignore-policy live
under `/admin/v1/features/trivy/repository-overrides/{repo}` (`PUT`/`GET`/
`DELETE`), applied before both push-time and scheduled-scan settings
resolution.

## Gitleaks (secret scanning)

Gitleaks scans an image's blobs for hardcoded secrets. It follows the same
managed-runtime install/upgrade/rollback pattern as Trivy — its own binary,
version directory, and receipt under `<storage-root>/features/gitleaks/` —
but its trigger and HTTP surface are both deliberately narrower.

### Enable and configure

```bash
regixtry feature install gitleaks -version 8.18.4
regixtry feature configure gitleaks -enabled -timeout 10m -max-concurrency 1
regixtry feature status gitleaks
```

Gitleaks' own settings modal in the TUI (and its practical configuration
surface) is intentionally narrower than Trivy's: only `Enabled`, `Timeout`,
and `MaxConcurrency`. Gitleaks scans immutable already-pushed content, so it
has no `ScheduleEnabled`/`Interval` (no periodic sweep) and no
`RegistryReachableURL` (it never pulls from the registry over HTTP — it
scans the blobs already in local storage).

### The asymmetry: no independent trigger

**Gitleaks has no trigger of its own.** It runs only as a side effect of a
Trivy-triggered rescan: `executeScanRun` (the function behind every Trivy
push/scheduled/manual scan) unconditionally spawns
`executeSecretScanLeg` in its own goroutine
(`internal/app/regixtry/service_scanning.go`) for every scan run, regardless
of which trigger started the Trivy leg. There is:

- no push-time gitleaks-only trigger,
- no scheduled gitleaks-only sweep,
- no `POST` endpoint to queue a gitleaks scan on demand.

If Trivy is disabled, or an image is never scanned by Trivy, it is also
never scanned by Gitleaks — even if Gitleaks itself is enabled. The secret
scan leg is entirely best-effort: a disabled Gitleaks feature, a not-ready
runtime, or a scan failure simply returns without persisting anything and
never affects the Trivy leg's own run or status.

### Findings — by image, not by run

Unlike Trivy, Gitleaks has **no** `scan-settings` (it reuses the generic
`/admin/v1/features/gitleaks/config` surface instead), **no**
`scan-policy`, and **no** `scan-runs` list endpoint. The only Gitleaks-
specific HTTP surface is:

```bash
curl "https://registry.example.com/admin/v1/secret-scan-findings?repository=library/alpine&digest=sha256:..." \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

This is a by-image lookup (`repository` + `digest`), not a by-run listing —
there is no equivalent of Trivy's `GET /admin/v1/scan-runs`. Findings are
resolved from the most recent secret-scan run recorded for that exact
repository+digest.

**Gitleaks never gates pulls.** There is no secret-scanning equivalent of
Trivy's `scan-policy` or Signing's `signing-policy`. Findings are purely
informational (visible in the admin API and the TUI's scan-history modal's
`Leaks` tab) and never block a pull, regardless of severity or count.

Per-repository overrides live under
`/admin/v1/features/gitleaks/repository-overrides/{repo}`, with a single
`config_path` field (a gitleaks TOML config path) instead of Trivy's
ignore-file/ignore-policy pair.

## Signing (image signature verification)

Signing verifies cosign-style image signatures against a configured set of
trusted public keys, and can **block pulls** of images that fail
verification. It is the one built-in feature with `managedRuntime: false` —
there is no external binary at all; verification is in-process Go code
(`internal/domain/signing/`) using cosign's signature-manifest and payload
conventions plus stdlib `crypto/ecdsa`.

### Enable and configure

Signing is **not** configured through `feature configure` — that endpoint
explicitly rejects `signing` (`"signing is configured through the signing
policy endpoint"`), so nothing can create a stray `scan_settings` row behind
it. Use the signing-policy endpoint (or its CLI/TUI equivalents) instead:

```bash
regixtry feature status signing
```

```bash
curl -X PUT https://registry.example.com/admin/v1/signing-policy \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"enabled":true,"trusted_public_keys":["-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"]}'
```

`regixtry feature enable signing` / `disable signing` still work — they flip
the same `Enabled` bit as `PUT /admin/v1/signing-policy`, preserving whatever
trusted keys are already configured (`setSigningFeatureEnabled` in
`internal/app/regixtry/feature_registry.go`). **Enabling signing with zero
configured trusted keys is refused outright** — the same
guaranteed-total-outage guard applies whether you enable through the
generic feature endpoint, the signing-policy endpoint, or the TUI's policy
modal.

### Algorithm restriction

Trusted keys must be **ECDSA P-256 public keys, PEM-encoded**. Both
`Ed25519` and `RSA` keys are explicitly rejected — a key that parses as
valid PKIX/SPKI but isn't `*ecdsa.PublicKey`, or is ECDSA on a curve other
than P-256, fails at configuration time with a descriptive error
(`internal/domain/signing/keys.go`, `parseECDSAP256PublicKey`):

> "signing: public key must be ECDSA (Ed25519 and RSA are not supported in
> this slice)"

This restriction is enforced once, when a key is configured
(`NormalizePublicKeyPEM`) — never re-validated at pull time — so a bad key
can never reach the verification hot path.

### Fail-closed default (opposite of Trivy)

**Signing's pull-time gate is fail-closed**, the deliberate opposite of
Trivy's fail-open scan-policy gate. When enabled, `enforceSigningPolicy`
treats "cannot verify" and "not trustworthy" as the same answer — an image
with no signature, an unparseable signature manifest, a signature that
doesn't validate against any trusted key, or a signature that validates but
binds a different digest, all block the pull. The only allow-without-verify
path is the policy being disabled outright.

`verifySignature` resolves to one of five states, shared between the
pull-time gate and the read-only status endpoint below: `unsigned`,
`unverifiable`, `untrusted`, `mismatched`, `verified`. Only `verified`
allows a pull when the policy is enabled. On `verified`, it also returns
*which* trusted key matched, as a short fingerprint (`signing.Fingerprint` —
SHA-256 of the trimmed PEM, first 12 hex characters) — surfaced as
`verified_key_fingerprint` on the signature-status endpoint below and as a
"Signed with: `<fingerprint>`" line on the TUI's manifest inspect screen.

### Unsigned self-read exemption

`unsigned_self_read` (global policy and per-repository override) is an
explicitly opt-in bootstrap exemption for the cosign chicken-and-egg
problem: cosign must `GET` the manifest to know what to sign, but that same
`GET` is blocked by signing's own fail-closed gate before the image has
been signed yet. It has three valid values — `""`/`"off"` (default, no
exemption), `"pusher"` (the exact principal who pushed the digest, matched
by `UserID` against the manifest's recorded pusher, may read it back
unsigned), and `"repo_push"` (any principal with write access to that
repository may read it back unsigned). The exemption only ever affects an
otherwise-blocked *read*; a properly signed digest pulls normally
regardless of this setting, and it is validated (rejecting any other
string) at write time.

### Signature status endpoint

```bash
curl "https://registry.example.com/v2/library/alpine/manifests/latest/signature-status" \
  -H "Authorization: Bearer $PULL_TOKEN"
```

`GET /v2/<repository>/manifests/<reference>/signature-status` is a CI-facing
read endpoint reachable with ordinary pull credentials (no admin session
required). It always answers `200` with the current verification verdict —
`state`, whether the policy is enabled, how many trusted keys are
configured, and whether the pull would currently be blocked — because it
*reports* a verdict rather than being subject to one. This endpoint is
implemented and live today; some in-code comments elsewhere in the service
layer still describe it as a "future phase," but that comment predates the
endpoint actually landing — the router (`internal/protocol/http/router.go`)
and `Service.SignatureStatus` (`internal/app/regixtry/queries.go`) both
confirm it exists and is wired end to end.

Per-repository overrides live under
`/admin/v1/features/signing/repository-overrides/{repo}`, replacing the
global `Enabled`/`TrustedPublicKeys` pair for one repository (full-row
replace, not a merge). The TUI's override editor pre-fills a
**never-configured** override's key list from the current global trusted
keys the first time it loads (so a new override starts from something
sensible instead of empty) — this only ever happens once, and only before
the operator's first save; once the override exists, its own stored keys
are used and the prefill never runs again.

### Managing trusted keys

A signing policy holds **up to 16** trusted public keys, not just one — both
globally and per repository. The TUI's Signing config screen (and the
per-repository override editor) render this as a navigable list rather
than a single field: `n` adds one key (paste the PEM, then Enter), `x`
deletes the selected key. Each key is shown as its short fingerprint
(`signing.Fingerprint`), never as raw key bytes.

Deleting a key first shows a usage advisory: "This key currently verifies
N tagged image(s). Delete it anyway?" (or "at least N (stopped counting)"
once the scan hits its cap). This count comes from
`GET /admin/v1/signing-policy/key-usage?key=<pem>&repository=<optional>`,
which scans up to 500 currently-tagged images in the given scope (global,
or one repository) for a signature that verifies against that specific
key. It is **best-effort and non-exhaustive** — an unreadable tag is
skipped rather than aborting the count — and it **never blocks the
deletion**; the count is purely informational, so an operator with no
credential-rotation plan cannot be trapped unable to remove a key.

## Quick comparison

| | Trivy | Gitleaks | Signing |
| --- | --- | --- | --- |
| Managed runtime (install/upgrade/rollback) | Yes | Yes | No — in-process |
| Independent trigger | push / scheduled / manual | No — only as a side effect of a Trivy scan | N/A (verifies at pull time, on demand) |
| Own settings endpoint | `scan-settings` | generic `features/gitleaks/config` only | `signing-policy` (not `feature configure`) |
| Pull-blocking policy | `scan-policy`, fail-open | none — never gates pulls | `signing-policy`, fail-closed |
| Run listing | `scan-runs` (list + detail) | none — by-image lookup only | none — live verdict via `signature-status` |
| Findings lookup | `scan-runs/{id}` | `secret-scan-findings?repository=&digest=` | `manifests/{ref}/signature-status` |
