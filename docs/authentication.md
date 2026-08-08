# Autenticación y usuarios

## Activación

La auth se activa pasando `-auth-postgres-dsn` o `REGISTRY_AUTH_POSTGRES_DSN`. Al abrir PostgreSQL se crean las tablas mediante `internal/infra/auth/postgres/migrations.go`. El runtime falla si no existe un administrador global activo.

## Bootstrap

```bash
printf '%s\n' '<admin-password>' | regixtry bootstrap-admin \
  -auth-postgres-dsn 'postgres://USER:PASSWORD@HOST:5432/regixtry_auth?sslmode=disable' \
  -username admin -password-stdin
```

`-password` existe, pero el código advierte que expone el secreto en argv. `-rotate-password` permite rotarlo si el admin ya existe.

## Challenge y token

Una solicitud protegida recibe `401` y `WWW-Authenticate: Bearer realm="...",service="...",scope="..."`. El cliente usa Basic contra el realm:

```bash
curl -u USERNAME:PASSWORD \
  'https://registry.example.com/auth/token?service=regixtry&scope=repository:team/image:pull'
```

La respuesta contiene `token`, `access_token`, `expires_in` (`900`), `issued_at` y, si corresponde, `scope`. También se acepta un admin credential token preemitido como secreto Basic.

```bash
printf '%s\n' '<PASSWORD>' | docker login registry.example.com -u USERNAME --password-stdin
docker logout registry.example.com
```

Los access tokens duran 15 minutos. Se guarda SHA-256 del secreto, no el secreto plano. Las credenciales de usuario se verifican con bcrypt. Los admin credential tokens tienen un TTL por defecto de 30 días; el servicio aplica límites y revocación.

## Autorización

Roles válidos: `repo-reader`, `repo-writer`, `repo-admin`. Reader permite leer, writer leer/escribir y admin añade la capacidad de administración del repositorio en el dominio, aunque la API HTTP expuesta actualmente no ofrece operaciones de administración de contenido.

Los scopes válidos son `repository:<name>:pull`, `repository:<name>:push` o ambos, y `regixtry:catalog:*`. Un usuario admin global bypassa grants de repositorio, pero el token aún debe incluir un scope compatible con la operación solicitada.

## API administrativa

Todas las rutas `/admin/v1` requieren `Authorization: Bearer <access-token>` y `Principal.IsAdmin`. Las rutas implementadas son:

- `GET /admin/v1/users`
- `POST /admin/v1/users`
- `POST /admin/v1/users/{id}:enable`
- `POST /admin/v1/users/{id}:disable`
- `POST /admin/v1/users/{id}:reset-password`
- `GET /admin/v1/users/{id}/grants`
- `PUT /admin/v1/users/{id}/grants/{repository}`
- `DELETE /admin/v1/users/{id}/grants/{repository}`
- `GET /admin/v1/users/{id}/admin-tokens`
- `POST /admin/v1/users/{id}/admin-tokens`
- `DELETE /admin/v1/users/{id}/admin-tokens/{accessor}`

Los cuerpos y ejemplos completos están en [api.md](api.md). No hay delete-user expuesto en esta API.
