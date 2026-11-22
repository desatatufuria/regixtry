# HTTP API

The implementation registers `/auth/token` only when auth is configured, `/admin/v1...` only when the service offers the admin interface, and always registers `/v2` / `/v2/`.

## Authentication API

### `GET|POST /auth/token`

Accepts Basic Auth and repeated `scope` parameters; `service` is optional and reflected back in the response. Returns `200` with `token`, `access_token`, `expires_in`, `issued_at`, and optionally `service`/`scope`. Invalid credentials return `401` with a Registry-style error. Every credential failure is indistinguishable to the caller: an unknown username, a disabled user, a robot trying a password and a wrong password all give the same `401` and the same `invalid credentials` body, and each one costs one bcrypt comparison (against a dummy hash when no real one applies), so neither the response nor its timing reveals whether a username exists or is disabled. The real reason is only written to the server log (`auth token denied username=... reason=...`). The endpoint accepts either a user's password or a pre-issued admin credential token.

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## Management API

Every route under `/admin/v1` requires a Bearer token for a global admin (`Principal.IsAdmin`), with one deliberate exception: `/admin/v1/repositories/{repo}/grants...` is reachable by any authenticated principal. The HTTP layer only authenticates the caller there (`requireAuthenticatedPrincipal`, no `IsAdmin` check); the service layer then decides admin-or-repo-admin authority per repository, since only it has the repository argument in scope. In practice this means a user holding the `repo-admin` role on that one repository (a delegate, not a global admin) can list, grant, and revoke grants for that repository — everything else under `/admin/v1` stays global-admin-only. See `internal/protocol/http/admin_handlers.go`'s `handleAdmin` for the exact prefix check.

The Bearer token must also be unrestricted: an access token that carries scopes (for example one obtained with `?scope=repository:team/image:pull`, which is what `docker login` and CI pulls use) is refused on every `/admin/v1` route with `403`, even when its owner is a global admin or a repo-admin. This includes a token that was requested with scopes but was granted none of them. Upgrade note: access tokens issued before this change whose requested scopes intersected to nothing carry an empty scope and stay unrestricted until they expire (15 minutes). Administrative clients (the TUI and CLI) log in without `scope`, which produces the unrestricted token these routes accept.

Resetting a password (`:reset-password`) replaces the password hash and, in the same transaction, revokes the user's active login sessions: their access tokens stop working at once (`401`). This includes the caller's own session when an admin resets their own password; they must log in again. Admin credential tokens (the PAT-like tokens managed on the Tokens screen) are not touched and still exchange for new access tokens at `/auth/token`; revoke them explicitly. The `user.password_reset` access event is recorded as before. A robot has no password, so resetting one is rejected with `422` (`target user is a robot account and has no password`) and nothing is changed or recorded.

JSON bodies are decoded rejecting unknown fields and multiple bodies.

