#!/usr/bin/env bash
#
# keyless-signing-smoke.sh — proves the ONE path signing-keyless-verification
# could never exercise in this project's sandbox (see
# openspec/changes/signing-keyless-verification/apply-progress.md "Open
# items"): a GENUINE keyless (Fulcio/OIDC) cosign signature, minted from a
# real short-lived certificate against Sigstore's public-good Fulcio/Rekor
# infrastructure, verifying successfully through regixtry's real pull-time
# gate. Every implementation phase could only prove the fail-closed paths
# (a self-signed, non-chaining test certificate); this script proves the
# success path AND a fail-closed-against-a-real-cert negative case, which is
# the one thing nothing in the test suite has proven yet.
#
# This only works in a real GitHub Actions job with `permissions:
# id-token: write` (cosign's ambient OIDC credential detector needs it) and
# real network access to Sigstore's public Fulcio/Rekor services -- it
# cannot be run in an offline sandbox, and every real run publishes a
# PERMANENT PUBLIC Rekor transparency-log entry tied to this repository's
# real identity. That is why the workflow that invokes this script is
# workflow_dispatch-only, never a push/PR trigger.
#
# Sibling of install-release-smoke.sh and container-release-smoke.sh; same
# fail()/cleanup()-trap conventions and bounded-polling discipline (no fixed
# `sleep N` waits). Combines both siblings' patterns: regixtry itself runs
# as a raw binary on 127.0.0.1 (install-release-smoke.sh's pattern -- cosign
# and the OIDC/Fulcio/Rekor network calls are simpler outside Docker
# networking), while a throwaway Postgres container is managed the same way
# container-release-smoke.sh's --auth-postgres scenario manages one
# (including its wait_postgres_ready/postgres_log_indicates_ready functions,
# reused near-verbatim below with credit, for the exact reason documented
# there: pg_isready against a fresh volume can report ready against the
# postgres image's temporary initdb-only server, well before the final
# server is actually accepting networked connections).
#
# Why this script needs Postgres auth at all, unlike install-release-smoke.sh
# and container-release-smoke.sh's anonymous scenario: reading
# internal/protocol/http/router.go's NewRouter shows /admin/v1/* routes are
# registered ONLY when the authService passed to NewRouter implements
# ports.AdminHTTPService; cmd/regixtry/main.go's newHandler leaves
# authService as a nil interface whenever -auth-postgres-dsn is empty. In
# that anonymous mode PUT /admin/v1/signing-policy is not just unauthorized,
# it 404s -- the route is never mounted. So -allow-anonymous-pull/-push
# cannot be combined with a reachable signing-policy admin API: this script
# always runs with -auth-postgres-dsn set, and once that flag is set,
# newHandler unconditionally swaps in ports.NewPrincipalAccessController,
# which requires an authenticated principal for every /v2/ action --
# -allow-anonymous-pull/-push would be silently ignored anyway in this mode,
# so this script does not pass them. Every /v2/ and /admin/v1/ call below
# authenticates the same way container-release-smoke.sh's run_auth_scenario
# does: bootstrap-admin, then Basic credentials exchanged for a Bearer token
# at GET /auth/token (router.go's authenticate() only ever recognizes
# Bearer -- Basic auth is accepted solely at the /auth/token exchange, never
# directly on /admin/v1 or /v2/ routes), and docker/cosign authenticate via
# an ordinary `docker login` so the token exchange happens through docker's
# own credential flow when they call /v2/.
#
# Also corrects the "poll /healthz" assumption: regixtry has no /healthz
# route. Its own Dockerfile HEALTHCHECK (and install-release-smoke.sh's real
# upgrade smoke case) both poll GET /v2/ and treat 200 OR 401 as ready --
# 401 covers exactly this script's auth-enabled deployment, where /v2/
# answers 401 + WWW-Authenticate once the server is up but before any
# request carries credentials.

set -euo pipefail

ROOT_DIR=""
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RUN_ID="$$-$(date +%s)"

