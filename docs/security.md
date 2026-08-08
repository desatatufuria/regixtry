# Seguridad

## Confirmado por el código

- Passwords de usuarios se verifican con bcrypt.
- Tokens y credenciales preemitidas se almacenan como SHA-256 del secreto.
- Access tokens expiran a los 15 minutos y pueden revocarse los admin tokens.
- `/admin/v1` exige Bearer válido y usuario admin.
- Los grants aplican roles `repo-reader`, `repo-writer`, `repo-admin`.
- TLS directo exige certificado y clave juntos; la configuración rechaza combinaciones inconsistentes.
- El instalador valida checksums SHA-256 de los releases.

## Recomendaciones operativas

- Preferir `-password-stdin` en lugar de `-password`.
- No exponer PostgreSQL ni el HTTP local sin una red controlada.
- Usar HTTPS directo o un reverse proxy TLS en redes no confiables.
- Proteger `regixtry.env`, bootstrap state, SQLite y el filesystem de blobs.
- Guardar el secreto de un admin token solo al crearlo.

Estas son recomendaciones, no garantías implementadas por el programa.

## Riesgos y límites

- `local-http` transmite credenciales y tokens sin cifrado si se usa fuera de una red confiable.
- El env file generado puede contener el DSN PostgreSQL; sus permisos reales derivan de la escritura del bootstrap y deben revisarse operacionalmente.
- No hay métricas, rate limiting ni auditoría de eventos confirmados en el código.
- ⚠️ No se ha podido confirmar a partir del código actual una revisión formal de la superficie OCI completa.