| Method | Endpoint | Body | Success |
| --- | --- | --- | --- |
| GET | `/admin/v1/users` | none | `200`, array of users |
| POST | `/admin/v1/users` | `username`, `password`, `is_admin`, `enabled` | `201`, user |
| POST | `/admin/v1/users/{id}:enable` | none | `200`, user |
| POST | `/admin/v1/users/{id}:disable` | none | `200`, user |
| POST | `/admin/v1/users/{id}:reset-password` | `new_password` | `204`; `422` for a robot |
| GET | `/admin/v1/users/{id}/grants` | none | `200`, array of grants |
| PUT | `/admin/v1/users/{id}/grants/{repository}` | `role` | `200`, grant |
| DELETE | `/admin/v1/users/{id}/grants/{repository}` | none | `204` |
| GET | `/admin/v1/users/{id}/admin-tokens` | none | `200`, array without secrets |
| POST | `/admin/v1/users/{id}/admin-tokens` | `name`, optional `ttl` (number plus `h`, `d` or `y`, for example `12h`, `30d`, `2y`; default 30 days; maximum 2 years), or the older `ttl_seconds` (seconds; maximum `63072000`), never both | `201`, token and secret |
| DELETE | `/admin/v1/users/{id}/admin-tokens/{accessor}` | none | `204` |
| GET | `/admin/v1/admin-tokens/expiring` | query `within` (number plus `h`, `d` or `y`; default `30d`; maximum `2y`) and `limit` (1 to 200; default 50); invalid values give `422` | `200`, array (soonest first) of `id`, `accessor`, `name`, `user_id`, `username`, `is_robot`, `created_at`, `expires_at` for enabled users and robots whose token is not revoked, not yet expired and expires within `within`; never a secret or hash |
| GET | `/admin/v1/access-events` | query `limit` (1 to 200; default 20); invalid values give `422` | `200`, array (newest first) of `id`, `at`, `action`, `actor`, `subject`, `repository`, `role`, `token_name`, `detail`; empty fields are omitted; `action` is `grant.put` (repository grant given or changed), `grant.delete` (grant revoked), `token.issued`, `token.revoked`, `robot.created`, `robot.deleted`, `user.created` (`detail` lists the role flags: `administrator`, `read-only` or `standard user`), `user.enabled`, `user.disabled`, `user.updated` (`detail` lists only the flags that changed, such as `administrator on`; no event when none changed), `user.password_reset` (never carries a password) or `user.deleted` (the deleted username stays in `subject`); names and short text only, never a secret, hash, accessor or token id; global admin only |
| GET | `/admin/v1/repository-grants` | query `limit` (1 to 200; default 100) and `after` (the opaque `next` of the previous page); invalid values or a malformed cursor give `422` | `200`, object `{"grants":[...],"next":"..."}`; `grants` holds the grants of every user and robot on every repository, including repositories that were never pushed, ordered by repository then username, each with `repository`, `user_id`, `username`, `is_robot`, `role`, `created_at`, `updated_at`; `next` is empty on the last page; disabled users still appear; never a secret, hash or token; global admin only |
| GET | `/admin/v1/robots` | none | `200`, array of robot accounts |
| POST | `/admin/v1/robots` | `name`, `repository`, `role`, optional `ttl` (number plus `h`, `d` or `y`, for example `12h`, `30d`, `2y`; default 30 days; maximum 2 years), or the older `ttl_seconds` (seconds; maximum `63072000`), never both | `201`, robot account with one-time token secret |
| DELETE | `/admin/v1/robots/{id}` | none | `204`; rejected if the target is not a robot account |
| GET | `/admin/v1/features` | none | `200`, built-in feature inventory |
| GET | `/admin/v1/features/{name}` | none | `200`, feature-owned configuration |
| GET | `/admin/v1/features/{name}/status` | none | `200`, feature configuration plus runtime health |
| PUT | `/admin/v1/features/{name}/config` | `enabled`, `schedule_enabled`, `interval`, `timeout`, `service_url`, `registry_reachable_url`, optional `auth_token`, `tls_ca_cert_path`, `tls_insecure_skip_verify`, `max_concurrency` | `200`, persisted feature state |
| POST | `/admin/v1/features/{name}:enable` | none | `200`, enabled feature state |
| POST | `/admin/v1/features/{name}:disable` | none | `200`, disabled feature state |
| POST | `/admin/v1/features/{name}:install` | optional `version` | `200`, runtime state |
| POST | `/admin/v1/features/{name}:upgrade` | optional `version` | `200`, runtime state |
| POST | `/admin/v1/features/{name}:rollback` | none | `200`, runtime state |
| GET | `/admin/v1/features/{name}/repository-overrides` | none | `200`, array of stored per-repository overrides |
| GET | `/admin/v1/features/{name}/repository-overrides/{repository}` | none | `200`, override, or `404` when none is set |
| PUT | `/admin/v1/features/{name}/repository-overrides/{repository}` | feature-specific override fields | `200`, override |
| DELETE | `/admin/v1/features/{name}/repository-overrides/{repository}` | none | `204`; reverts that repository to the global settings |
| POST | `/admin/v1/features/{name}/actions/{actionID}` | none | `200`, action result |
| GET | `/admin/v1/scan-settings` | none | `200`, current settings |
| PUT | `/admin/v1/scan-settings` | `enabled`, `schedule_enabled`, `interval`, `timeout`, `service_url`, `registry_reachable_url`, optional `auth_token`, `tls_ca_cert_path`, `tls_insecure_skip_verify`, `max_concurrency` | `200`, persisted settings |
| GET | `/admin/v1/scan-policy` | none | `200`, current vulnerability-gate policy |
| PUT | `/admin/v1/scan-policy` | `enabled`, `severity_threshold` (`critical` or `critical_high`) | `200`, persisted policy |
| GET | `/admin/v1/signing-policy` | none | `200`, current signing-gate policy |
| PUT | `/admin/v1/signing-policy` | `enabled`, `trusted_public_keys` (up to 16 PEM-encoded ECDSA P-256 keys), `trusted_identities` (up to 16 keyless/Fulcio identities, each `certificate_identity_regexp` + `certificate_oidc_issuer`), `unsigned_self_read` (`""`/`"off"`, `"pusher"`, or `"repo_push"`) | `200`, persisted policy |
| GET | `/admin/v1/signing-policy/key-usage?key=<pem>&repository=<optional>` | none | `200`, `{"count": int, "capped": bool}` — best-effort count of currently-tagged images verifying against `key` (global scope, or one repository), shown before an operator deletes that key; never a blocking gate |
| POST | `/admin/v1/scan-runs` | `repository`, `reference` | `202`, queued run with canonical digest |
| GET | `/admin/v1/scan-runs?repository=&limit=` | none | `200`, run history |
| GET | `/admin/v1/scan-runs/{id}` | none | `200`, run detail with persisted findings, DB freshness, and reference freshness |
| GET | `/admin/v1/repository-scan-summaries?limit=` | none | `200`, one row per repository with its latest scan run, collapsed before `limit` |
| GET | `/admin/v1/secret-scan-findings?repository=&digest=` | none | `200`, secret-scan findings for that repository/digest pair |
| GET | `/admin/v1/repositories/{repo}/grants` | none | `200`, array of grants for that repository |
| PUT | `/admin/v1/repositories/{repo}/grants/{username}` | `role` | `200`, grant |
| DELETE | `/admin/v1/repositories/{repo}/grants/{username}` | none | `204` |
| POST | `/admin/v1/gc/reports` | none | `201`, computed report of unreferenced, grace-expired blob candidates; always reachable regardless of `REGISTRY_GC_DELETE_ENABLED` |
| GET | `/admin/v1/gc/reports/{id}` | none | `200`, a previously computed report by id, with its full candidate list |
| POST | `/admin/v1/gc/reports/{id}/delete` | none | `200`, delete outcome (per-candidate `deleted`/`retained`/`missing`/`failed`); gated by `REGISTRY_GC_DELETE_ENABLED` (`UNSUPPORTED`/`501` while disabled) |

