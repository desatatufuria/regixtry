#!/usr/bin/env bash
set -euo pipefail

: "${REGISTRY:?set REGISTRY}"
: "${REGISTRY_USERNAME:?set REGISTRY_USERNAME}"
: "${REGISTRY_PASSWORD:?set REGISTRY_PASSWORD}"
: "${IMAGE:?set IMAGE}"
: "${TAG:?set TAG}"

printf '%s\n' "$REGISTRY_PASSWORD" | docker login "$REGISTRY" -u "$REGISTRY_USERNAME" --password-stdin
