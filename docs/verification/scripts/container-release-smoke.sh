#!/usr/bin/env bash
#
# container-release-smoke.sh — verifies a published (or locally built)
# regixtry container image actually behaves like the release image: runs
# non-root, becomes healthy, and persists blob data across a restart on the
# same named volume. Sibling of install-release-smoke.sh; same fail()/trap
# conventions.
#
# Usage:
#   container-release-smoke.sh --image <ref> [--expect-multiarch]
#
# `--auth-postgres` (Postgres-backed auth scenario) is added by a later
# revision of this script; it is not implemented here.

set -euo pipefail

IMAGE=""
EXPECT_MULTIARCH=0
RUN_ID="$$-$(date +%s)"

# CURL_IMAGE runs every HTTP probe joined to the target container's network
# namespace (`docker run --network container:<name>`) instead of publishing
# a host port and curling localhost. This keeps the script correct in any
# Docker environment — including CI runners and network-restricted sandboxes
# where host-to-container port forwarding is unavailable — because it never
# depends on host<->container routing, only on the daemon's ability to share
# a network namespace between two containers it manages directly.
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"

ANON_VOLUME=""
ANON_CONTAINER=""
ANON_CONTAINER2=""

fail() {
  printf 'container-release-smoke: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: container-release-smoke.sh --image <ref> [--expect-multiarch]
EOF
}

