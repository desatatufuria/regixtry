#!/usr/bin/env bash
#
# setup-docker-smoke.sh -- proves `regixtry setup --mode docker` actually
# produces a healthy, reachable, auth-enabled Docker Compose stack from a
# clean checkout, for BOTH Postgres scenarios tasks.md Phase 11 requires:
# bundled (the default, no DSN supplied) and external (--external-postgres,
# a standalone postgres:17-alpine instance supplies the DSN).
#
# Sibling of install-release-smoke.sh (binary-only/daemon-sqlite lifecycle)
# and container-release-smoke.sh (hand-run container behavior); this script
# reuses their fail()/cleanup()-trap/bounded-poll conventions but drives the
# real `internal/infra/install/compose` orchestration through the actual
# `regixtry setup --mode docker` CLI entry point, not hand-assembled `docker
# run`/`docker compose` commands -- the whole point is proving the Go
# orchestration itself, end to end, against a real Docker daemon.
#
# NOT WIRED INTO CI. This script needs a real Docker Engine + Compose v2
# plugin and materially longer runtime than the rest of this change's
# fake-exec unit tests. Per design.md's Risk table ("Smoke script needs a
# real daemon, so CI cost grows... Not wired into release.yml in this
# change"), it is deliberately absent from .github/workflows/release.yml.
# Run it locally or on demand -- tasks.md Phase 12 names this the sole
# verification gate for `docker` setup mode until a future change adds CI
# wiring. Do not assume CI coverage exists for this path.
#
# Image note: this branch's repository-root Dockerfile is still the
# original single-stage build (no `dev`/`release` build targets, no
# HEALTHCHECK) -- the multi-stage, non-root, healthchecked rewrite lives on
# the separate, not-yet-merged `registry-container-mode` change. So this
# script does not offer a `--build-local` option; it defaults to the real
# published `ghcr.io/desatatufuria/regixtry` image tag pinned below
# (verified pullable during this change's own review session), and accepts
# --image-tag to point at a different published tag once one exists.
#
# Usage:
#   setup-docker-smoke.sh [--external-postgres] [--image-tag <tag>] [--keep] [root-dir]
#
# Requires: a real Docker Engine + Compose v2 plugin, reachable either
# directly or via passwordless sudo (this script always uses sudo, matching
# install.sh's own documented `sudo ${binary} setup --mode docker ...`
# usage -- `docker`-socket access and /etc-style state paths both assume
# root in real deployments); a Go toolchain to build the binary under test;
# network access to pull the pinned image tag; python3 (free-port probe,
# matching install-release-smoke.sh's own reserve_free_port).

set -euo pipefail

EXTERNAL_POSTGRES=0
IMAGE_TAG="${REGIXTRY_SMOKE_IMAGE_TAG:-v0.2.1-rc2}"
KEEP_ROOT="${KEEP_ROOT:-0}"
ROOT_DIR=""
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BINARY=""
RUN_ID="$$-$(date +%s)"
SUDO=(sudo)

BUNDLED_PROJECT=""
BUNDLED_PROJECT_DIR=""
EXTERNAL_PROJECT=""
EXTERNAL_PROJECT_DIR=""
EXTERNAL_NETWORK=""
EXTERNAL_PG_CONTAINER=""

fail() {
  printf 'setup-docker-smoke: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: setup-docker-smoke.sh [--external-postgres] [--image-tag <tag>] [--keep] [root-dir]
EOF
}

# compose_down mirrors the compose package's own Down(): tear the project's
# stack down (including volumes) and remove the generated project
# directory, best-effort, matching the exact command shape
# internal/infra/install/compose/down.go uses.
compose_down() {
  local project_dir="$1"
  local project_name="$2"
  local compose_file="${project_dir}/docker-compose.yml"
  local env_file="${project_dir}/regixtry.env"

  if [[ -n "${project_name}" && -f "${compose_file}" && -f "${env_file}" ]]; then
    "${SUDO[@]}" docker compose --project-name "${project_name}" --file "${compose_file}" --env-file "${env_file}" down --volumes >/dev/null 2>&1 || true
  fi
  if [[ -n "${project_dir}" ]]; then
    "${SUDO[@]}" rm -rf "${project_dir}" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  if [[ "${KEEP_ROOT}" == "1" ]]; then
    printf 'setup-docker-smoke: --keep set, leaving containers/network/root dir in place (root=%s)\n' "${ROOT_DIR}" >&2
    return 0
  fi

  compose_down "${BUNDLED_PROJECT_DIR}" "${BUNDLED_PROJECT}"
  compose_down "${EXTERNAL_PROJECT_DIR}" "${EXTERNAL_PROJECT}"

  if [[ -n "${EXTERNAL_PG_CONTAINER}" ]]; then
    "${SUDO[@]}" docker rm -f "${EXTERNAL_PG_CONTAINER}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${EXTERNAL_NETWORK}" ]]; then
    "${SUDO[@]}" docker network rm "${EXTERNAL_NETWORK}" >/dev/null 2>&1 || true
  fi

  if [[ -n "${ROOT_DIR}" ]]; then
    "${SUDO[@]}" rm -rf "${ROOT_DIR}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