Example: tokens that expire in the next 14 days (global admin token required):

```sh
curl -H "Authorization: Bearer $TOKEN" \
  "https://registry.example.com/admin/v1/admin-tokens/expiring?within=14d"
```

```json
[{"id":"6f1c2b0e-8d4a-4a53-9a57-1f3e5c7d9b21","accessor":"act_6f1c2b0e8d4a4a539a571f3e5c7d9b21","name":"ci","user_id":"u_1","username":"alice","is_robot":false,"created_at":"2026-02-01T10:00:00Z","expires_at":"2026-03-10T10:00:00Z"}]
```

Example: the five most recent access changes (global admin token required):

```sh
curl -H "Authorization: Bearer $TOKEN" \
  "https://registry.example.com/admin/v1/access-events?limit=5"
```

```json
[{"id":"0b6f4c1e-52c1-4e0a-9f55-7d1a2c3e4b5f","at":"2026-03-01T10:00:00.5Z","action":"grant.put","actor":"admin","subject":"alice","repository":"team/app","role":"repo-writer","detail":"Reader to Writer"},{"id":"7a1d9e20-3b4c-4f6a-8d12-9e0f1a2b3c4d","at":"2026-03-01T09:30:00Z","action":"token.issued","actor":"admin","subject":"alice","token_name":"ci","detail":"30 days"}]
```

