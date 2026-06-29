#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/registry-foundation-smoke}"
PORT="${PORT:-${2:-5500}}"
ADDR="127.0.0.1:${PORT}"
IMAGE="localhost:${PORT}/registry-foundation/smoke:latest"
PID=""
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/registry-foundation-serve.${PORT}.XXXXXX.log")"

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" >/dev/null 2>&1; then
    kill "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" 2>/dev/null || true
  fi

  rm -f "${LOG_FILE}" >/dev/null 2>&1 || true
}

trap cleanup EXIT

mkdir -p "${ROOT_DIR}"

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry serve \
  -addr "${ADDR}" \
  -storage-root "${ROOT_DIR}" \
  -allow-anonymous-pull \
  -allow-anonymous-push \
  >"${LOG_FILE}" 2>&1 &
PID="$!"

sleep 2

docker pull alpine:3.20
docker tag alpine:3.20 "${IMAGE}"
docker push "${IMAGE}"
docker image rm -f "${IMAGE}" >/dev/null 2>&1 || true
docker pull "${IMAGE}"

echo "Docker push/pull smoke completed for ${IMAGE}."
