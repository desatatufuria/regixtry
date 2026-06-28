#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/registry-foundation-smoke}"
OUTPUT_FILE="${ROOT_DIR}/tui-smoke.txt"

mkdir -p "${ROOT_DIR}"

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/registry tui \
  -storage-root "${ROOT_DIR}" \
  -snapshot \
  >"${OUTPUT_FILE}"

grep -q "Registry Console" "${OUTPUT_FILE}"

echo "TUI smoke output captured at ${OUTPUT_FILE}."
