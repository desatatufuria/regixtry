#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/registry-auth-smoke}"
PORT="${PORT:-${2:-5500}}"
POSTGRES_PORT="${POSTGRES_PORT:-55432}"
ADDR="127.0.0.1:${PORT}"
REGISTRY_HOST="localhost:${PORT}"
REPOSITORY="registry-auth/smoke"
IMAGE="${REGISTRY_HOST}/${REPOSITORY}:latest"
AUTH_DSN="postgres://registry:registry@127.0.0.1:${POSTGRES_PORT}/registry_auth?sslmode=disable"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-change-me-now}"
POSTGRES_CONTAINER="registry-auth-smoke-postgres-${PORT}"
PID=""
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/registry-auth-serve.${PORT}.XXXXXX.log")"

cleanup() {
  docker logout "${REGISTRY_HOST}" >/dev/null 2>&1 || true
  if [[ -n "${PID}" ]] && kill -0 "${PID}" >/dev/null 2>&1; then
    kill "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" 2>/dev/null || true
  fi
  docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  rm -f "${LOG_FILE}" >/dev/null 2>&1 || true
}

trap cleanup EXIT

mkdir -p "${ROOT_DIR}"

docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
docker run -d \
  --name "${POSTGRES_CONTAINER}" \
  -e POSTGRES_DB=registry_auth \
  -e POSTGRES_USER=registry \
  -e POSTGRES_PASSWORD=registry \
  -p "${POSTGRES_PORT}:5432" \
  postgres:17-alpine >/dev/null

for _ in $(seq 1 30); do
  if docker exec "${POSTGRES_CONTAINER}" pg_isready -U registry -d registry_auth >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

docker exec "${POSTGRES_CONTAINER}" pg_isready -U registry -d registry_auth >/dev/null 2>&1

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry bootstrap-admin \
  -auth-postgres-dsn "${AUTH_DSN}" \
  -username "${ADMIN_USERNAME}" \
  -password "${ADMIN_PASSWORD}"

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry serve \
  -addr "${ADDR}" \
  -storage-root "${ROOT_DIR}" \
  -auth-postgres-dsn "${AUTH_DSN}" \
  -auth-token-realm "http://${ADDR}/auth/token" \
  >"${LOG_FILE}" 2>&1 &
PID="$!"

for _ in $(seq 1 30); do
  if curl -fsSI "http://${ADDR}/v2/" >/dev/null 2>&1; then
    break
  fi
  if curl -si "http://${ADDR}/v2/" | grep -q "401 Unauthorized"; then
    break
  fi
  sleep 1
done

docker pull alpine:3.20
docker login "${REGISTRY_HOST}" -u "${ADMIN_USERNAME}" -p "${ADMIN_PASSWORD}"
docker tag alpine:3.20 "${IMAGE}"
docker push "${IMAGE}"
docker image rm -f "${IMAGE}" >/dev/null 2>&1 || true
docker pull "${IMAGE}"

catalog_response="$(curl -si "http://${ADDR}/v2/_catalog")"
tags_response="$(curl -si "http://${ADDR}/v2/${REPOSITORY}/tags/list")"

grep -q "401 Unauthorized" <<<"${catalog_response}"
grep -q "WWW-Authenticate: Bearer" <<<"${catalog_response}"
grep -q "401 Unauthorized" <<<"${tags_response}"
grep -q "repository:${REPOSITORY}:pull" <<<"${tags_response}"

echo "Authenticated Docker login/push/pull smoke completed for ${IMAGE}. Anonymous catalog/tag access was rejected as expected."
