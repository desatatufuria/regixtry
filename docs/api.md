# HTTP API

The implementation registers `/auth/token` only when auth is configured, `/admin/v1...` only when the service offers the admin interface, and always registers `/v2` / `/v2/`.

## Authentication API

### `GET|POST /auth/token`

Accepts Basic Auth and repeated `scope` parameters; `service` is optional and reflected back in the response. Returns `200` with `token`, `access_token`, `expires_in`, `issued_at`, and optionally `service`/`scope`. Invalid credentials return `401` with a Registry-style error. The endpoint accepts either a user's password or a pre-issued admin credential token.

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## Management API

Every route under `/admin/v1` requires a Bearer token for a global admin (`Principal.IsAdmin`), with one deliberate exception: `/admin/v1/repositories/{repo}/grants...` is reachable by any authenticated principal. The HTTP layer only authenticates the caller there (`requireAuthenticatedPrincipal`, no `IsAdmin` check); the service layer then decides admin-or-repo-admin authority per repository, since only it has the repository argument in scope. In practice this means a user holding the `repo-admin` role on that one repository (a delegate, not a global admin) can list, grant, and revoke grants for that repository — everything else under `/admin/v1` stays global-admin-only. See `internal/protocol/http/admin_handlers.go`'s `handleAdmin` for the exact prefix check.

JSON bodies are decoded rejecting unknown fields and multiple bodies.

| Method | Endpoint | Body | Success |
| --- | --- | --- | --- |
| GET | `/admin/v1/users` | none | `200`, array of users |
| POST | `/admin/v1/users` | `username`, `password`, `is_admin`, `enabled` | `201`, user |
| POST | `/admin/v1/users/{id}:enable` | none | `200`, user |
| POST | `/admin/v1/users/{id}:disable` | none | `200`, user |
| POST | `/admin/v1/users/{id}:reset-password` | `new_password` | `204` |
| GET | `/admin/v1/users/{id}/grants` | none | `200`, array of grants |
| PUT | `/admin/v1/users/{id}/grants/{repository}` | `role` | `200`, grant |
| DELETE | `/admin/v1/users/{id}/grants/{repository}` | none | `204` |
| GET | `/admin/v1/users/{id}/admin-tokens` | none | `200`, array without secrets |
| POST | `/admin/v1/users/{id}/admin-tokens` | `name`, optional `ttl_seconds` | `201`, token and secret |
| DELETE | `/admin/v1/users/{id}/admin-tokens/{accessor}` | none | `204` |
| GET | `/admin/v1/robots` | none | `200`, array of robot accounts |
| POST | `/admin/v1/robots` | `name`, `repository`, `role`, optional `ttl_seconds` | `201`, robot account with one-time token secret |
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
| PUT | `/admin/v1/signing-policy` | `enabled`, `trusted_public_keys` (up to 16 PEM-encoded ECDSA P-256 keys) | `200`, persisted policy |
| POST | `/admin/v1/scan-runs` | `repository`, `reference` | `202`, queued run with canonical digest |
| GET | `/admin/v1/scan-runs?repository=&limit=` | none | `200`, run history |
| GET | `/admin/v1/scan-runs/{id}` | none | `200`, run detail with persisted findings, DB freshness, and reference freshness |
| GET | `/admin/v1/secret-scan-findings?repository=&digest=` | none | `200`, secret-scan findings for that repository/digest pair |
| GET | `/admin/v1/repositories/{repo}/grants` | none | `200`, array of grants for that repository |
| PUT | `/admin/v1/repositories/{repo}/grants/{username}` | `role` | `200`, grant |
| DELETE | `/admin/v1/repositories/{repo}/grants/{username}` | none | `204` |

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

Keys must be PEM-encoded ECDSA P-256 public keys; up to 16 entries are accepted. `enabled: true` with zero usable keys is rejected as a validation error.

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

`_catalog` and `tags/list` accept `n` and `last`. Without `n`, no limit is applied; a negative or non-numeric `n` is `400`. Registry errors follow `{ "errors": [{"code":"...","message":"..."}] }`.

Both `DELETE` routes are gated by the opt-in `-delete-enabled`/`REGISTRY_DELETE_ENABLED` flag (default `false`). When the flag is off, an otherwise-authorized caller gets `UNSUPPORTED` (`400`), the same acknowledged-but-refused shape `blobs/uploads/<id>` DELETE already uses, never a bare `405`. Deletion requires the distinct `delete` scope action (`repository:<name>:delete`) and never removes blob files on disk.

Relevant headers: `Docker-Content-Digest`, `Docker-Upload-UUID`, `Location`, `Range`, `Content-Length`, `Content-Type`, `WWW-Authenticate`.

Implementation: `internal/protocol/http/router.go` and `internal/protocol/http/admin_handlers.go`.
