# Inicio rápido

## Ejecución local sin auth

```bash
go run ./cmd/regixtry serve \
  -addr 127.0.0.1:5000 \
  -public-url http://127.0.0.1:5000 \
  -storage-root ./data
```

`serve` crea `./data/content` para blobs y `./data/metadata.db` para metadata si no se indica `-db`.

## Push y pull

```bash
docker tag my-image:latest 127.0.0.1:5000/example/my-image:latest
docker push 127.0.0.1:5000/example/my-image:latest
docker pull 127.0.0.1:5000/example/my-image:latest
```

El daemon Docker debe aceptar el endpoint HTTP local si no se configura TLS. La configuración de esa confianza depende del daemon Docker y no es gestionada por Regixtry.

## Verificación

```bash
curl -i http://127.0.0.1:5000/v2/
curl -i 'http://127.0.0.1:5000/v2/_catalog'
curl -i 'http://127.0.0.1:5000/v2/example/my-image/tags/list'
```

Si se habilita auth, `/v2/` normalmente responde `401` hasta completar el challenge Bearer. Ver [authentication.md](authentication.md).

## Compose local

El Compose del repositorio requiere una red externa llamada `dtf-netwok`:

```bash
docker network create dtf-netwok
docker compose up -d postgres
printf '%s\n' '<admin-password>' | docker compose run --rm -T regixtry bootstrap-admin -username admin -password-stdin
docker compose up -d regixtry
printf '%s\n' '<admin-password>' | docker login localhost:${REGISTRY_PORT:-5517} -u admin --password-stdin
```

Compose publica el registry en `127.0.0.1:${REGISTRY_PORT:-5517}` y PostgreSQL en `15432`.