REPOSITORY="smoke/keyless"
TAG="v1"
ADMIN_USERNAME="admin"
ADMIN_PASSWORD="regixtry-smoke-admin-${RUN_ID}"

REGISTRY_PORT=""
PG_PORT=""
PG_CONTAINER=""
SERVER_PID=""

DAEMON_JSON_PATH="/etc/docker/daemon.json"
DAEMON_JSON_BACKUP=""
DAEMON_JSON_HAD_ORIGINAL=0
DAEMON_JSON_MODIFIED=0

fail() {
  printf 'keyless-signing-smoke: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi

  if [[ -n "${REGISTRY_PORT}" ]]; then
    docker rmi -f "127.0.0.1:${REGISTRY_PORT}/${REPOSITORY}:${TAG}" >/dev/null 2>&1 || true
  fi

  if [[ -n "${PG_CONTAINER}" ]]; then
    docker rm -f "${PG_CONTAINER}" >/dev/null 2>&1 || true
  fi

  if [[ "${DAEMON_JSON_MODIFIED}" == "1" ]]; then
    if [[ "${DAEMON_JSON_HAD_ORIGINAL}" == "1" && -n "${DAEMON_JSON_BACKUP}" ]]; then
      sudo cp "${DAEMON_JSON_BACKUP}" "${DAEMON_JSON_PATH}" >/dev/null 2>&1 || true
    else
      sudo rm -f "${DAEMON_JSON_PATH}" >/dev/null 2>&1 || true
    fi
    sudo systemctl restart docker >/dev/null 2>&1 || true
  fi

  if [[ -n "${ROOT_DIR}" ]]; then
    rm -rf "${ROOT_DIR}"
  fi
}

trap cleanup EXIT

reserve_free_port() {
  python3 - <<'PY'
import socket
sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
}

# wait_postgres_ready / postgres_log_indicates_ready mirror
# container-release-smoke.sh's functions of the same name exactly (same
# rationale: the postgres image's own log markers are the only reliable
# "final server accepting networked connections" signal on a fresh volume,
# not pg_isready).
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

# wait_registry_ready polls GET /v2/ for a bounded number of seconds and
# accepts 200 or 401 as ready, mirroring the Dockerfile HEALTHCHECK
# convention (regixtry has no /healthz route) and
# install-release-smoke.sh's run_real_upgrade_success_case readiness probe.
wait_registry_ready() {
  local port="$1"
  local timeout="${2:-30}"
  local elapsed=0
  local status=""

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/v2/" || true)"
    if [[ "${status}" == "200" || "${status}" == "401" ]]; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "regixtry did not become ready on 127.0.0.1:${port} within ${timeout}s (last status: '${status}')"
}

wait_docker_daemon_ready() {
  local timeout="${1:-30}"
  local elapsed=0

  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    if docker info >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  fail "docker daemon did not come back up within ${timeout}s after daemon.json was changed"
}

# configure_insecure_registry allows a plain-HTTP docker push/pull against
# 127.0.0.1:<port>. This edits the HOST daemon's config, so the build below
# must go through the classic docker-backed buildx driver (this script never
# runs docker/setup-buildx-action, deliberately -- see the workflow file's
# own comment: the docker-container driver runs buildkit in an isolated
# container that does not inherit this daemon.json).
configure_insecure_registry() {
  local port="$1"

  if [[ -f "${DAEMON_JSON_PATH}" ]]; then
    DAEMON_JSON_HAD_ORIGINAL=1
    DAEMON_JSON_BACKUP="${ROOT_DIR}/daemon.json.orig"
    sudo cp "${DAEMON_JSON_PATH}" "${DAEMON_JSON_BACKUP}"
  fi

  printf '{"insecure-registries": ["127.0.0.1:%s"]}\n' "${port}" | sudo tee "${DAEMON_JSON_PATH}" >/dev/null
  DAEMON_JSON_MODIFIED=1
  sudo systemctl restart docker
  wait_docker_daemon_ready 30
}