cleanup() {
  local container=""

  for container in "${ANON_CONTAINER2}" "${ANON_CONTAINER}"; do
    if [[ -n "${container}" ]]; then
      docker rm -f "${container}" >/dev/null 2>&1 || true
    fi
  done

  if [[ -n "${ANON_VOLUME}" ]]; then
    docker volume rm "${ANON_VOLUME}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

# wait_healthy polls `docker inspect`'s Health.Status until it reports
# "healthy" or the timeout elapses. An explicit "unhealthy" status fails
# immediately rather than waiting out the full timeout, since the
# HEALTHCHECK's own retry budget already gave it a chance to recover.
wait_healthy() {
  local container="$1"
  local timeout="${2:-60}"
  local elapsed=0
  local status=""

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    status="$(docker inspect --format '{{.State.Health.Status}}' "${container}" 2>/dev/null || echo "")"

    if [[ "${status}" == "healthy" ]]; then
      return 0
    fi

    if [[ "${status}" == "unhealthy" ]]; then
      fail "${container} reported unhealthy before becoming healthy"
    fi

    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "${container} did not become healthy within ${timeout}s (last status: '${status}')"
}

# http_status issues one HTTP request against ${container}'s own loopback
# (127.0.0.1:5000, exactly as the process inside it sees itself) via a
# short-lived curl sidecar sharing that container's network namespace, and
# prints only the numeric status code.
#
# HEAD is special-cased to curl's `--head` flag rather than `-X HEAD`: this
# server sets a real Content-Length on HEAD responses (matching what the
# equivalent GET would send) and writes no body, which is correct per RFC
# 7231. `-X HEAD` alone leaves curl's internal state machine still expecting
# a GET-shaped body, so it hangs forever waiting for bytes that a compliant
# server will never send. `--head` tells curl up front that no body is
# coming. Reproduced and confirmed while writing this script: the `-X HEAD`
# form hung indefinitely against this exact server before this fix.
http_status() {
  local container="$1"
  local method="$2"
  local url="$3"
  shift 3

  if [[ "${method}" == "HEAD" ]]; then
    docker run --rm --network "container:${container}" "${CURL_IMAGE}" \
      -s -o /dev/null -w '%{http_code}' --max-time 20 --head "$@" "${url}"
  else
    docker run --rm --network "container:${container}" "${CURL_IMAGE}" \
      -s -o /dev/null -w '%{http_code}' --max-time 20 -X "${method}" "$@" "${url}"
  fi
}

assert_non_root() {
  local container="$1"
  local config_user=""
  local uid=""

  config_user="$(docker inspect --format '{{.Config.User}}' "${container}")"
  [[ "${config_user}" == "65532:65532" ]] || fail "expected Config.User 65532:65532 for ${container}, got '${config_user}'"

  uid="$(docker exec "${container}" id -u)"
  [[ "${uid}" == "65532" ]] || fail "expected in-container uid 65532 for ${container}, got '${uid}'"
}

# check_multiarch only makes sense against a real pushed manifest list, so it
# skips with a clear stderr message rather than failing when buildx is
# unavailable or the reference is a local single-arch build.
check_multiarch() {
  local image="$1"
  local output=""

  if ! docker buildx version >/dev/null 2>&1; then
    printf 'container-release-smoke: skipping --expect-multiarch check (buildx plugin unavailable)\n' >&2
    return 0
  fi

  if ! output="$(docker buildx imagetools inspect "${image}" 2>&1)"; then
    printf 'container-release-smoke: skipping --expect-multiarch check (imagetools inspect failed for %s, likely a local single-arch build rather than a pushed multi-arch manifest)\n' "${image}" >&2
    return 0
  fi

  printf '%s\n' "${output}" | grep -q 'linux/amd64' || fail "expected linux/amd64 in multi-arch manifest for ${image}"
  printf '%s\n' "${output}" | grep -q 'linux/arm64' || fail "expected linux/arm64 in multi-arch manifest for ${image}"
}

# run_anonymous_scenario: run detached on a named volume with anonymous
# push/pull enabled; assert non-root; poll to healthy; upload a small blob
# over /v2/; destroy the container; start a fresh one on the SAME volume;
# HEAD the blob digest to prove the data survived the restart.
run_anonymous_scenario() {
  local repo="smoke/anon"
  local blob_content="regixtry-anon-smoke-${RUN_ID}"
  local blob_digest=""
  local headers=""
  local location=""
  local status=""

  blob_digest="sha256:$(printf '%s' "${blob_content}" | sha256sum | cut -d' ' -f1)"

  ANON_VOLUME="regixtry-smoke-anon-vol-${RUN_ID}"
  ANON_CONTAINER="regixtry-smoke-anon-${RUN_ID}"

  docker volume create "${ANON_VOLUME}" >/dev/null

  docker run -d --name "${ANON_CONTAINER}" \
    -v "${ANON_VOLUME}:/var/lib/regixtry" \
    "${IMAGE}" \
    serve -addr 0.0.0.0:5000 -storage-root /var/lib/regixtry \
      -public-url "http://127.0.0.1:5000" \
      -allow-anonymous-pull -allow-anonymous-push >/dev/null

  assert_non_root "${ANON_CONTAINER}"
  wait_healthy "${ANON_CONTAINER}" 60

  headers="$(docker run --rm --network "container:${ANON_CONTAINER}" "${CURL_IMAGE}" \
    -sS -D - -o /dev/null --max-time 20 -X POST "http://127.0.0.1:5000/v2/${repo}/blobs/uploads/")"
  location="$(printf '%s' "${headers}" | tr -d '\r' | awk -F': ' 'tolower($1) == "location" {print $2; exit}')"
  [[ -n "${location}" ]] || fail "blob upload start did not return a Location header"

  status="$(docker run --rm --network "container:${ANON_CONTAINER}" "${CURL_IMAGE}" \
    -s -o /dev/null -w '%{http_code}' --max-time 20 -X PUT --data-raw "${blob_content}" \
    "http://127.0.0.1:5000${location}?digest=${blob_digest}")"
  [[ "${status}" == "201" ]] || fail "blob upload PUT returned ${status}, expected 201"

  status="$(http_status "${ANON_CONTAINER}" HEAD "http://127.0.0.1:5000/v2/${repo}/blobs/${blob_digest}")"
  [[ "${status}" == "200" ]] || fail "blob HEAD immediately after upload returned ${status}, expected 200"

  docker rm -f "${ANON_CONTAINER}" >/dev/null 2>&1 || true
  ANON_CONTAINER=""

  ANON_CONTAINER2="regixtry-smoke-anon-restart-${RUN_ID}"
  docker run -d --name "${ANON_CONTAINER2}" \
    -v "${ANON_VOLUME}:/var/lib/regixtry" \
    "${IMAGE}" \
    serve -addr 0.0.0.0:5000 -storage-root /var/lib/regixtry \
      -public-url "http://127.0.0.1:5000" \
      -allow-anonymous-pull -allow-anonymous-push >/dev/null

  wait_healthy "${ANON_CONTAINER2}" 60

  status="$(http_status "${ANON_CONTAINER2}" HEAD "http://127.0.0.1:5000/v2/${repo}/blobs/${blob_digest}")"
  [[ "${status}" == "200" ]] || fail "blob HEAD after restart on the same volume returned ${status}, expected 200 (persistence proof)"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --image)
        [[ $# -ge 2 ]] || fail "--image requires a value"
        IMAGE="$2"
        shift 2
        ;;
      --expect-multiarch)
        EXPECT_MULTIARCH=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        usage >&2
        fail "unknown argument: $1"
        ;;
    esac
  done

  [[ -n "${IMAGE}" ]] || { usage >&2; fail "--image is required"; }
}

main() {
  parse_args "$@"

  if [[ "${EXPECT_MULTIARCH}" -eq 1 ]]; then
    check_multiarch "${IMAGE}"
  fi

  run_anonymous_scenario

  printf 'container-release-smoke: anonymous scenario passed for %s\n' "${IMAGE}"
}

main "$@"
