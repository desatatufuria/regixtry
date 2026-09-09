#!/usr/bin/env bash
#
# keyless-signing-live-verify.sh -- live-production companion to
# keyless-signing-smoke.sh. That sibling proves the genuine keyless
# (Fulcio/OIDC) verification round trip against an EPHEMERAL, throwaway
# regixtry instance spun up fresh inside the CI job itself (fresh Postgres
# container, fresh binary built from source, destroyed at job end) --
# touching the global signing policy there is harmless because nothing
# outlives the job.
#
# This script proves the exact same round trip against the user's REAL,
# already-running, production regixtry instance instead. That is a live
# system with real traffic, which raises the stakes considerably and shapes
# every design decision below:
#
#   - It NEVER reads or writes the global signing policy (PUT or GET
#     /admin/v1/signing-policy) -- confirmed by the absence of any reference
#     to that route anywhere in this file. It exclusively uses the
#     PER-REPOSITORY override mechanism instead:
#       PUT    /admin/v1/features/signing/repository-overrides/<repository>
#              body (internal/ports/regixtry.go's SigningOverride):
#              {"enabled":true,"trusted_identities":[{"certificate_identity_regexp":"...","certificate_oidc_issuer":"..."}]}
#       DELETE /admin/v1/features/signing/repository-overrides/<repository>
#              (internal/app/regixtry/repository_overrides.go's
#              ClearRepositoryOverride) -- reverts that ONE repository to
#              whatever the live global policy already is; every other
#              repository on the instance, and the global policy row
#              itself, is never touched.
#   - Every real keyless signature still becomes a PERMANENT PUBLIC Rekor
#     transparency-log entry, exactly like the smoke sibling -- this is why
#     the workflow that invokes this script is workflow_dispatch-only,
#     never push/pull_request, same as keyless-signing-smoke.yml.
#   - Credentials for the live host arrive as LIVE_REGISTRY_HOST/USERNAME/
#     PASSWORD env vars, sourced exclusively from GitHub Secrets by the
#     workflow. They are never printed by this script; Actions' own secret
#     masking covers them as long as they only ever flow through env vars,
#     never a string-interpolated command. LIVE_REGISTRY_USERNAME/PASSWORD
#     must already name an admin principal on the live instance --
#     /admin/v1/features/signing/repository-overrides/* requires
#     IsAdmin:true (admin_handlers_test.go's
#     TestAdminRepositoryOverrideRequiresAdminPrincipal) -- this script has
#     no way to create or verify that account, it is the user's own
#     responsibility, set up outside this workflow via `gh secret set`.
#   - Everything happens under one throwaway repository path,
#     smoke/keyless-live-verify, with a run-unique tag (GitHub's own
#     RUN_ID/RUN_ATTEMPT plus a timestamp), so repeated runs never collide
#     with each other or with anything real, and no other repository on the
#     live registry is ever touched, listed, or referenced.
#   - Cleanup is maximal and runs from an EXIT trap so it fires even when an
#     earlier assertion fails: the repository signing override is always
#     deleted first (reverting the throwaway repository to the live global
#     policy, leaving zero trace of this run in the policy state), and the
#     pushed test manifest/tag is deleted too, but tolerantly -- the live
#     instance's -delete-enabled flag may be off, in which case this script
#     logs a clear warning and leaves the throwaway image behind for manual
#     cleanup rather than failing the whole job over it.
#   - No --allow-http-registry (or any insecure-registry daemon
#     configuration) by default: registry.desatatufuria.com is a real
#     production .com host, almost certainly behind real TLS, and this
#     script has no way to confirm otherwise from a sandbox. LIVE_REGISTRY_INSECURE
#     is an optional escape hatch (see main(), default unset/false) for the
#     user to flip later if it genuinely turns out to be needed.
#
# Sibling of keyless-signing-smoke.sh: same fail()/cleanup()-trap
# conventions and the same bearer_token/regex_escape/manifest_digest/
# signature_status_body/json_field helpers, duplicated here near-verbatim
# rather than factored into a shared library -- this repo's existing
# pattern (compare install-release-smoke.sh and container-release-smoke.sh,
# which duplicate their own fail()/cleanup() conventions independently
# rather than sharing one).
#
# Unlike keyless-signing-smoke.sh, this script never builds regixtry from
# source, never starts Postgres, and never starts a server -- the target is
# already running. It only needs docker, cosign, curl, and python3.

set -euo pipefail

RUN_ID="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-$(date +%s)"
REPOSITORY="smoke/keyless-live-verify"
TAG="live-verify-${RUN_ID}"

