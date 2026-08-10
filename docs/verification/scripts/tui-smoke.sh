#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${1:-/tmp/tui-feature-manager-smoke}"
OUTPUT_FILE="${ROOT_DIR}/tui-smoke.txt"

mkdir -p "${ROOT_DIR}"

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go run ./cmd/regixtry tui \
  -storage-root "${ROOT_DIR}" \
  -snapshot \
  >"${OUTPUT_FILE}"

grep -q "Regixtry Console" "${OUTPUT_FILE}"

GOMODCACHE="${GOMODCACHE:-/tmp/opencode/gomodcache}" \
GOPATH="${GOPATH:-/tmp/opencode/gopath}" \
GOSUMDB="${GOSUMDB:-off}" \
go test ./internal/tui -run 'TestModelFeatureViewRendersGenericPageAndAllowsDeclaredAction|TestModelFeatureViewKeepsMinimalPagesUsable|TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions'

echo "TUI smoke verified snapshot launch and feature-manager action/help coverage. Snapshot saved to ${OUTPUT_FILE}."
