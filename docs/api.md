# HTTP API

The implementation registers `/auth/token` only when auth is configured, `/admin/v1...` only when the service offers the admin interface, and always registers `/v2` / `/v2/`.

## Authentication API

### `GET|POST /auth/token`

Accepts Basic Auth and repeated `scope` parameters; `service` is optional and reflected back in the response. Returns `200` with `token`, `access_token`, `expires_in`, `issued_at`, and optionally `service`/`scope`. Invalid credentials return `401` with a Registry-style error. Every credential failure is indistinguishable to the caller: an unknown username, a disabled user, a robot trying a password and a wrong password all give the same `401` and the same `invalid credentials` body, and each one costs one bcrypt comparison (against a dummy hash when no real one applies), so neither the response nor its timing reveals whether a username exists or is disabled. The real reason is only written to the server log (`login failed endpoint=/auth/token ip=... reason=... account=...`, with an `account_hash=` instead of the name for an unknown username) and to an in-process counter; passwords and tokens are never logged. The endpoint accepts either a user's password or a pre-issued admin credential token. A secret of exactly 64 lowercase hex characters is tried first as a credential token, which skips bcrypt; a miss falls back to the normal flow, so success and failure responses are unchanged.

Under a real flood of password logins, bcrypt concurrency is capped (`-login-max-concurrency`): a request that cannot get a slot within 250 ms is refused with `429`, header `Retry-After: 1` and the registry error code `TOOMANYREQUESTS`, without its password being checked. The `429` has no `WWW-Authenticate` challenge and is identical for every username. Clients should wait `Retry-After` seconds and retry. The same `429` is returned while the username is in a per-account backoff (`-login-backoff-mode=enforce`; the default `log` mode never refuses), with `Retry-After` set to the remaining wait in seconds. It looks the same for existing and unknown usernames. A valid credential token is never refused for backoff.

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## Management API

Every route under `/admin/v1` requires a Bearer token for a global admin (`Principal.IsAdmin`), with one deliberate exception: `/admin/v1/repositories/{repo}/grants...` is reachable by any authenticated principal. The HTTP layer only authenticates the caller there (`requireAuthenticatedPrincipal`, no `IsAdmin` check); the service layer then decides admin-or-repo-admin authority per repository, since only it has the repository argument in scope. In practice this means a user holding the `repo-admin` role on that one repository (a delegate, not a global admin) can list, grant, and revoke grants for that repository — everything else under `/admin/v1` stays global-admin-only. See `internal/protocol/http/admin_handlers.go`'s `handleAdmin` for the exact prefix check.

A second exception follows the same pattern: `/admin/v1/projects/{project}/members...` is reachable by any authenticated principal, and the service decides. A global admin may do everything there; a user holding the project admin role (`repo-admin` on the project) may list the members of that one project and grant or revoke `repo-reader` and `repo-writer` for users and robots of that project only. A project admin cannot grant project admin, cannot change or revoke another project admin, cannot act on another project (`odabank` never covers `odabank2`, nor a flat repository named `odabank`), and cannot create or delete projects. The caller is checked before the project is looked up, so a caller without rights gets the same `403` whether or not the project exists. Creating, listing and deleting projects, and the per-user project grants, stay global-admin-only.

The Bearer token must also be unrestricted: an access token that carries scopes (for example one obtained with `?scope=repository:team/image:pull`, which is what `docker login` and CI pulls use) is refused on every `/admin/v1` route with `403`, even when its owner is a global admin or a repo-admin. This includes a token that was requested with scopes but was granted none of them. Upgrade note: access tokens issued before this change whose requested scopes intersected to nothing carry an empty scope and stay unrestricted until they expire (15 minutes). Administrative clients (the TUI and CLI) log in without `scope`, which produces the unrestricted token these routes accept.