Example: the first page of all repository grants, then the next one (global admin token required):

```sh
curl -H "Authorization: Bearer $TOKEN" \
  "https://registry.example.com/admin/v1/repository-grants?limit=2"
curl -H "Authorization: Bearer $TOKEN" \
  "https://registry.example.com/admin/v1/repository-grants?limit=2&after=eyJyIjoidGVhbS93ZWIiLCJ1IjoiYW5hIiwiaSI6InVfMSJ9"
```

```json
{"grants":[{"repository":"alpha/api","user_id":"u_1","username":"ana","is_robot":false,"role":"repo-admin","created_at":"2026-03-01T09:00:00Z","updated_at":"2026-03-01T09:00:00Z"},{"repository":"team/web","user_id":"u_1","username":"ana","is_robot":false,"role":"repo-writer","created_at":"2026-03-01T09:30:00Z","updated_at":"2026-03-02T08:00:00Z"}],"next":"eyJyIjoidGVhbS93ZWIiLCJ1IjoiYW5hIiwiaSI6InVfMSJ9"}
```

The cursor is opaque: pass `next` back unchanged as `after`, never build or edit one. The order is bytewise by repository name, then username. Grants are read live, so a grant added or removed between pages can appear or vanish but the walk never repeats or skips a stable row.

Events are recorded after the change succeeded and on a best-effort basis: a failed history write never fails the change itself.

