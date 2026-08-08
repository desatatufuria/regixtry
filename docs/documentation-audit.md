# Auditoría de documentación

## Funcionalidad documentada

- Binary release installer y build desde fuente.
- `serve`, `tui`, `bootstrap-admin`, `bootstrap`, `setup`, `uninstall` y `upgrade`.
- Configuración de bind, URL pública, TLS, storage, SQLite, PostgreSQL, auth y timeouts.
- Arquitectura por capas y persistencia filesystem/SQLite/PostgreSQL.
- Bearer challenge, Basic token exchange, access tokens, scopes, grants y roles.
- API `/auth/token`, `/admin/v1` y rutas Registry `/v2` implementadas.
- Push/pull, uploads chunked, tags, catálogo y HEAD.
- Compose local, systemd, CI/CD, TUI, operación, seguridad y troubleshooting.

## Funcionalidad parcialmente entendida

- La compatibilidad exacta con toda la OCI Distribution Specification no se demuestra con un test de conformidad completo.
- Multi-arch/index manifests no se confirmó de forma suficiente a partir de la implementación inspeccionada.
- El comportamiento práctico de cada daemon Docker frente a HTTP inseguro depende de su configuración externa.
- El backup/restore operativo no tiene comandos propios en el código.

## Funcionalidad aparentemente incompleta

- Cancelación de upload: la ruta `DELETE` existe en el dispatcher, pero devuelve `UNSUPPORTED`.
- La interfaz de storage define cancelación, pero la API pública no la expone funcionalmente.
- La TUI puede listar y cambiar enablement de usuarios, pero no cubre todas las mutaciones del admin API.
- El job runner es inline; no hay ejecución asíncrona persistente.

## Código potencialmente muerto o no expuesto

- Existen interfaces y métodos de servicio para operaciones más amplias, incluido delete de usuario y acciones de repositorio, sin una ruta HTTP pública equivalente en esta versión.
- No se marca código como muerto con certeza: se requiere análisis de cobertura/runtime adicional para distinguir seams futuros de código no usado.

## Ausencias importantes verificadas

- No hay health endpoint separado de `/v2/`.
- No hay endpoint de métricas ni logging estructurado confirmado.
- No hay garbage collection.
- No hay API de delete de manifest/blob.
- No hay almacenamiento remoto, replicación ni multi-tenant.
- No hay autorización granular independiente de grants/scopes más allá del modelo existente.
- No hay flujo de refresh token silencioso en la TUI.

## Tests y contradicciones

El repositorio contiene tests unitarios para dominio, aplicación, stores, router, auth, lifecycle, releases y TUI, además de scripts de smoke bajo `docs/verification/scripts/`. La documentación usa los contratos observados en handlers y structs; los ejemplos no deben interpretarse como certificación completa de Docker/OCI.

## Preguntas abiertas

- ¿Se desea declarar formalmente qué subconjunto de OCI Distribution se soporta y probarlo contra una suite de conformidad?
- ¿Debe implementarse una política futura de retención/garbage collection?
- ¿La TUI debe recibir las mutaciones administrativas restantes o debe mantenerse como cliente parcial?
- ¿Se requiere una guía oficial de backup/restore para SQLite, blobs y PostgreSQL?
