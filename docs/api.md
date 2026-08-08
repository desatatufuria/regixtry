# API HTTP

La implementación registra `/auth/token` solo cuando auth está configurada, `/admin/v1...` cuando el servicio ofrece la interfaz administrativa y siempre `/v2`/`/v2/`.

## Authentication API

### `GET|POST /auth/token`

Acepta Basic Auth y parámetros repetidos `scope`; `service` es opcional y se refleja en la respuesta. Devuelve `200` con `token`, `access_token`, `expires_in`, `issued_at` y opcionalmente `service`/`scope`. Credenciales inválidas devuelven `401` con error Registry. El endpoint acepta password de usuario o admin credential token preemitido.

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## Management API

Todas estas rutas requieren Bearer de un usuario admin. Los JSON se decodifican rechazando campos desconocidos y cuerpos múltiples.

| Método | Endpoint | Body | Éxito |
| --- | --- | --- | --- |
| GET | `/admin/v1/users` | ninguno | `200`, array de usuarios |
| POST | `/admin/v1/users` | `username`, `password`, `is_admin`, `enabled` | `201`, usuario |
| POST | `/admin/v1/users/{id}:enable` | ninguno | `200`, usuario |
| POST | `/admin/v1/users/{id}:disable` | ninguno | `200`, usuario |
| POST | `/admin/v1/users/{id}:reset-password` | `new_password` | `204` |
| GET | `/admin/v1/users/{id}/grants` | ninguno | `200`, array de grants |
| PUT | `/admin/v1/users/{id}/grants/{repository}` | `role` | `200`, grant |
| DELETE | `/admin/v1/users/{id}/grants/{repository}` | ninguno | `204` |
| GET | `/admin/v1/users/{id}/admin-tokens` | ninguno | `200`, array sin secretos |
| POST | `/admin/v1/users/{id}/admin-tokens` | `name`, opcional `ttl_seconds` | `201`, token y secreto |
| DELETE | `/admin/v1/users/{id}/admin-tokens/{accessor}` | ninguno | `204` |

Errores administrativos: `401`, `403`, `404`, `409`, `422` según auth, existencia, conflicto o validación; el cuerpo es `{ "error": "..." }`.

## Registry API V2

| Método | Endpoint | Auth/éxito |
| --- | --- | --- |
| GET/HEAD | `/v2/` | check de registry; `200` o `401` |
| GET | `/v2/_catalog` | catalog; `200` JSON `{repositories:[...]}` |
| GET | `/v2/<repo>/tags/list` | tags; `200` JSON `{name,tags}` |
| POST | `/v2/<repo>/blobs/uploads/` | inicia upload; `202` |
| GET/HEAD | `/v2/<repo>/blobs/uploads/<id>` | estado; `204` |
| PATCH | `/v2/<repo>/blobs/uploads/<id>` | añade chunk; `202` |
| PUT | `/v2/<repo>/blobs/uploads/<id>?digest=sha256:...` | completa; `201` |
| DELETE | `/v2/<repo>/blobs/uploads/<id>` | no implementado; `UNSUPPORTED` |
| GET/HEAD | `/v2/<repo>/blobs/<digest>` | blob; `200` |
| PUT | `/v2/<repo>/manifests/<tag-or-digest>` | publica manifest; `201` |
| GET/HEAD | `/v2/<repo>/manifests/<tag-or-digest>` | manifest; `200` |

`_catalog` y `tags/list` aceptan `n` y `last`. Sin `n`, no se aplica límite; `n` negativo o no numérico es `400`. Los errores Registry tienen `{ "errors": [{"code":"...","message":"..."}] }`.

Headers relevantes: `Docker-Content-Digest`, `Docker-Upload-UUID`, `Location`, `Range`, `Content-Length`, `Content-Type`, `WWW-Authenticate`.

Implementación: `internal/protocol/http/router.go` y `internal/protocol/http/admin_handlers.go`.