Resetting a password (`:reset-password`) replaces the password hash and, in the same transaction, revokes the user's active login sessions: their access tokens stop working at once (`401`). This includes the caller's own session when an admin resets their own password; they must log in again. Admin credential tokens (the PAT-like tokens managed on the Tokens screen) are not touched and still exchange for new access tokens at `/auth/token`; revoke them explicitly. The `user.password_reset` access event is recorded as before. A new password (on create, reset and `bootstrap-admin`) must have at least 8 characters and must not be on the built-in common-password list; otherwise `422` with `password is too common; choose a less guessable one`. Existing passwords are not re-checked. A robot has no password, so resetting one is rejected with `422` (`target user is a robot account and has no password`) and nothing is changed or recorded.

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
| GET | `/admin/v1/access-events` | query `limit` (1 to 200; default 20); invalid values give `422` | `200`, array (newest first) of `id`, `at`, `action`, `actor`, `subject`, `repository`, `role`, `token_name`, `detail`; empty fields are omitted; `action` is `grant.put` (repository grant given or changed), `grant.delete` (grant revoked), `token.issued`, `token.revoked`, `robot.created`, `robot.deleted`, `user.created` (`detail` lists the role flags: `administrator`, `read-only` or `standard user`), `user.enabled`, `user.disabled`, `user.updated` (`detail` lists only the flags that changed, such as `administrator on`; no event when none changed), `user.password_reset` (never carries a password) or `user.deleted` (the deleted username stays in `subject`), `project.grant.put` and `project.grant.delete` (a project grant given, changed or revoked; `subject` is the member, `role` the new role, `detail` such as `Reader to Writer`), `project.created` or `project.deleted`; the events of a project carry the project in `repository` as `project:<name>` (for example `project:odabank`), and `robot.created` for a project robot does the same; names and short text only, never a secret, hash, accessor or token id; global admin only |
| GET | `/admin/v1/repository-grants` | query `limit` (1 to 200; default 100) and `after` (the opaque `next` of the previous page); invalid values or a malformed cursor give `422` | `200`, object `{"grants":[...],"next":"..."}`; `grants` holds the grants of every user and robot on every repository, including repositories that were never pushed, ordered by repository then username, each with `repository`, `user_id`, `username`, `is_robot`, `role`, `created_at`, `updated_at`; `next` is empty on the last page; disabled users still appear; never a secret, hash or token; global admin only |
| GET | `/admin/v1/projects` | none | `200`, object `{"projects":[...]}` ordered by name, each with `name`, `created_at`, `created_by`, `repositories`, `tags`, `last_pushed` (`null` when nothing was pushed); the counts include only repositories whose first path segment is the project; global admin only |
| POST | `/admin/v1/projects` | `name` (one lowercase segment of letters, digits and the separators `.` `_` `-`, at most 64 characters, no slash, not reserved: `blobs`, `manifests`, `tags`, `referrers`, `grants`, `repository-overrides`) | `201`, the project; `409` if it exists (`project "odabank" already exists`); `422` for an invalid or reserved name (`project name "blobs" is reserved`); global admin only. Stale project grants left under that name by an earlier failed cleanup are purged first |
| GET | `/admin/v1/projects/{project}` | none | `200`, the project; `404` if unknown; global admin only |
| DELETE | `/admin/v1/projects/{project}` | none | `200`, `{"deleted":"odabank"}`; only an EMPTY project (no repository, no open upload): `409` otherwise with a message such as `project "odabank" still has 3 repositories; delete or finish them first`; `422` `project deletion is not enabled` unless the server runs with `-delete-enabled` (same switch as deleting repositories); `404` if unknown. Its grants are purged afterwards; global admin only. If the project row was deleted but its grants could not be purged the answer is `503` `project "x" was deleted but its grants could not be purged; retry the delete`: retry the same DELETE, which converges (`404` only when there was nothing left to clean) and writes no second `project.deleted` event (that event is written once, when the row goes); the cleanup call is bounded to 15 seconds |
| GET | `/admin/v1/projects/{project}/members` | query `limit` (1 to 200; default 100) and `after` (the opaque `next` of the previous page); invalid values or a malformed cursor give `422` | `200`, object `{"members":[...],"next":"..."}`; `members` holds the users and robots with a grant on the project, each with `username`, `is_robot`, `role`, `created_at`, `updated_at`; no user id, hash or token; `next` is empty on the last page; `404` if the project does not exist (after authorization); global admin or admin of that project |
| PUT | `/admin/v1/projects/{project}/members/{username}` | `role` (`repo-reader`, `repo-writer`, `repo-admin`) | `200`, the member; `403` when a project admin asks for `repo-admin` or targets another project admin; `422` for an invalid role, for `repo-writer` or `repo-admin` given to a read-only user (they can only be readers), and for `repo-admin` given to a robot; `409` for a robot that already holds any other grant (a robot holds exactly one grant, whichever route gives it: the repository grant routes answer `409` for a robot bound to a project too); `404` for an unknown user or project; global admin or admin of that project |
| DELETE | `/admin/v1/projects/{project}/members/{username}` | none | `204`; `404` when the user has no grant on the project (works for a project that no longer exists, to clear an orphan grant); a project admin cannot remove another project admin (`403`); exact repository grants are never touched; global admin or admin of that project |
| GET | `/admin/v1/users/{id}/project-grants` | none | `200`, array (ordered by project) of `project`, `role`, `created_at`, `updated_at`; global admin only |
| GET | `/admin/v1/robots` | none | `200`, array of robot accounts |
| POST | `/admin/v1/robots` | `name`, EITHER `repository` OR `project` (never both; `repository` keeps today's behavior), `role` (`repo-reader` or `repo-writer` for a project robot, never `repo-admin`; a project robot covers every repository of the project, now and later), optional `ttl` (number plus `h`, `d` or `y`, for example `12h`, `30d`, `2y`; default 30 days; maximum 2 years), or the older `ttl_seconds` (seconds; maximum `63072000`), never both | `201`, robot account with one-time token secret; the robot shows `project` instead of `repository` when bound to a project (`repository` is then empty); a project that does not exist gives `404` |
| DELETE | `/admin/v1/robots/{id}` | none | `204`; rejected if the target is not a robot account |
| GET | `/admin/v1/features` | none | `200`, built-in feature inventory |
| GET | `/admin/v1/features/{name}` | none | `200`, feature-owned configuration |
| GET | `/admin/v1/features/{name}/status` | none | `200`, feature configuration plus runtime health |
| PUT | `/admin/v1/features/{name}/config` | `enabled`, `schedule_enabled`, `interval`, `timeout`, `service_url`, `registry_reachable_url`, optional `auth_token`, `tls_ca_cert_path`, `tls_insecure_skip_verify`, `max_concurrency` | `200`, persisted feature state |
| POST | `/admin/v1/features/{name}:enable` | none | `200`, enabled feature state |
| POST | `/admin/v1/features/{name}:disable` | none | `200`, disabled feature state |
| POST | `/admin/v1/features/{name}:install` | optional `version`: `v1.2.3` or `1.2.3`, optionally with a `-prerelease` suffix, at most 64 characters; anything else (paths, `..`, queries, fragments, percent escapes, whitespace, `latest`) gives `422`; empty means the latest release | `200`, runtime state |
| POST | `/admin/v1/features/{name}:upgrade` | optional `version`: `v1.2.3` or `1.2.3`, optionally with a `-prerelease` suffix, at most 64 characters; anything else (paths, `..`, queries, fragments, percent escapes, whitespace, `latest`) gives `422`; empty means the latest release | `200`, runtime state |
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

Example: create a project, give a user a project role, then page through its members (global admin token for the first two calls; a project admin of `odabank` may also make the last two):

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" -d '{"name":"odabank"}' \
  "https://registry.example.com/admin/v1/projects"
curl -X PUT -H "Authorization: Bearer $TOKEN" -d '{"role":"repo-writer"}' \
  "https://registry.example.com/admin/v1/projects/odabank/members/ana"
curl -H "Authorization: Bearer $TOKEN" \
  "https://registry.example.com/admin/v1/projects/odabank/members?limit=2"
```

```json
{"name":"odabank","created_at":"2026-03-01T09:00:00Z","created_by":"admin","repositories":0,"tags":0,"last_pushed":null}
{"username":"ana","is_robot":false,"role":"repo-writer","created_at":"2026-03-01T09:01:00Z","updated_at":"2026-03-01T09:01:00Z"}
{"members":[{"username":"ana","is_robot":false,"role":"repo-writer","created_at":"2026-03-01T09:01:00Z","updated_at":"2026-03-01T09:01:00Z"}],"next":""}
```

A project role covers every repository whose first path segment is the project, now and later, and takes effect on the next request (tokens are not rebuilt or revoked; the grants are read on each request, so a token issued before a revoke is refused on its next use). The role of an exact repository grant can only add access. Revoking the project grant removes the access it gave; exact repository grants stay. Project names are one lowercase path segment: `odabank` never covers `odabank2/...` nor a flat repository called `odabank`.

Events are recorded after the change succeeded and on a best-effort basis: a failed history write never fails the change itself.

See [registry.md](registry.md#blob-garbage-collection) for the full garbage-collection flow, including the grace window and delete-time re-check that keep it safe.

Security notes for the member routes: a project admin can tell whether a username exists from the answer to a guessed name (`404` unknown user), as on the repository grant routes; and a grant put concurrently with a project delete can land just after its purge, leaving a harmless orphan grant that the next create of that name purges.

Admin errors: `401` (with a `WWW-Authenticate` challenge), `403`, `404`, `409`, `422`, `501`, `503` (a store failed mid-operation; the body says what to retry) depending on auth, existence, conflict, validation, or a capability that is disabled in this deployment; the body is `{ "error": "..." }`.

An unexpected server failure on an admin endpoint answers `500` with the body `{ "error": "internal server error" }`. The cause is not returned to the client; it is recorded in the server log together with the request.

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

Scan results are latest-only: a digest keeps one completed result per repository, and a completed scan replaces the previous one atomically. Run objects carry these additive fields (all omitted when empty): `settings_hash` (hash of the effective scan configuration), `db_version`, `last_checked_at` (when a push or sweep last confirmed the result was still current without rescanning, because the scanner version, vulnerability database and scan configuration were unchanged), and `stale` plus `stale_reason` (a rescan failed: the previous completed result is kept and still enforced by the pull policy). A manual rescan always scans and replaces the result. `db_version` and `db_updated_at` come from the Trivy cache metadata read after the scan, and `settings_hash` is taken before it. If two scans of one digest complete out of order, the result with the newer `db_updated_at` is kept (compared only when both are known; otherwise the last completion wins).

`GET /admin/v1/features/trivy/status` adds an additive `sweep` object once a scheduled sweep has run (absent before): `started_at`, `finished_at`, `visited` (tagged digests examined), `skipped_unchanged` (latest result already current: no new run), `queued` (new scan runs), `errors` (digests or repositories that could not be examined), `last_db_refresh_at` and `last_db_refresh_error` (outcome of the once-per-sweep vulnerability database download: the error is omitted on success, and a failed download never aborts the sweep, which continues with the cached database), `db_fingerprint` (database version and `updated_at` the sweep ran against) and `db_changed` (it differs from the previous sweep's). The Trivy feature page (`GET /admin/v1/features/trivy`) shows the same data in a `Last sweep` section.

## Registry API V2

| Method | Endpoint | Auth/success |
| --- | --- | --- |
| GET/HEAD | `/v2/` | Registry check; `200` or `401` |
| GET | `/v2/_catalog` | Catalog; `200` JSON `{repositories:[...]}` |
| GET | `/v2/<repo>/tags/list` | Tags; `200` JSON `{name,tags}` |
| POST | `/v2/<repo>/blobs/uploads/` | Starts upload; `202` |
| GET/HEAD | `/v2/<repo>/blobs/uploads/<id>` | Upload state; `204` |
| PATCH | `/v2/<repo>/blobs/uploads/<id>` | Appends a chunk; `202` |
| PUT | `/v2/<repo>/blobs/uploads/<id>?digest=sha256:...` | Completes upload and links the blob to `<repo>`; `201` |
| DELETE | `/v2/<repo>/blobs/uploads/<id>` | Cancels the upload (session row and temp data); `204`. Needs `push` on the upload's own repository; an upload of another repository is `404` |
| GET/HEAD | `/v2/<repo>/blobs/<digest>` | Blob; `200`. Served only if `<repo>` holds the blob (uploaded into it or referenced by one of its manifests); otherwise `404 BLOB_UNKNOWN` even if another repository has it |
| PUT | `/v2/<repo>/manifests/<tag-or-digest>` | Publishes manifest; `201` |
| GET/HEAD | `/v2/<repo>/manifests/<tag-or-digest>` | Manifest; `200` |
| DELETE | `/v2/<repo>/manifests/<digest>` | Deletes the manifest, cascading to every tag pointing at it; `202` JSON body naming what was removed; `404` if absent |
| DELETE | `/v2/<repo>/manifests/<tag>` | Untags only, leaving the manifest and its other tags intact; `202`; `404` if absent |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/scan-status` | CI-facing scan verdict; pull-credential auth; always `200` |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/signature-status` | CI-facing signature verdict; pull-credential auth; always `200` |
| GET | `/v2/<repo>/manifests/<tag-or-digest>/secret-scan-status` | CI-facing secret-scan verdict; pull-credential auth; always `200` |

`_catalog` and `tags/list` accept `n` and `last`. Without `n`, no limit is applied; a negative or non-numeric `n` is `400`. Registry errors follow `{ "errors": [{"code":"...","message":"..."}] }`.

Project enforcement (`-require-project`, on by default when auth is enabled; see [configuration.md](configuration.md)): a push is refused unless its project exists. The check runs after authorization, so a caller without push permission gets the same `401` or `403` whether or not the project exists, and it applies when an upload starts, when an upload is completed and when a manifest is published. Reads (pull, tags, catalog, referrers) never consult projects.

| Case | Status | Body |
| --- | --- | --- |
| Push to `ghost/app`, project `ghost` does not exist | `404` | `{"errors":[{"code":"NAME_UNKNOWN","message":"project \"ghost\" does not exist; ask an administrator to create it"}]}` |
| First push of a new flat name such as `flat` | `400` | `{"errors":[{"code":"NAME_INVALID","message":"repository \"flat\" has no project; use <project>/flat (flat repository names are no longer created)"}]}` |
| Push to an existing flat repository | as before | existing flat repositories keep working |

With `-require-project=false` none of these refusals exists and pushes behave as before projects were introduced. Without `-auth-postgres-dsn` the `/admin/v1` routes are not registered (`404`), so a deployment without auth cannot create projects: enforcement defaults to off there, and an explicit `-require-project=true` without a DSN fails at startup.

Both `DELETE` routes are gated by the opt-in `-delete-enabled`/`REGISTRY_DELETE_ENABLED` flag (default `false`). When the flag is off, an otherwise-authorized caller gets `UNSUPPORTED` (`400`), an acknowledged-but-refused shape rather than a bare `405`. Deletion requires the distinct `delete` scope action (`repository:<name>:delete`) and never removes blob files on disk.

Limits: a manifest `PUT` body above `-max-manifest-bytes` (default 4 MiB) is refused with `413` and OCI code `SIZE_INVALID`; a blob upload whose accumulated size (across all `PATCH`/`PUT` chunks) would exceed `-max-upload-bytes` (default 5 GiB) is refused the same way and keeps its previous size; opening an upload beyond `-max-open-uploads` / `-max-open-uploads-per-repo` is `429` with `TOOMANYREQUESTS`. A `PUT` that commits an upload while a `PATCH` to the same upload is still streaming is `409`. Uploads idle longer than `-upload-ttl` (default 24h) are deleted by a background reaper. See [configuration.md](configuration.md).

Relevant headers: `Docker-Content-Digest`, `Docker-Upload-UUID`, `Location`, `Range`, `Content-Length`, `Content-Type`, `WWW-Authenticate`.

Implementation: `internal/protocol/http/router.go` and `internal/protocol/http/admin_handlers.go`.
