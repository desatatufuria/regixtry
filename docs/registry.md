# Registry y Docker/OCI

## Flujo de `docker login`

Con auth habilitada, Docker solicita `/v2/`, recibe Bearer challenge, llama `/auth/token` con Basic y reintenta `/v2/` con el access token. El token es corto y se almacena en el proceso cliente; Regixtry conserva solo su hash.

## Push

```bash
docker login registry.example.com
docker tag my-image:latest registry.example.com/team/my-image:latest
docker push registry.example.com/team/my-image:latest
```

El handler admite uploads monolíticos/chunked con `POST`, `PATCH`, `PUT`. El digest soportado es SHA-256. El manifest se rechaza si un blob referenciado no existe.

## Pull

```bash
docker pull registry.example.com/team/my-image:latest
```

La resolución acepta tag o digest. Blobs y manifests responden a `GET` y `HEAD`.

## Compatibilidad y límites verificados

| Área | Estado |
| --- | --- |
| Docker Registry-style `/v2/` | Implementado en las rutas listadas |
| JSON manifests | Implementado mediante `manifestEnvelope` |
| SHA-256 digest | Implementado |
| Tags y catálogo | Implementado |
| Upload chunking | Implementado con `PATCH` |
| Upload cancellation | No implementado; devuelve `UNSUPPORTED` |
| Deletes de manifests/blobs | No hay rutas registradas |
| Garbage collection | No hay implementación encontrada |
| Replicación/remote storage | No hay adapter encontrado |
| Multi-tenant | No; resolver single-tenant |
| Multi-arch/indexes | ⚠️ No se ha podido confirmar a partir del código actual |
| Certificación OCI completa | ⚠️ No se ha podido confirmar a partir del código actual |

Nombres de repositorio se validan mediante `internal/domain/regixtry/repository.go`. No hay namespace administrativo separado: `team/my-image` es un nombre de repository.