# reserve_free_port mirrors install-release-smoke.sh's own helper -- same
# bind-then-release trick, so scenarios never collide with each other or
# with anything else already listening on the host.
reserve_free_port() {
  python3 - <<'PY'
import socket
sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
}

assert_contains() {
  local file="$1" expected="$2"
  "${SUDO[@]}" grep -F -- "${expected}" "${file}" >/dev/null || fail "expected '${expected}' in ${file}"
}

assert_not_contains() {
  local file="$1" unexpected="$2"
  if "${SUDO[@]}" grep -F -- "${unexpected}" "${file}" >/dev/null; then
    fail "did not expect '${unexpected}' in ${file}"
  fi
}

# assert_count_one asserts needle appears in file on exactly one line --
# used for the "generated password printed to stdout exactly once"
# assertion (tasks.md 11.2), distinct from assert_contains (>=1).
assert_count_one() {
  local file="$1" needle="$2" actual
  actual="$("${SUDO[@]}" grep -Fc -- "${needle}" "${file}" || true)"
  [[ "${actual}" == "1" ]] || fail "expected '${needle}' to appear on exactly 1 line of ${file}, found ${actual}"
}

assert_mode() {
  local path="$1" expected="$2" actual
  actual="$("${SUDO[@]}" stat -c '%a' "${path}")"
  [[ "${actual}" == "${expected}" ]] || fail "expected mode ${expected} for ${path}, got ${actual}"
}

assert_exists() {
  local path="$1"
  "${SUDO[@]}" test -e "${path}" || fail "expected path to exist: ${path}"
}

# wait_reachable polls GET <url>/v2/ until it returns 200 or 401 (the same
# semantics WaitReachable itself uses -- registry.go), bounded, so this
# script never hangs forever on a stack that setup already reported success
# for but that somehow isn't actually answering.
wait_reachable() {
  local url="$1" timeout="${2:-60}" elapsed=0 status=""

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "${url}/v2/" || echo "")"
    if [[ "${status}" == "200" || "${status}" == "401" ]]; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "${url}/v2/ did not become reachable within ${timeout}s (last status: '${status}')"
}

# wait_postgres_ready mirrors container-release-smoke.sh's helper of the
# same name -- polls pg_isready inside the standalone external Postgres
# container this script itself starts (bundled Postgres readiness is
# StartDatabase's own job, proven by the compose package's own unit tests
# and implicitly by this script's later reachability/auth assertions).
wait_postgres_ready() {
  local container="$1" timeout="${2:-60}" elapsed=0

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    if "${SUDO[@]}" docker exec "${container}" pg_isready -U registry -d regixtry_auth >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "${container} did not report pg_isready within ${timeout}s"
}