start_postgres() {
  PG_PORT="$(reserve_free_port)"
  PG_CONTAINER="regixtry-keyless-smoke-pg-${RUN_ID}"

  docker run -d --name "${PG_CONTAINER}" \
    -p "127.0.0.1:${PG_PORT}:5432" \
    -e POSTGRES_DB=regixtry_keyless_smoke -e POSTGRES_USER=registry -e POSTGRES_PASSWORD=registry \
    postgres:17-alpine >/dev/null

  wait_postgres_ready "${PG_CONTAINER}" 60
}

# regex_escape escapes RE2 metacharacters so the constructed
# certificate_identity_regexp matches the real Fulcio SAN literally, never
# as an accidentally-loose pattern.
regex_escape() {
  printf '%s' "$1" | sed -E 's/[][(){}.^$|*+?\\]/\\&/g'
}

# bearer_token exchanges Basic credentials for a Bearer access token via
# GET /auth/token, mirroring container-release-smoke.sh's registry_token
# (host-curl variant: regixtry runs on the host here, not in a container, so
# no --network container:<name> sidecar is needed).
bearer_token() {
  local username="$1"
  local password="$2"
  local scope="$3"
  local response=""
  local token=""

  response="$(curl -sS --max-time 20 -u "${username}:${password}" \
    "http://127.0.0.1:${REGISTRY_PORT}/auth/token?service=regixtry&scope=${scope}")"

  token="$(printf '%s' "${response}" | grep -o '"token"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed -E 's/.*"([^"]*)"$/\1/')"
  [[ -n "${token}" ]] || fail "bearer_token: could not parse a token from the /auth/token response: ${response}"

  printf '%s' "${token}"
}

# manifest_digest GETs the reference through the real /v2/ API and prints
# the Docker-Content-Digest response header -- the registry's own answer,
# not a value computed independently by this script.
manifest_digest() {
  local token="$1"
  local reference="$2"
  local headers=""
  local digest=""

  headers="$(curl -sS -D - -o /dev/null --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "http://127.0.0.1:${REGISTRY_PORT}/v2/${REPOSITORY}/manifests/${reference}")"
  digest="$(printf '%s' "${headers}" | tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" {print $2; exit}')"
  [[ -n "${digest}" ]] || fail "manifest_digest: no Docker-Content-Digest header for ${reference}, response headers: ${headers}"

  printf '%s' "${digest}"
}

manifest_status() {
  local token="$1"
  local digest="$2"

  curl -s -o /dev/null -w '%{http_code}' --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "http://127.0.0.1:${REGISTRY_PORT}/v2/${REPOSITORY}/manifests/${digest}"
}

signature_status_body() {
  local token="$1"
  local digest="$2"

  curl -sS --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "http://127.0.0.1:${REGISTRY_PORT}/v2/${REPOSITORY}/manifests/${digest}/signature-status"
}

# put_signing_policy is the admin write path this whole smoke test exists to
# exercise: PUT /admin/v1/signing-policy, the exact route (not under
# /admin/v1/policies/) and payload shape confirmed by reading
# signing_keyless_e2e_test.go's own PUT request.
put_signing_policy() {
  local token="$1"
  local issuer="$2"
  local identity_regexp="$3"
  local status=""
  local body=""

  body="$(python3 - "${issuer}" "${identity_regexp}" <<'PY'
import json
import sys
issuer, identity_regexp = sys.argv[1], sys.argv[2]
print(json.dumps({
    "enabled": True,
    "trusted_identities": [
        {"certificate_identity_regexp": identity_regexp, "certificate_oidc_issuer": issuer},
    ],
}))
PY
)"

  status="$(curl -sS -o "${ROOT_DIR}/signing-policy-put.json" -w '%{http_code}' --max-time 20 \
    -X PUT -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" \
    --data-raw "${body}" \
    "http://127.0.0.1:${REGISTRY_PORT}/admin/v1/signing-policy")"
  [[ "${status}" == "200" ]] || fail "PUT /admin/v1/signing-policy returned ${status}, expected 200, body: $(cat "${ROOT_DIR}/signing-policy-put.json")"
}