ROOT_DIR=""
BASE_URL=""
ADMIN_TOKEN=""
REPO_TOKEN=""
OVERRIDE_SET=0
PUSHED_DIGEST=""

# DAEMON_JSON_* mirror keyless-signing-smoke.sh's own insecure-registry
# state, only ever touched when LIVE_REGISTRY_INSECURE is explicitly set --
# see configure_insecure_registry() below.
DAEMON_JSON_PATH="/etc/docker/daemon.json"
DAEMON_JSON_BACKUP=""
DAEMON_JSON_HAD_ORIGINAL=0
DAEMON_JSON_MODIFIED=0

fail() {
  printf 'keyless-signing-live-verify: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'keyless-signing-live-verify: WARNING: %s\n' "$*" >&2
}

# delete_repository_override reverts the throwaway repository's signing
# policy to whatever the live global policy already is. 204 (removed) and
# 404 (already absent, e.g. a retried cleanup) both count as success --
# ClearRepositoryOverride/the admin resource's own DELETE precedent treats a
# second DELETE as 404, never an error worth failing over.
delete_repository_override() {
  local status=""

  status="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 \
    -X DELETE -H "Authorization: Bearer ${ADMIN_TOKEN}" \
    "${BASE_URL}/admin/v1/features/signing/repository-overrides/${REPOSITORY}")"

  case "${status}" in
    204 | 404)
      OVERRIDE_SET=0
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

# delete_pushed_manifest tries to delete the throwaway manifest/tag this
# script pushed. It NEVER fails the job: a 400 UNSUPPORTED (router.go's
# handleManifest DELETE case, domain.ErrorCodeValidation) means the live
# instance's -delete-enabled flag is off, which is entirely the operator's
# choice -- this script only warns loudly so the leftover image is not a
# silent surprise.
delete_pushed_manifest() {
  local status=""
  local body_file="${ROOT_DIR}/manifest-delete-status.json"

  status="$(curl -sS -o "${body_file}" -w '%{http_code}' --max-time 20 \
    -X DELETE -H "Authorization: Bearer ${REPO_TOKEN}" \
    "${BASE_URL}/v2/${REPOSITORY}/manifests/${PUSHED_DIGEST}" 2>/dev/null || true)"

  case "${status}" in
    202)
      printf 'keyless-signing-live-verify: deleted the pushed test manifest %s/%s@%s from the live registry\n' "${REPOSITORY}" "${TAG}" "${PUSHED_DIGEST}"
      ;;
    400)
      warn "DELETE /v2/${REPOSITORY}/manifests/${PUSHED_DIGEST} returned 400 (the live instance's -delete-enabled flag is most likely off) -- the test image ${REPOSITORY}:${TAG} was left behind on the live registry and needs manual cleanup"
      ;;
    *)
      warn "DELETE /v2/${REPOSITORY}/manifests/${PUSHED_DIGEST} returned '${status}' -- the test image ${REPOSITORY}:${TAG} may have been left behind on the live registry and needs manual cleanup"
      ;;
  esac
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

# configure_insecure_registry is only ever called when LIVE_REGISTRY_INSECURE
# is explicitly set truthy (see main()) -- the default path never edits the
# host daemon's config, because registry.desatatufuria.com is expected to be
# real TLS. This mirrors keyless-signing-smoke.sh's own
# configure_insecure_registry exactly, generalized from a bare port to a
# full host[:port].
configure_insecure_registry() {
  local host="$1"

  if [[ -f "${DAEMON_JSON_PATH}" ]]; then
    DAEMON_JSON_HAD_ORIGINAL=1
    DAEMON_JSON_BACKUP="${ROOT_DIR}/daemon.json.orig"
    sudo cp "${DAEMON_JSON_PATH}" "${DAEMON_JSON_BACKUP}"
  fi

  printf '{"insecure-registries": ["%s"]}\n' "${host}" | sudo tee "${DAEMON_JSON_PATH}" >/dev/null
  DAEMON_JSON_MODIFIED=1
  sudo systemctl restart docker
  wait_docker_daemon_ready 30
}