# registry_token exchanges Basic credentials for a Bearer token via
# GET /auth/token, mirroring container-release-smoke.sh's registry_token --
# but issued as a direct host curl (this script runs setup on the host
# itself, against a real published host port, so no docker-network-sidecar
# trick is needed the way container-release-smoke.sh needed one for
# hand-run containers without a host port).
registry_token() {
  local base_url="$1" user="$2" pass="$3" repo="$4" response token

  response="$(curl -sS --max-time 20 -u "${user}:${pass}" "${base_url}/auth/token?service=regixtry&scope=repository:${repo}:pull,push")"
  token="$(printf '%s' "${response}" | grep -o '"token"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed -E 's/.*"([^"]*)"$/\1/')"
  [[ -n "${token}" ]] || fail "registry_token: could not parse a token from the /auth/token response: ${response}"
  printf '%s' "${token}"
}

# assert_auth_scenario proves auth actually gates and grants (tasks.md
# 11.4), not merely that the flag was accepted: anonymous GET /v2/ MUST be
# 401 with WWW-Authenticate, an authenticated GET (via the token exchange)
# MUST be 200, and the TUI must be reachable inside the container.
assert_auth_scenario() {
  local base_url="$1" container="$2" user="$3" password="$4"
  local status headers token repo="smoke/setup-docker-${RUN_ID}"

  status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "${base_url}/v2/")"
  [[ "${status}" == "401" ]] || fail "anonymous GET /v2/ returned ${status}, expected 401"

  headers="$(curl -sS -D - -o /dev/null --max-time 10 "${base_url}/v2/")"
  printf '%s' "${headers}" | tr -d '\r' | grep -qi '^www-authenticate:' \
    || fail "anonymous GET /v2/ response missing WWW-Authenticate header"

  token="$(registry_token "${base_url}" "${user}" "${password}" "${repo}")"
  status="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "Authorization: Bearer ${token}" "${base_url}/v2/")"
  [[ "${status}" == "200" ]] || fail "authenticated GET /v2/ returned ${status}, expected 200"

  "${SUDO[@]}" docker exec "${container}" regixtry tui -storage-root /var/lib/regixtry -snapshot >/dev/null \
    || fail "docker exec ${container} regixtry tui -snapshot did not exit 0"
}

build_binary() {
  local out="${ROOT_DIR}/bin/regixtry"
  mkdir -p "$(dirname "${out}")"
  (cd "${REPO_ROOT}" && go build -ldflags "-X main.buildVersion=${IMAGE_TAG}" -o "${out}" ./cmd/regixtry) \
    || fail "go build ./cmd/regixtry failed"
  BINARY="${out}"
}

require_docker() {
  "${SUDO[@]}" true 2>/dev/null || fail "setup-docker-smoke requires passwordless sudo to reach the Docker socket and to write project state, matching install.sh's own documented 'sudo \${binary} setup --mode docker ...' usage"
  "${SUDO[@]}" docker version >/dev/null 2>&1 || fail "docker is not reachable (even via sudo) -- is the daemon running?"
  "${SUDO[@]}" docker compose version >/dev/null 2>&1 || fail "docker compose v2 plugin is not available"
}

pull_image() {
  "${SUDO[@]}" docker pull "ghcr.io/desatatufuria/regixtry:${IMAGE_TAG}" >/dev/null \
    || fail "could not pull ghcr.io/desatatufuria/regixtry:${IMAGE_TAG} -- pass --image-tag to point at a different published tag"
}

# run_bundled_scenario: clean checkout, no -auth-postgres-dsn, bundled
# Postgres. Proves tasks.md 11.2's full assertion list: stack healthy, the
# generated password present in the 0600 env file, absent from the
# generated docker-compose.yml copy, and printed to setup's own stdout
# exactly once; plus 11.4's auth round trip and TUI check.
run_bundled_scenario() {
  local port project state_dir state_path project_dir log admin_password password

  port="$(reserve_free_port)"
  project="regixtry-smoke-bundled-${RUN_ID}"
  state_dir="${ROOT_DIR}/bundled"
  state_path="${state_dir}/etc/regixtry/bootstrap-state.json"
  project_dir="${state_dir}/etc/regixtry/compose"
  log="${ROOT_DIR}/bundled-setup.log"
  admin_password="regixtry-smoke-admin-${RUN_ID}"

  BUNDLED_PROJECT="${project}"
  BUNDLED_PROJECT_DIR="${project_dir}"

  "${SUDO[@]}" "${BINARY}" setup \
    -mode docker \
    -public-url "http://127.0.0.1:${port}" \
    -state-path "${state_path}" \
    -service "${project}" \
    -admin-username admin \
    -admin-password "${admin_password}" \
    >"${log}" 2>&1 \
    || fail "regixtry setup --mode docker (bundled) failed; see ${log}"

  assert_exists "${project_dir}/regixtry.env"
  assert_mode "${project_dir}/regixtry.env" "600"
  assert_exists "${project_dir}/regixtry-compose-state.json"
  assert_mode "${project_dir}/regixtry-compose-state.json" "600"

  password="$("${SUDO[@]}" grep -oP '(?<=^REGIXTRY_POSTGRES_PASSWORD=).*' "${project_dir}/regixtry.env")"
  [[ -n "${password}" ]] || fail "REGIXTRY_POSTGRES_PASSWORD is empty in ${project_dir}/regixtry.env"

  assert_not_contains "${project_dir}/docker-compose.yml" "${password}"
  assert_not_contains "${project_dir}/regixtry-compose-state.json" "${password}"
  assert_count_one "${log}" "${password}"
  assert_contains "${log}" "Generated bundled Postgres password"
  assert_contains "${log}" "docker exec -it <container> regixtry tui -storage-root /var/lib/regixtry"

  wait_reachable "http://127.0.0.1:${port}" 30
  assert_auth_scenario "http://127.0.0.1:${port}" "${project}-regixtry-1" admin "${admin_password}"

  printf 'setup-docker-smoke: bundled scenario passed (project=%s)\n' "${project}"
}

# run_external_scenario: --external-postgres. Starts a standalone
# postgres:17-alpine on a pre-created, correctly-labeled Compose default
# network so the compose project reuses it instead of erroring on a name
# collision (Compose v2 requires com.docker.compose.network/project labels
# to match before it will attach to a pre-existing network of the same
# name -- confirmed empirically against this sandbox's Compose v5.0.2
# before relying on it here). -auth-postgres-dsn then points the real
# regixtry setup --mode docker invocation at that instance by its
# in-network container DNS name.
run_external_scenario() {
  local port project state_dir state_path project_dir log admin_password
  local pg_password pg_container network dsn password

  port="$(reserve_free_port)"
  project="regixtry-smoke-external-${RUN_ID}"
  network="${project}_default"
  state_dir="${ROOT_DIR}/external"
  state_path="${state_dir}/etc/regixtry/bootstrap-state.json"
  project_dir="${state_dir}/etc/regixtry/compose"
  log="${ROOT_DIR}/external-setup.log"
  admin_password="regixtry-smoke-admin-ext-${RUN_ID}"
  pg_password="regixtry-smoke-pg-${RUN_ID}"
  pg_container="regixtry-smoke-ext-pg-${RUN_ID}"

  EXTERNAL_PROJECT="${project}"
  EXTERNAL_PROJECT_DIR="${project_dir}"
  EXTERNAL_NETWORK="${network}"
  EXTERNAL_PG_CONTAINER="${pg_container}"

  "${SUDO[@]}" docker network create \
    --label com.docker.compose.network=default \
    --label com.docker.compose.project="${project}" \
    --label com.docker.compose.version=2.0.0 \
    "${network}" >/dev/null

  "${SUDO[@]}" docker run -d --name "${pg_container}" --network "${network}" \
    -e POSTGRES_DB=regixtry_auth -e POSTGRES_USER=registry -e "POSTGRES_PASSWORD=${pg_password}" \
    postgres:17-alpine >/dev/null

  wait_postgres_ready "${pg_container}" 60

  dsn="postgres://registry:${pg_password}@${pg_container}:5432/regixtry_auth?sslmode=disable"

  "${SUDO[@]}" "${BINARY}" setup \
    -mode docker \
    -public-url "http://127.0.0.1:${port}" \
    -state-path "${state_path}" \
    -service "${project}" \
    -auth-postgres-dsn "${dsn}" \
    -admin-username admin \
    -admin-password "${admin_password}" \
    >"${log}" 2>&1 \
    || fail "regixtry setup --mode docker (external) failed; see ${log}"

  # The bundled postgres service MUST never be created for an external
  # project (tasks.md 11.3) -- `up -d --no-deps regixtry` never starts it.
  if "${SUDO[@]}" docker compose --project-name "${project}" --file "${project_dir}/docker-compose.yml" --env-file "${project_dir}/regixtry.env" ps -a --format '{{.Service}}' 2>/dev/null | grep -qx postgres; then
    fail "expected no bundled postgres service for an external-DSN project, found one running"
  fi

  assert_contains "${project_dir}/regixtry.env" "REGIXTRY_AUTH_POSTGRES_DSN=${dsn}"
  password="$("${SUDO[@]}" grep -oP '(?<=^REGIXTRY_POSTGRES_PASSWORD=).*' "${project_dir}/regixtry.env")"
  [[ -n "${password}" ]] || fail "REGIXTRY_POSTGRES_PASSWORD is empty in ${project_dir}/regixtry.env (must still be generated even when unused, per design.md)"
  assert_not_contains "${log}" "${password}"

  wait_reachable "http://127.0.0.1:${port}" 30
  assert_auth_scenario "http://127.0.0.1:${port}" "${project}-regixtry-1" admin "${admin_password}"

  printf 'setup-docker-smoke: external-postgres scenario passed (project=%s)\n' "${project}"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --external-postgres)
        EXTERNAL_POSTGRES=1
        shift
        ;;
      --image-tag)
        [[ $# -ge 2 ]] || fail "--image-tag requires a value"
        IMAGE_TAG="$2"
        shift 2
        ;;
      --keep)
        KEEP_ROOT=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      -*)
        usage >&2
        fail "unknown argument: $1"
        ;;
      *)
        ROOT_DIR="$1"
        shift
        ;;
    esac
  done
}

main() {
  parse_args "$@"

  require_cmd docker
  require_cmd go
  require_cmd curl
  require_cmd python3
  require_cmd sudo

  if [[ -z "${ROOT_DIR}" ]]; then
    ROOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-setup-docker-smoke.XXXXXX")"
  fi
  mkdir -p "${ROOT_DIR}"

  require_docker
  build_binary
  pull_image

  if [[ "${EXTERNAL_POSTGRES}" -eq 1 ]]; then
    run_external_scenario
  else
    run_bundled_scenario
  fi

  printf 'setup-docker-smoke: scenario(s) passed. Root: %s\n' "${ROOT_DIR}"
}

main "$@"