json_field() {
  local file="$1"
  local field="$2"

  python3 - "${file}" "${field}" <<'PY'
import json
import sys
path, field = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as fh:
    doc = json.load(fh)
node = doc
for part in field.split("."):
    node = node.get(part, "") if isinstance(node, dict) else ""
print(node)
PY
}

build_and_push_image() {
  local build_dir="${ROOT_DIR}/build-context"
  local image_ref="127.0.0.1:${REGISTRY_PORT}/${REPOSITORY}:${TAG}"
  mkdir -p "${build_dir}"
  printf 'regixtry keyless smoke fixture %s\n' "${RUN_ID}" >"${build_dir}/smoke.txt"
  cat >"${build_dir}/Dockerfile" <<'EOF'
FROM scratch
COPY smoke.txt /smoke.txt
EOF

  # Deliberately build-then-push in two steps, NOT `docker buildx build
  # --push` in one: the default "docker" buildx driver (the one tied to the
  # host daemon, see the workflow file's comment on why no
  # docker/setup-buildx-action step creates a docker-container builder
  # here) only supports the local/tarball/image exporters -- it does not
  # support the registry exporter `--push` selects. `--load` builds and
  # imports into the host daemon's own image store instead, and the
  # ordinary `docker push` that follows goes through that same host daemon,
  # honoring the insecure-registries entry configure_insecure_registry
  # wrote to daemon.json.
  #
  # This also sidesteps regixtry's documented OCI Referrers gap (no GET
  # /v2/<name>/referrers/<digest> yet) for a different reason than the
  # usual `--provenance=false` workaround: plain `docker push` never
  # synthesizes provenance/SBOM attestation referrer manifests in the first
  # place -- that behavior belongs to buildx's own registry exporter
  # (`--push`), which this script never invokes.
  docker buildx build \
    --load \
    -f "${build_dir}/Dockerfile" \
    -t "${image_ref}" \
    "${build_dir}"

  docker push "${image_ref}"
}