See [registry.md](registry.md#blob-garbage-collection) for the full garbage-collection flow, including the grace window and delete-time re-check that keep it safe.

Admin errors: `401`, `403`, `404`, `409`, `422` depending on auth, existence, conflict, or validation; the body is `{ "error": "..." }`.

### Scan settings example

```bash
curl -X PUT \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  https://registry.example.com/admin/v1/scan-settings \
  -d '{
    "enabled": true,
    "schedule_enabled": true,
    "interval": "6h",
    "timeout": "20m",
    "service_url": "https://scanner.example.com",
    "registry_reachable_url": "https://registry.internal:5443",
    "tls_ca_cert_path": "/etc/regixtry/trivy-ca.pem",
    "max_concurrency": 2
  }'
```

`auth_token` is write-only. Read/status responses never echo it back.

### Scan policy example

```bash
curl -X PUT \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  https://registry.example.com/admin/v1/scan-policy \
  -d '{"enabled": true, "severity_threshold": "critical_high"}'
```

### Signing policy example

```bash
curl -X PUT \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  https://registry.example.com/admin/v1/signing-policy \
  -d '{"enabled": true, "trusted_public_keys": ["-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"]}'
```

Keys must be PEM-encoded ECDSA P-256 public keys; up to 16 entries are accepted. `enabled: true` with zero usable anchors of either kind (keys or identities) is rejected as a validation error — a signature verifies against ANY trusted key OR ANY trusted identity, never both combined with AND.

### Keyless (Fulcio/OIDC) identity example

```bash
curl -X PUT \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  https://registry.example.com/admin/v1/signing-policy \
  -d '{
    "enabled": true,
    "trusted_identities": [
      {
        "certificate_identity_regexp": "^https://github.com/acme/.+$",
        "certificate_oidc_issuer": "https://token.actions.githubusercontent.com"
      }
    ]
  }'
```

`certificate_identity_regexp` matches against the certificate's Subject Alternative Name (a regexp, compiled and rejected as a validation error at write time if malformed); `certificate_oidc_issuer` is the exact required OIDC issuer, both required per entry. Up to 16 identities are accepted, the same cap `trusted_public_keys` already has. Verification is entirely offline: the embedded certificate is checked against a pinned Sigstore public-good trusted root (no TUF auto-update, no operator-supplied root override, no live Rekor query), and the embedded Signed Entry Timestamp is verified against the pinned root rather than querying Rekor live. A bundle whose only timestamp proof is a TSA signature with no Rekor transparency-log entry is rejected as unverifiable, never silently accepted. The pinned root ships with the binary; rotating it is a release-time task, not a runtime configuration option.

Per-repository overrides (`PUT /admin/v1/features/signing/repository-overrides/{repository}`) accept the identical `trusted_identities` field and persist it with the same full-row-replace semantics as `trusted_public_keys`: saving an override with keys but no identities clears any inherited global identities for that repository, and vice versa.

When a pull verifies via the identity path, both the pull-time gate and `GET .../manifests/{reference}/signature-status` report the matched Subject Alternative Name and OIDC issuer as their own distinct value, never folded into the key-fingerprint field a key-verified signature reports.

### Feature status example

```bash
curl \
  -H 'Authorization: Bearer <admin-token>' \
  https://registry.example.com/admin/v1/features/trivy/status
```

### Manual rescan example

```bash
curl -X POST \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  https://registry.example.com/admin/v1/scan-runs \
  -d '{"repository":"library/alpine","reference":"latest"}'
```

### Scan run detail example

```bash
curl \
  -H 'Authorization: Bearer <admin-token>' \
  https://registry.example.com/admin/v1/scan-runs/run-123
```

The detail payload keeps the summary `run` object, a compact ordered `findings` table, `db_freshness`, and `reference_freshness`. Summary list payloads stay compact; drill-down data is returned only from the detail endpoint.

## Registry API V2

| Method | Endpoint | Auth/success |
| --- | --- | --- |
| GET/HEAD | `/v2/` | Registry check; `200` or `401` |
| GET | `/v2/_catalog` | Catalog; `200` JSON `{repositories:[...]}` |
| GET | `/v2/<repo>/tags/list` | Tags; `200` JSON `{name,tags}` |
| POST | `/v2/<repo>/blobs/uploads/` | Starts upload; `202` |
| GET/HEAD | `/v2/<repo>/blobs/uploads/<id>` | Upload state; `204` |
| PATCH | `/v2/<repo>/blobs/uploads/<id>` | Appends a chunk; `202` |
| PUT | `/v2/<repo>/blobs/uploads/<id>?digest=sha256:...` | Completes upload; `201` |
| DELETE | `/v2/<repo>/blobs/uploads/<id>` | Not implemented; `UNSUPPORTED` |
| GET/HEAD | `/v2/<repo>/blobs/<digest>` | Blob; `200` |
| PUT | `/v2/<repo>/manifests/<tag-or-digest>` | Publishes manifest; `201` |
| GET/HEAD | `/v2/<repo>/manifests/<tag-or-digest>` | Manifest; `200` |
| DELETE | `/v2/<repo>/manifests/<digest>` | Deletes the manifest, cascading to every tag pointing at it; `202` JSON body naming what was removed; `404` if absent |
| DELETE | `/v2/<repo>/manifests/<tag>` | Untags only, leaving the manifest and its other tags intact; `202`; `404` if absent |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/scan-status` | CI-facing scan verdict; pull-credential auth; always `200` |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/signature-status` | CI-facing signature verdict; pull-credential auth; always `200` |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/secret-scan-status` | CI-facing secret-scan verdict; pull-credential auth; always `200` |

`_catalog` and `tags/list` accept `n` and `last`. Without `n`, no limit is applied; a negative or non-numeric `n` is `400`. Registry errors follow `{ "errors": [{"code":"...","message":"..."}] }`.

Both `DELETE` routes are gated by the opt-in `-delete-enabled`/`REGISTRY_DELETE_ENABLED` flag (default `false`). When the flag is off, an otherwise-authorized caller gets `UNSUPPORTED` (`400`), the same acknowledged-but-refused shape `blobs/uploads/<id>` DELETE already uses, never a bare `405`. Deletion requires the distinct `delete` scope action (`repository:<name>:delete`) and never removes blob files on disk.

Relevant headers: `Docker-Content-Digest`, `Docker-Upload-UUID`, `Location`, `Range`, `Content-Length`, `Content-Type`, `WWW-Authenticate`.

Implementation: `internal/protocol/http/router.go` and `internal/protocol/http/admin_handlers.go`.
