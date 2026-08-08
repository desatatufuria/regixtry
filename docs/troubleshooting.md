# Troubleshooting

## El servicio no arranca

**Síntoma:** `systemctl` muestra fallo.

**Diagnóstico:**

```bash
systemctl status regixtry
journalctl -u regixtry -n 100 --no-pager
```

Revisar URL pública, paths TLS, DSN, permisos y si existe un admin activo cuando auth está habilitada.

## Puerto ocupado

**Síntoma:** setup informa que el bind local está ocupado.

```bash
ss -ltnp | grep ':5000'
```

Cambiar `-addr` o detener el proceso que usa el puerto.

## `docker login` falla

Confirmar que `REGISTRY_PUBLIC_URL` coincide con la URL usada por Docker y que `/auth/token` es accesible. Verificar que el usuario esté habilitado, la contraseña sea correcta y exista un admin inicial.

```bash
curl -i https://registry.example.com/v2/
curl -u USERNAME:PASSWORD 'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## `push` devuelve `UNAUTHORIZED` o `DENIED`

El token debe tener scope de push y el usuario debe poseer grant writer/admin para ese repository. Revisar `GET /admin/v1/users/{id}/grants`.

## `push` devuelve digest/manifest inválido

El blob debe completarse con el digest SHA-256 correcto. Un manifest se rechaza si referencia blobs ausentes. Revisar la secuencia `POST`, `PATCH`, `PUT` y el parámetro `digest`.

## Base de datos inaccesible

Revisar `-db`, `REGISTRY_DATABASE_PATH`, permisos del directorio y que el archivo SQLite pertenezca al usuario del servicio. Para auth, validar el DSN y conectividad PostgreSQL.

## TLS falla

Certificado y clave deben existir y ser entregados juntos. `http` no puede combinarse con TLS inputs. En `direct-tls`, usar `https://` en `-public-url` y confiar en la CA desde Docker/curl.

## El TUI no carga admin

Usar `-api-base-url` con una URL absoluta HTTP/HTTPS y credenciales válidas. La TUI inspecciona localmente, pero las mutaciones administrativas no implementadas se muestran como no disponibles.