main() {
  command -v docker >/dev/null 2>&1 || fail "missing required command: docker"
  command -v go >/dev/null 2>&1 || fail "missing required command: go"
  command -v cosign >/dev/null 2>&1 || fail "missing required command: cosign"
  command -v python3 >/dev/null 2>&1 || fail "missing required command: python3"

  local repository_context="${GITHUB_REPOSITORY:-}"
  local workflow_ref="${GITHUB_WORKFLOW_REF:-}"
  [[ -n "${repository_context}" ]] || fail "GITHUB_REPOSITORY must be set (this script only runs meaningfully inside the real GitHub Actions job)"
  [[ -n "${workflow_ref}" ]] || fail "GITHUB_WORKFLOW_REF must be set (this script only runs meaningfully inside the real GitHub Actions job)"

  ROOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-keyless-smoke.XXXXXX")"

  REGISTRY_PORT="$(reserve_free_port)"
  configure_insecure_registry "${REGISTRY_PORT}"

  start_postgres
  local dsn="postgres://registry:registry@127.0.0.1:${PG_PORT}/regixtry_keyless_smoke?sslmode=disable"

  printf 'building regixtry from source\n'
  (cd "${REPO_ROOT}" && go build -o "${ROOT_DIR}/regixtry" ./cmd/regixtry)

  # bootstrap-admin MUST complete before serve starts: serve fails fast when
  # auth is enabled and no admin exists yet (internal/app/auth/service.go's
  # EnsureBootstrapAdmin), exactly as container-release-smoke.sh documents.
  if ! printf '%s\n' "${ADMIN_PASSWORD}" | "${ROOT_DIR}/regixtry" bootstrap-admin \
    -auth-postgres-dsn "${dsn}" -username "${ADMIN_USERNAME}" -password-stdin; then
    fail "bootstrap-admin failed against ${dsn}"
  fi

  local storage_root="${ROOT_DIR}/storage"
  mkdir -p "${storage_root}"

  "${ROOT_DIR}/regixtry" serve \
    -addr "127.0.0.1:${REGISTRY_PORT}" \
    -public-url "http://127.0.0.1:${REGISTRY_PORT}" \
    -storage-root "${storage_root}" \
    -db "${storage_root}/metadata.db" \
    -auth-postgres-dsn "${dsn}" \
    >"${ROOT_DIR}/regixtry-serve.log" 2>&1 &
  SERVER_PID="$!"

  wait_registry_ready "${REGISTRY_PORT}" 30

  local admin_token=""
  local pull_push_token=""
  admin_token="$(bearer_token "${ADMIN_USERNAME}" "${ADMIN_PASSWORD}" "")"
  pull_push_token="$(bearer_token "${ADMIN_USERNAME}" "${ADMIN_PASSWORD}" "repository:${REPOSITORY}:pull,push")"

  # `docker login` populates the credential store cosign and docker buildx
  # both read (go-containerregistry's DefaultKeychain) -- neither tool needs
  # its own separate auth flag after this.
  printf '%s\n' "${ADMIN_PASSWORD}" | docker login "127.0.0.1:${REGISTRY_PORT}" -u "${ADMIN_USERNAME}" --password-stdin

  printf 'building and pushing throwaway image\n'
  build_and_push_image

  local digest=""
  digest="$(manifest_digest "${pull_push_token}" "${TAG}")"
  printf 'pushed %s/%s@%s\n' "${REPOSITORY}" "${TAG}" "${digest}"

  # cosign sign: no COSIGN_EXPERIMENTAL is set. Verified against cosign's
  # current documented behavior (docs.sigstore.dev quickstart-ci and
  # signing/overview): identity-based (keyless) signing has been the
  # default since cosign v2, and the flag is stale/no longer read. The
  # ambient GitHub Actions OIDC credential is auto-detected purely from the
  # job's `permissions: id-token: write` -- no extra cosign flag is needed
  # for that either. --allow-http-registry is required because this
  # registry is plain HTTP on 127.0.0.1.
  #
  # --registry-referrers-mode legacy is REQUIRED, confirmed the hard way: the
  # first real run of this script (cosign v3.1.3's own default,
  # registry-referrers-mode=oci-1-1) pushed the bundle as a genuine OCI 1.1
  # referrer (subject field, discoverable only via GET
  # /v2/<repo>/referrers/<digest>) and wrote NO sha256-<digest> fallback tag
  # at all -- confirmed via this script's own tags/list diagnostic, which
  # showed only ["v1"]. internal/app/regixtry/service_signing.go's
  # verifyBundleSignature exclusively resolves signing.BundleIndexTag(digest)
  # (the legacy sha256-<hex> tag), never the real Referrers API, so the
  # keyless identity branch was never even reached -- signature-status
  # reported "unsigned". Forcing legacy mode here makes cosign write that
  # tag, matching what verifyBundleSignature actually looks up today. Making
  # verifyBundleSignature itself fall back to the real Referrers API
  # (regixtry already implements GET /v2/<repo>/referrers/<digest>, see
  # openspec/changes/oci-referrers-api/) is tracked as separate follow-up
  # work, not fixed here.
  printf 'signing keylessly via the real GitHub Actions OIDC identity\n'
  cosign sign --yes --allow-http-registry --registry-referrers-mode legacy \
    "127.0.0.1:${REGISTRY_PORT}/${REPOSITORY}@${digest}"

  local identity_san=""
  local identity_regexp=""
  local issuer="https://token.actions.githubusercontent.com"
  identity_san="https://github.com/${workflow_ref}"
  identity_regexp="^$(regex_escape "${identity_san}")\$"
  # The registry's verified_identity is never the bare SAN alone: service.go's
  # composeVerifiedIdentity always composes "<SAN> (<issuer>)" once a
  # TrustedIdentity regexp matches (internal/app/regixtry/service_signing.go).
  # Comparing against the bare SAN here would make the positive case below
  # fail deterministically even on a fully correct verification.
  local expected_verified_identity="${identity_san} (${issuer})"

  printf 'configuring signing policy with matching identity: %s\n' "${identity_regexp}"
  put_signing_policy "${admin_token}" "${issuer}" "${identity_regexp}"

  # Diagnostics printed UNCONDITIONALLY, before the pass/fail assertion
  # below: this is the first real run against live Fulcio/Rekor, so if the
  # positive case fails, the CI log must already carry regixtry's own
  # signature-status verdict (its "reason" string says WHICH check failed --
  # no candidate signature found, bad cert chain, tlog/SET invalid, or
  # identity mismatch) and the actual tags cosign wrote, not just an HTTP
  # code. Re-running blind would mint another real, permanent Rekor entry
  # for no new information.
  printf 'diagnostics -- tags present for %s:\n' "${REPOSITORY}"
  curl -sS --max-time 20 -H "Authorization: Bearer ${pull_push_token}" \
    "http://127.0.0.1:${REGISTRY_PORT}/v2/${REPOSITORY}/tags/list" || true
  printf '\ndiagnostics -- signature-status for %s:\n' "${digest}"
  signature_status_body "${pull_push_token}" "${digest}" | tee "${ROOT_DIR}/signature-status-match.json" || true
  printf '\n'

  # Positive case: real signed manifest, matching policy -- pull-time gate
  # must accept it.
  local pull_status=""
  pull_status="$(manifest_status "${pull_push_token}" "${digest}")"
  [[ "${pull_status}" == "200" ]] || fail "pull with a matching identity policy returned ${pull_status}, expected 200 -- see the signature-status diagnostic above for regixtry's own verdict/reason"

  local state=""
  local verified_identity=""
  state="$(json_field "${ROOT_DIR}/signature-status-match.json" state)"
  verified_identity="$(json_field "${ROOT_DIR}/signature-status-match.json" signature.verified_identity)"
  [[ "${state}" == "verified" ]] || fail "signature-status state = '${state}', want 'verified' (body: $(cat "${ROOT_DIR}/signature-status-match.json"))"
  [[ "${verified_identity}" == "${expected_verified_identity}" ]] || fail "signature-status signature.verified_identity = '${verified_identity}', want '${expected_verified_identity}'"

  printf 'positive case passed: verified identity = %s\n' "${verified_identity}"

  # Negative case, SAME real signed artifact: reconfigure with a
  # non-matching certificate_identity_regexp (right issuer, wrong
  # repository) and confirm the pull-time gate now fails closed against a
  # certificate that genuinely verifies cryptographically -- the one case
  # nothing else in this project has proven.
  local mismatched_regexp="^https://github\\.com/${repository_context}-does-not-exist/.*\$"
  printf 'reconfiguring signing policy with a non-matching identity: %s\n' "${mismatched_regexp}"
  put_signing_policy "${admin_token}" "${issuer}" "${mismatched_regexp}"

  pull_status="$(manifest_status "${pull_push_token}" "${digest}")"
  [[ "${pull_status}" == "403" ]] || fail "pull with a mismatched identity policy returned ${pull_status}, expected 403 (fail-closed against a real cert with the wrong configured identity)"

  signature_status_body "${pull_push_token}" "${digest}" >"${ROOT_DIR}/signature-status-mismatch.json"
  state="$(json_field "${ROOT_DIR}/signature-status-mismatch.json" state)"
  [[ "${state}" == "untrusted" ]] || fail "signature-status state after policy mismatch = '${state}', want 'untrusted' (body: $(cat "${ROOT_DIR}/signature-status-mismatch.json"))"

  printf 'negative case passed: real signature correctly rejected under a mismatched policy\n'
  printf 'keyless-signing-smoke: all scenarios passed. Root: %s\n' "${ROOT_DIR}"
}

main "$@"
