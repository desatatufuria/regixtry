# Getting Started

## Local run without auth

```bash
go run ./cmd/regixtry serve \
  -addr 127.0.0.1:5000 \
  -public-url http://127.0.0.1:5000 \
  -storage-root ./data
```

`serve` creates `./data/content` for blobs and `./data/metadata.db` for metadata when `-db` is not given.

## Push and pull

```bash
docker tag my-image:latest 127.0.0.1:5000/example/my-image:latest
docker push 127.0.0.1:5000/example/my-image:latest
docker pull 127.0.0.1:5000/example/my-image:latest
```

The Docker daemon must accept the local HTTP endpoint if TLS is not configured. Configuring that trust is the Docker daemon's responsibility, not something Regixtry manages.

## Verification

```bash
curl -i http://127.0.0.1:5000/v2/
curl -i 'http://127.0.0.1:5000/v2/_catalog'
curl -i 'http://127.0.0.1:5000/v2/example/my-image/tags/list'
```

If auth is enabled, `/v2/` normally responds `401` until the Bearer challenge completes. See [authentication.md](authentication.md).

## Local Compose

The repository's Compose setup requires an external network named `dtf-netwok`:

```bash
docker network create dtf-netwok
docker compose up -d postgres
printf '%s\n' '<admin-password>' | docker compose run --rm -T regixtry bootstrap-admin -username admin -password-stdin
docker compose up -d regixtry
printf '%s\n' '<admin-password>' | docker login localhost:${REGISTRY_PORT:-5517} -u admin --password-stdin
```

Compose publishes the registry on `127.0.0.1:${REGISTRY_PORT:-5517}` and PostgreSQL on `15432`.
