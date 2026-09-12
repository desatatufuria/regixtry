#!/usr/bin/env bash
#
# container-release-smoke.sh — verifies a published (or locally built)
# regixtry container image actually behaves like the release image: runs
# non-root, becomes healthy, and persists blob data across a restart on the
# same named volume. Sibling of install-release-smoke.sh; same fail()/trap
# conventions.
#
# Usage:
#   container-release-smoke.sh --image <ref> [--expect-multiarch] [--auth-postgres]
#
# `--auth-postgres` runs a second scenario: a sibling `postgres:17-alpine`
# container on an ephemeral network, `bootstrap-admin -password-stdin`
# against it, then a `serve` container started with that DSN, proving auth
# actually gates `/v2/` (anonymous rejected, authenticated Bearer token
# grants access) rather than only that the flag is accepted.

set -euo pipefail

IMAGE=""
EXPECT_MULTIARCH=0
AUTH_POSTGRES=0
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

# AUTH_* track the --auth-postgres scenario's resources so cleanup() can tear
# them down in the load-bearing order design.md requires: containers first,
# then volumes, then the network last (docker network rm fails while any
# container endpoint is still attached to it).
AUTH_NETWORK=""
AUTH_PG_CONTAINER=""
AUTH_CONTAINER=""
AUTH_VOLUME=""

fail() {
  printf 'container-release-smoke: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: container-release-smoke.sh --image <ref> [--expect-multiarch] [--auth-postgres]
EOF
}

