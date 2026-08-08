# Usuarios

La gestión de usuarios se realiza mediante `bootstrap-admin` para el primer admin y mediante `/admin/v1` para operaciones posteriores.

## Crear usuario

```bash
curl -X POST 'https://registry.example.com/admin/v1/users' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"username":"builder","password":"<PASSWORD>","is_admin":false,"enabled":true}'
```

Los usernames se normalizan a minúsculas. La respuesta no expone el hash.

## Grant

```bash
curl -X PUT 'https://registry.example.com/admin/v1/users/USER_ID/grants/team%2Fimage' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"role":"repo-writer"}'
```

## Token administrativo preemitido

```bash
curl -X POST 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci","ttl_seconds":2592000}'
```

El secreto aparece en la respuesta de creación y no debe registrarse. Para revocarlo:

```bash
curl -X DELETE 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens/ACCESSOR' \
  -H "Authorization: Bearer ${TOKEN}"
```

## Limitaciones

La última cuenta admin activa no puede deshabilitarse. El TUI soporta listar usuarios, grants y tokens, y habilitar/deshabilitar usuarios; las demás mutaciones deben hacerse por HTTP.
