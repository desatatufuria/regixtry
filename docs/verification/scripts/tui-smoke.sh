#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/registry-auth-smoke}"
OUTPUT_FILE="${ROOT_DIR}/tui-smoke.txt"
POSTGRES_PORT="${POSTGRES_PORT:-55433}"
AUTH_DSN="${AUTH_DSN:-postgres://registry:registry@127.0.0.1:${POSTGRES_PORT}/registry_auth?sslmode=disable}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-change-me-now}"
POSTGRES_CONTAINER="registry-tui-smoke-postgres-${POSTGRES_PORT}"
STARTED_POSTGRES=0

cleanup() {
  if [[ "${STARTED_POSTGRES}" == "1" ]]; then
    docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

mkdir -p "${ROOT_DIR}"

if [[ -z "${AUTH_DSN:-}" || "${AUTH_DSN}" == postgres://registry:registry@127.0.0.1:${POSTGRES_PORT}/registry_auth?sslmode=disable ]]; then
  docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  docker run -d \
    --name "${POSTGRES_CONTAINER}" \
    -e POSTGRES_DB=registry_auth \
    -e POSTGRES_USER=registry \
    -e POSTGRES_PASSWORD=registry \
    -p "${POSTGRES_PORT}:5432" \
    postgres:17-alpine >/dev/null
  STARTED_POSTGRES=1

  for _ in $(seq 1 30); do
    if docker exec "${POSTGRES_CONTAINER}" pg_isready -U registry -d registry_auth >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  docker exec "${POSTGRES_CONTAINER}" pg_isready -U registry -d registry_auth >/dev/null 2>&1
fi

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry bootstrap-admin \
  -auth-postgres-dsn "${AUTH_DSN}" \
  -username "${ADMIN_USERNAME}" \
  -password "${ADMIN_PASSWORD}" >/dev/null

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry tui \
  -storage-root "${ROOT_DIR}" \
  -auth-postgres-dsn "${AUTH_DSN}" \
  -snapshot \
  >"${OUTPUT_FILE}"

grep -q "Registry Console" "${OUTPUT_FILE}"
grep -q "registry-auth/smoke" "${OUTPUT_FILE}"
grep -q "Auth-backed admin actions are disabled in the local TUI until a real operator login flow exists." "${OUTPUT_FILE}"

echo "Auth-enabled TUI smoke output captured at ${OUTPUT_FILE}."