cleanup() {
  if [[ "${KEEP_ARTIFACTS:-false}" == "true" ]]; then
    if [[ "${OVERRIDE_SET}" == "1" || -n "${PUSHED_DIGEST}" ]]; then
      warn "KEEP_ARTIFACTS=true -- skipping cleanup on purpose. Left on the live registry for manual inspection:"
      [[ "${OVERRIDE_SET}" == "1" ]] && warn "  - signing override: DELETE ${BASE_URL}/admin/v1/features/signing/repository-overrides/${REPOSITORY}"
      [[ -n "${PUSHED_DIGEST}" ]] && warn "  - test image: ${REPOSITORY}:${TAG} (${PUSHED_DIGEST}) -- DELETE ${BASE_URL}/v2/${REPOSITORY}/manifests/${PUSHED_DIGEST}"
      warn "Remember to clean these up manually (or re-run this workflow with keep_artifacts=false, which reuses the same repository path and will not remove a prior run's differently-tagged image)."
    fi
  else
    if [[ "${OVERRIDE_SET}" == "1" ]]; then
      delete_repository_override || warn "failed to delete the ${REPOSITORY} signing override -- it may still be present on the live registry and needs manual removal: DELETE ${BASE_URL}/admin/v1/features/signing/repository-overrides/${REPOSITORY}"
    fi

    if [[ -n "${PUSHED_DIGEST}" ]]; then
      delete_pushed_manifest
    fi
  fi

  if [[ -n "${LIVE_REGISTRY_HOST:-}" ]]; then
    docker rmi -f "${LIVE_REGISTRY_HOST}/${REPOSITORY}:${TAG}" >/dev/null 2>&1 || true
    docker logout "${LIVE_REGISTRY_HOST}" >/dev/null 2>&1 || true
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

# regex_escape escapes RE2 metacharacters so the constructed
# certificate_identity_regexp matches the real Fulcio SAN literally, never
# as an accidentally-loose pattern. Identical to keyless-signing-smoke.sh's.
regex_escape() {
  printf '%s' "$1" | sed -E 's/[][(){}.^$|*+?\\]/\\&/g'
}

# bearer_token exchanges Basic credentials for a Bearer access token via
# GET /auth/token, mirroring keyless-signing-smoke.sh's own bearer_token
# against the real host instead of 127.0.0.1. Deliberately never echoes the
# raw /auth/token response on failure (unlike the smoke sibling): a derived
# Bearer token is not a value GitHub Actions' secret masking knows to
# redact, and this script talks to a real, live registry, so a leaked
# short-lived credential is a real (if bounded) exposure worth avoiding.
bearer_token() {
  local username="$1"
  local password="$2"
  local scope="$3"
  local response=""
  local token=""

  response="$(curl -sS --max-time 20 -u "${username}:${password}" \
    "${BASE_URL}/auth/token?service=regixtry&scope=${scope}")"

  token="$(printf '%s' "${response}" | grep -o '"token"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed -E 's/.*"([^"]*)"$/\1/')"
  [[ -n "${token}" ]] || fail "bearer_token: could not parse a token from the /auth/token response for scope '${scope}' (response withheld -- may contain other issued tokens)"

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
    "${BASE_URL}/v2/${REPOSITORY}/manifests/${reference}")"
  digest="$(printf '%s' "${headers}" | tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" {print $2; exit}')"
  [[ -n "${digest}" ]] || fail "manifest_digest: no Docker-Content-Digest header for ${reference}"

  printf '%s' "${digest}"
}

manifest_status() {
  local token="$1"
  local reference="$2"

  curl -s -o /dev/null -w '%{http_code}' --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "${BASE_URL}/v2/${REPOSITORY}/manifests/${reference}"
}

signature_status_body() {
  local token="$1"
  local digest="$2"

  curl -sS --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "${BASE_URL}/v2/${REPOSITORY}/manifests/${digest}/signature-status"
}

# put_repository_override is the per-repository admin write path this whole
# script exists to exercise safely against a live instance: PUT
# /admin/v1/features/signing/repository-overrides/<repository> -- NEVER
# /admin/v1/signing-policy (the global resource keyless-signing-smoke.sh
# uses, safe only because that script's instance is ephemeral). Route and
# payload shape confirmed by reading internal/ports/regixtry.go's
# SigningOverride and admin_handlers_test.go's own PUT requests against
# /admin/v1/features/signing/repository-overrides/*.
put_repository_override() {
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

  status="$(curl -sS -o "${ROOT_DIR}/repository-override-put.json" -w '%{http_code}' --max-time 20 \
    -X PUT -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" \
    --data-raw "${body}" \
    "${BASE_URL}/admin/v1/features/signing/repository-overrides/${REPOSITORY}")"
  [[ "${status}" == "200" ]] || fail "PUT /admin/v1/features/signing/repository-overrides/${REPOSITORY} returned ${status}, expected 200, body: $(cat "${ROOT_DIR}/repository-override-put.json")"
  OVERRIDE_SET=1
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
  local image_ref="${LIVE_REGISTRY_HOST}/${REPOSITORY}:${TAG}"

  mkdir -p "${build_dir}"
  printf 'regixtry keyless live-verify fixture %s\n' "${RUN_ID}" >"${build_dir}/live-verify.txt"
  cat >"${build_dir}/Dockerfile" <<'EOF'
FROM scratch
COPY live-verify.txt /live-verify.txt
EOF

  # Same build-then-push two-step (not `buildx build --push`) as
  # keyless-signing-smoke.sh, for the same reason: the default "docker"
  # buildx driver only supports local/tarball/image exporters, not the
  # registry exporter `--push` needs.
  docker buildx build \
    --load \
    -f "${build_dir}/Dockerfile" \
    -t "${image_ref}" \
    "${build_dir}"

  docker push "${image_ref}"
}

main() {
  command -v docker >/dev/null 2>&1 || fail "missing required command: docker"
  command -v cosign >/dev/null 2>&1 || fail "missing required command: cosign"
  command -v curl >/dev/null 2>&1 || fail "missing required command: curl"
  command -v python3 >/dev/null 2>&1 || fail "missing required command: python3"

  local repository_context="${GITHUB_REPOSITORY:-}"
  local workflow_ref="${GITHUB_WORKFLOW_REF:-}"
  [[ -n "${repository_context}" ]] || fail "GITHUB_REPOSITORY must be set (this script only runs meaningfully inside the real GitHub Actions job)"
  [[ -n "${workflow_ref}" ]] || fail "GITHUB_WORKFLOW_REF must be set (this script only runs meaningfully inside the real GitHub Actions job)"

  [[ -n "${LIVE_REGISTRY_HOST:-}" ]] || fail "LIVE_REGISTRY_HOST must be set (from secrets.LIVE_REGISTRY_HOST)"
  [[ -n "${LIVE_REGISTRY_USERNAME:-}" ]] || fail "LIVE_REGISTRY_USERNAME must be set (from secrets.LIVE_REGISTRY_USERNAME)"
  [[ -n "${LIVE_REGISTRY_PASSWORD:-}" ]] || fail "LIVE_REGISTRY_PASSWORD must be set (from secrets.LIVE_REGISTRY_PASSWORD)"

  # LIVE_REGISTRY_INSECURE is the escape hatch for requirement 7: this
  # script never assumes registry.desatatufuria.com needs plain HTTP or
  # skip-verify TLS (it is a real .com production host). Left unset/false,
  # nothing below touches TLS behavior or the host daemon's registry config
  # at all. Only set this to "true" later if a real run proves the live
  # host genuinely is not behind valid TLS.
  local insecure="${LIVE_REGISTRY_INSECURE:-false}"
  local scheme="https"
  if [[ "${insecure}" == "1" || "${insecure}" == "true" ]]; then
    scheme="http"
    warn "LIVE_REGISTRY_INSECURE is set -- talking to ${LIVE_REGISTRY_HOST} over plain HTTP, no TLS"
  fi
  BASE_URL="${scheme}://${LIVE_REGISTRY_HOST}"

  ROOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-keyless-live-verify.XXXXXX")"

  if [[ "${scheme}" == "http" ]]; then
    configure_insecure_registry "${LIVE_REGISTRY_HOST}"
  fi

  printf 'target: %s (repository %s, tag %s)\n' "${BASE_URL}" "${REPOSITORY}" "${TAG}"

  local admin_token=""
  local repo_token=""
  admin_token="$(bearer_token "${LIVE_REGISTRY_USERNAME}" "${LIVE_REGISTRY_PASSWORD}" "")"
  repo_token="$(bearer_token "${LIVE_REGISTRY_USERNAME}" "${LIVE_REGISTRY_PASSWORD}" "repository:${REPOSITORY}:pull,push,delete")"
  ADMIN_TOKEN="${admin_token}"
  REPO_TOKEN="${repo_token}"

  # `docker login` populates the credential store cosign and docker buildx
  # both read (go-containerregistry's DefaultKeychain) -- neither tool needs
  # its own separate auth flag after this. The password is piped via stdin,
  # never interpolated into the command line or printed.
  printf '%s\n' "${LIVE_REGISTRY_PASSWORD}" | docker login "${LIVE_REGISTRY_HOST}" -u "${LIVE_REGISTRY_USERNAME}" --password-stdin

  printf 'building and pushing throwaway image %s/%s:%s\n' "${LIVE_REGISTRY_HOST}" "${REPOSITORY}" "${TAG}"
  build_and_push_image

  local digest=""
  digest="$(manifest_digest "${repo_token}" "${TAG}")"
  PUSHED_DIGEST="${digest}"
  printf 'pushed %s/%s@%s\n' "${REPOSITORY}" "${TAG}" "${digest}"

  # cosign sign: same defaults as keyless-signing-smoke.sh (no
  # COSIGN_EXPERIMENTAL, ambient GitHub Actions OIDC auto-detected from
  # `permissions: id-token: write`, --registry-referrers-mode legacy
  # required for the same reason documented there: regixtry's
  # verifyBundleSignature only resolves the legacy sha256-<digest> fallback
  # tag today, not the real OCI 1.1 Referrers API). --allow-http-registry is
  # added only when LIVE_REGISTRY_INSECURE selected plain HTTP above.
  printf 'signing keylessly via the real GitHub Actions OIDC identity\n'
  local sign_flags=(--yes --registry-referrers-mode legacy)
  if [[ "${scheme}" == "http" ]]; then
    sign_flags+=(--allow-http-registry)
  fi
  cosign sign "${sign_flags[@]}" "${LIVE_REGISTRY_HOST}/${REPOSITORY}@${digest}"

  local identity_san=""
  local identity_regexp=""
  local issuer="https://token.actions.githubusercontent.com"
  identity_san="https://github.com/${workflow_ref}"
  identity_regexp="^$(regex_escape "${identity_san}")\$"
  # verified_identity is never the bare SAN alone: composeVerifiedIdentity
  # always composes "<SAN> (<issuer>)" once a TrustedIdentity regexp
  # matches (internal/app/regixtry/service_signing.go), exactly like the
  # smoke sibling documents.
  local expected_verified_identity="${identity_san} (${issuer})"

  printf 'configuring repository override for %s with a matching identity: %s\n' "${REPOSITORY}" "${identity_regexp}"
  put_repository_override "${admin_token}" "${issuer}" "${identity_regexp}"

  printf 'diagnostics -- signature-status for %s:\n' "${digest}"
  signature_status_body "${repo_token}" "${digest}" | tee "${ROOT_DIR}/signature-status-match.json" || true
  printf '\n'

  # Positive case: real signed manifest, matching per-repository override --
  # pull-time gate must accept it.
  local pull_status=""
  pull_status="$(manifest_status "${repo_token}" "${digest}")"
  [[ "${pull_status}" == "200" ]] || fail "pull with a matching repository override returned ${pull_status}, expected 200 -- see the signature-status diagnostic above for regixtry's own verdict/reason"

  local state=""
  local verified_identity=""
  state="$(json_field "${ROOT_DIR}/signature-status-match.json" state)"
  verified_identity="$(json_field "${ROOT_DIR}/signature-status-match.json" signature.verified_identity)"
  [[ "${state}" == "verified" ]] || fail "signature-status state = '${state}', want 'verified' (body: $(cat "${ROOT_DIR}/signature-status-match.json"))"
  [[ "${verified_identity}" == "${expected_verified_identity}" ]] || fail "signature-status signature.verified_identity = '${verified_identity}', want '${expected_verified_identity}'"

  printf 'positive case passed: verified identity = %s\n' "${verified_identity}"

  # Negative case, SAME real signed artifact: reconfigure the SAME
  # repository override with a non-matching certificate_identity_regexp
  # (right issuer, wrong repository) and confirm the pull-time gate now
  # fails closed against a certificate that genuinely verifies
  # cryptographically.
  local mismatched_regexp="^https://github\\.com/${repository_context}-does-not-exist/.*\$"
  printf 'reconfiguring repository override with a non-matching identity: %s\n' "${mismatched_regexp}"
  put_repository_override "${admin_token}" "${issuer}" "${mismatched_regexp}"

  pull_status="$(manifest_status "${repo_token}" "${digest}")"
  [[ "${pull_status}" == "403" ]] || fail "pull with a mismatched repository override returned ${pull_status}, expected 403 (fail-closed against a real cert with the wrong configured identity)"

  signature_status_body "${repo_token}" "${digest}" >"${ROOT_DIR}/signature-status-mismatch.json"
  state="$(json_field "${ROOT_DIR}/signature-status-mismatch.json" state)"
  [[ "${state}" == "untrusted" ]] || fail "signature-status state after override mismatch = '${state}', want 'untrusted' (body: $(cat "${ROOT_DIR}/signature-status-mismatch.json"))"

  printf 'negative case passed: real signature correctly rejected under a mismatched repository override\n'
  printf 'keyless-signing-live-verify: all scenarios passed against %s. Root: %s\n' "${BASE_URL}" "${ROOT_DIR}"
}

main "$@"