cleanup() {
  local container=""
  local volume=""

  for container in "${ANON_CONTAINER2}" "${ANON_CONTAINER}" "${AUTH_CONTAINER}" "${AUTH_PG_CONTAINER}"; do
    if [[ -n "${container}" ]]; then
      docker rm -f "${container}" >/dev/null 2>&1 || true
    fi
  done

  for volume in "${ANON_VOLUME}" "${AUTH_VOLUME}"; do
    if [[ -n "${volume}" ]]; then
      docker volume rm "${volume}" >/dev/null 2>&1 || true
    fi
  done

  # Network removal is last and only possible once every attached container
  # endpoint above has already been removed.
  if [[ -n "${AUTH_NETWORK}" ]]; then
    docker network rm "${AUTH_NETWORK}" >/dev/null 2>&1 || true
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

# http_headers issues one HTTP request the same way http_status does, but
# prints the raw response headers instead of just the status code. Shares
# http_status's HEAD special-case (curl's `--head`, not `-X HEAD`) for the
# same reason: this server sets a real Content-Length on HEAD responses and
# `-X HEAD` alone leaves curl waiting for a body that never arrives.
http_headers() {
  local container="$1"
  local method="$2"
  local url="$3"
  shift 3

  if [[ "${method}" == "HEAD" ]]; then
    docker run --rm --network "container:${container}" "${CURL_IMAGE}" \
      -sS -D - -o /dev/null --max-time 20 --head "$@" "${url}"
  else
    docker run --rm --network "container:${container}" "${CURL_IMAGE}" \
      -sS -D - -o /dev/null --max-time 20 -X "${method}" "$@" "${url}"
  fi
}

# wait_postgres_ready polls `docker logs` for the postgres image's own
# readiness markers, mirroring
# internal/infra/install/compose/database.go's waitPostgresReady exactly
# (design.md "Postgres readiness" decision). Deliberately not `pg_isready`:
# on a FRESH volume, the official postgres image starts a temporary,
# Unix-socket-only server to run initdb, shuts it down, then starts the
# final server that actually accepts networked, password-authenticated
# connections. `docker exec ... pg_isready` (no -h) defaults to that local
# Unix socket and can report success against the temporary server, well
# before the final one is listening on the network -- confirmed live in this
# script during v0.2.1-rc9: bootstrap-admin got "connection refused" against
# the sibling container's TCP port immediately after pg_isready reported
# ready. The postgres image's own log output is the reliable signal instead.
wait_postgres_ready() {
  local container="$1"
  local timeout="${2:-60}"
  local elapsed=0

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    if postgres_log_indicates_ready "$(docker logs "${container}" 2>&1 || true)"; then
      return 0
    fi

    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "${container} logs never showed the final server accepting connections within ${timeout}s"
}

# postgres_log_indicates_ready mirrors database.go's postgresLogIndicatesReady
# exactly: "database system is ready to accept connections" must appear a
# second time when "PostgreSQL init process complete; ready for start up."
# shows the temp-initdb-to-final handoff is in progress (fresh volume); on an
# existing volume there is no handoff, so the marker's first appearance
# already is the final server.
postgres_log_indicates_ready() {
  local logs="$1"
  local ready_marker="database system is ready to accept connections"
  local reinit_marker="PostgreSQL init process complete; ready for start up."
  local ready_count=0

  ready_count="$(grep -Fc "${ready_marker}" <<<"${logs}" || true)"
  [[ "${ready_count}" -gt 0 ]] || return 1

  if grep -Fq "${reinit_marker}" <<<"${logs}"; then
    [[ "${ready_count}" -ge 2 ]]
  else
    return 0
  fi
}

# run_bootstrap_admin_with_retry retries the real bootstrap-admin connection
# attempt a bounded number of times, short sleep between -- see the call
# site's comment for why a readiness probe run beforehand (wait_postgres_ready)
# cannot close this gap to zero by itself. Fails loudly, with the last
# attempt's own error output, rather than looping forever.
run_bootstrap_admin_with_retry() {
  local admin_password="$1"
  local dsn="$2"
  local pg_container="$3"
  local max_attempts=5
  local attempt=1

  while [[ "${attempt}" -le "${max_attempts}" ]]; do
    if printf '%s\n' "${admin_password}" | docker run --rm -i --network "${AUTH_NETWORK}" \
      "${IMAGE}" bootstrap-admin -auth-postgres-dsn "${dsn}" -username admin -password-stdin; then
      return 0
    fi

    if [[ "${attempt}" -eq "${max_attempts}" ]]; then
      fail "bootstrap-admin failed against ${pg_container} after ${max_attempts} attempts (auth scenario cannot continue)"
    fi

    printf 'container-release-smoke: bootstrap-admin attempt %d/%d failed, retrying in 2s...\n' "${attempt}" "${max_attempts}" >&2
    sleep 2
    attempt=$((attempt + 1))
  done
}

# registry_token exchanges Basic credentials for a Bearer access token via
# GET /auth/token?scope=repository:<repo>:pull,push, per design.md's auth
# smoke sequence. Basic credentials on /v2/ itself are silently ignored
# (authenticate() only recognizes Bearer), so this exchange is mandatory,
# not a shortcut. Prints only the extracted token string on success.
registry_token() {
  local container="$1"
  local username="$2"
  local password="$3"
  local repo="$4"
  local response=""
  local token=""

  response="$(docker run --rm --network "container:${container}" "${CURL_IMAGE}" \
    -sS --max-time 20 -u "${username}:${password}" \
    "http://127.0.0.1:5000/auth/token?service=regixtry&scope=repository:${repo}:pull,push")"

  token="$(printf '%s' "${response}" | grep -o '"token"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed -E 's/.*"([^"]*)"$/\1/')"
  [[ -n "${token}" ]] || fail "registry_token: could not parse a token from the /auth/token response: ${response}"

  printf '%s' "${token}"
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

# run_auth_scenario: ephemeral network + sibling postgres:17-alpine +
# bootstrap-admin (must succeed BEFORE serve starts, since serve fails fast
# with no existing admin) + serve with -auth-postgres-dsn. Asserts anonymous
# rejection, then an authenticated token exchange followed by a successful
# blob PUT/HEAD, then confirms the same HEAD is rejected without the token.
run_auth_scenario() {
  local repo="smoke/auth"
  local admin_password="regixtry-smoke-admin-${RUN_ID}"
  local blob_content="regixtry-auth-smoke-${RUN_ID}"
  local blob_digest=""
  local dsn=""
  local token=""
  local headers=""
  local location=""
  local status=""

  blob_digest="sha256:$(printf '%s' "${blob_content}" | sha256sum | cut -d' ' -f1)"

  AUTH_NETWORK="regixtry-smoke-auth-net-${RUN_ID}"
  AUTH_PG_CONTAINER="regixtry-smoke-pg-${RUN_ID}"
  AUTH_CONTAINER="regixtry-smoke-auth-${RUN_ID}"
  AUTH_VOLUME="regixtry-smoke-auth-vol-${RUN_ID}"

  docker network create "${AUTH_NETWORK}" >/dev/null

  docker run -d --name "${AUTH_PG_CONTAINER}" --network "${AUTH_NETWORK}" \
    -e POSTGRES_DB=regixtry_auth -e POSTGRES_USER=registry -e POSTGRES_PASSWORD=registry \
    postgres:17-alpine >/dev/null

  wait_postgres_ready "${AUTH_PG_CONTAINER}" 60

  dsn="postgres://registry:registry@${AUTH_PG_CONTAINER}:5432/regixtry_auth?sslmode=disable"

  # bootstrap-admin MUST complete before the serve container starts: serve
  # fails fast when auth is enabled and no admin exists yet, so there would
  # be no running container to exec into afterward.
  #
  # Retried, not a single shot: wait_postgres_ready confirms the postgres
  # image's OWN log output already shows the final server accepting
  # connections (see that function's doc comment), but bootstrap-admin here
  # runs in a brand-new, separate `docker run` container -- its very first
  # TCP dial can still race a residual, sub-second window between "postgres
  # logged ready" and "a different container's connection actually reaches
  # accept()" (confirmed live on v0.2.1-rc14: a single "connection refused"
  # immediately after wait_postgres_ready returned true, no timeout, no
  # further retries at the time). No readiness probe run BEFORE the real
  # connection attempt can close that residual gap to zero -- the fix is a
  # short bounded retry on the connection attempt itself, the standard
  # pattern for exactly this class of distributed-startup race.
  run_bootstrap_admin_with_retry "${admin_password}" "${dsn}" "${AUTH_PG_CONTAINER}"

  docker volume create "${AUTH_VOLUME}" >/dev/null

  docker run -d --name "${AUTH_CONTAINER}" --network "${AUTH_NETWORK}" \
    -v "${AUTH_VOLUME}:/var/lib/regixtry" \
    -e REGISTRY_AUTH_POSTGRES_DSN="${dsn}" \
    "${IMAGE}" >/dev/null

  assert_non_root "${AUTH_CONTAINER}"
  wait_healthy "${AUTH_CONTAINER}" 60

  # Auth rejects: anonymous GET /v2/ MUST be 401 and carry WWW-Authenticate.
  # This is the expected, correct outcome — distinct from liveness above,
  # which goes healthy via 401 and proves nothing about auth by itself.
  status="$(http_status "${AUTH_CONTAINER}" GET "http://127.0.0.1:5000/v2/")"
  [[ "${status}" == "401" ]] || fail "anonymous GET /v2/ returned ${status}, expected 401"

  headers="$(http_headers "${AUTH_CONTAINER}" GET "http://127.0.0.1:5000/v2/")"
  printf '%s' "${headers}" | tr -d '\r' | grep -qi '^www-authenticate:' \
    || fail "anonymous GET /v2/ response missing WWW-Authenticate header"

  # Auth grants: Basic credentials exchanged for a Bearer token, then that
  # token authorizes a real blob PUT/HEAD round trip.
  token="$(registry_token "${AUTH_CONTAINER}" admin "${admin_password}" "${repo}")"

  headers="$(docker run --rm --network "container:${AUTH_CONTAINER}" "${CURL_IMAGE}" \
    -sS -D - -o /dev/null --max-time 20 -X POST -H "Authorization: Bearer ${token}" \
    "http://127.0.0.1:5000/v2/${repo}/blobs/uploads/")"
  location="$(printf '%s' "${headers}" | tr -d '\r' | awk -F': ' 'tolower($1) == "location" {print $2; exit}')"
  [[ -n "${location}" ]] || fail "authenticated blob upload start did not return a Location header"

  status="$(docker run --rm --network "container:${AUTH_CONTAINER}" "${CURL_IMAGE}" \
    -s -o /dev/null -w '%{http_code}' --max-time 20 -X PUT -H "Authorization: Bearer ${token}" \
    --data-raw "${blob_content}" "http://127.0.0.1:5000${location}?digest=${blob_digest}")"
  [[ "${status}" == "201" ]] || fail "authenticated blob upload PUT returned ${status}, expected 201"

  status="$(docker run --rm --network "container:${AUTH_CONTAINER}" "${CURL_IMAGE}" \
    -s -o /dev/null -w '%{http_code}' --max-time 20 --head -H "Authorization: Bearer ${token}" \
    "http://127.0.0.1:5000/v2/${repo}/blobs/${blob_digest}")"
  [[ "${status}" == "200" ]] || fail "authenticated blob HEAD returned ${status}, expected 200"

  # The same HEAD without the Bearer token MUST be rejected — proves the
  # 200 above came from the token, not from the endpoint being open.
  status="$(http_status "${AUTH_CONTAINER}" HEAD "http://127.0.0.1:5000/v2/${repo}/blobs/${blob_digest}")"
  [[ "${status}" == "401" ]] || fail "unauthenticated blob HEAD returned ${status}, expected 401"
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
      --auth-postgres)
        AUTH_POSTGRES=1
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

  if [[ "${AUTH_POSTGRES}" -eq 1 ]]; then
    run_auth_scenario
    printf 'container-release-smoke: auth scenario passed for %s\n' "${IMAGE}"
  fi
}

main "$@"
