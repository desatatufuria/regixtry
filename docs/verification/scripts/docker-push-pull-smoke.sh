#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/registry-foundation-smoke}"
ADDR="127.0.0.1:5500"
IMAGE="localhost:5500/registry-foundation/smoke:latest"
PID=""

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" >/dev/null 2>&1; then
    kill "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" 2>/dev/null || true
  fi
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
  >/tmp/registry-foundation-serve.log 2>&1 &
PID="$!"

sleep 2

docker pull alpine:3.20
docker tag alpine:3.20 "${IMAGE}"
docker push "${IMAGE}"
docker image rm -f "${IMAGE}" >/dev/null 2>&1 || true
docker pull "${IMAGE}"

echo "Docker push/pull smoke completed for ${IMAGE}."
