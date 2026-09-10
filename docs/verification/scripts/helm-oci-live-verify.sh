#!/usr/bin/env bash
#
# helm-oci-live-verify.sh -- proves that the user's REAL, already-running
# production regixtry instance (registry.desatatufuria.com) can host Helm
# charts pushed as OCI artifacts (`helm push chart.tgz oci://.../helm`),
# alongside existing container images, with ZERO regixtry-side or
# Traefik-side configuration changes.
#
# This is a much lower-stakes sibling of keyless-signing-live-verify.sh:
# there is no signing, no OIDC identity, no Rekor transparency log, and no
# admin API at all. This script only exercises the ordinary, unauthenticated
# (beyond normal registry auth) OCI Distribution surface every docker/skopeo
# client already uses -- confirmed generic over artifact type by reading the
# actual Go source, not assumed from Helm's own documentation alone:
#
#   - internal/app/regixtry/service.go's parseManifestPayload (~line 392-426)
#     and PublishManifest (~line 274) have NO mediaType allowlist: any valid
#     OCI manifest JSON (config + layers, referenced blobs already pushed) is
#     accepted, regardless of content type. Helm's OCI push
#     (config.mediaType: application/vnd.cncf.helm.config.v1+json, chart
#     layer application/vnd.cncf.helm.chart.content.v1.tar+gzip) needs zero
#     special-casing.
#   - internal/infra/storage/fsblob/store.go's blob upload path
#     (PutUploadChunk/CommitUpload) has NO size cap -- io.Copy with no limit.
#     A Helm chart (KB-scale) is a complete non-issue.
#   - internal/domain/regixtry/repository.go's ParseRepositoryRef accepts any
#     "/"-separated lowercase-alphanumeric-plus-._- segment path, so
#     helm/smoke-test-chart needs no pre-creation -- repositories are created
#     implicitly on first push (ensureRepository in
#     internal/infra/metadata/sqlite/store.go, an INSERT-or-lookup, not a
#     distinct provisioning step).
#   - internal/protocol/http/router.go's tags/list route (handleTags),
#     manifest GET/DELETE route (handleManifest), and /auth/token route
#     (handleToken) are all identically generic over artifact type: standard
#     registry auth applies (Basic credentials exchanged for a Bearer token
#     scoped repository:<name>:pull,push via GET /auth/token, response field
#     "token"), and DELETE /v2/<repo>/manifests/<digest> returns 202 on
#     success, 400 when -delete-enabled is off -- gated by the same flag
#     already confirmed ON for this live instance, no Helm-specific handling
#     anywhere.
#
# Unlike keyless-signing-live-verify.sh, this script:
#   - Never touches any signing policy (global or per-repository) -- nothing
#     here is signed.
#   - Never touches docker or the host daemon's config -- Helm's OCI client
#     is self-contained (go-containerregistry-based, no dockerd dependency),
#     so there is no insecure-registry/daemon.json concern to manage.
#   - Targets a FIXED throwaway repository path (helm/smoke-test-chart,
#     never a real chart name) with a run-unique chart VERSION, so repeated
#     runs never collide with each other but the repository path itself is
#     reused run over run (mirroring keyless-signing-live-verify.sh's own
#     smoke/keyless-live-verify repository, reused the same way).
#   - Proves a full push+pull round trip, not just a successful exit code:
#     the pulled chart's values.yaml is checked for a run-unique marker
#     string, confirming the bytes that came back are genuinely this run's
#     content and not some stale cached artifact.
#
# Sibling of keyless-signing-live-verify.sh: same fail()/cleanup()-trap
# conventions and the same bearer_token/manifest_digest helper pattern,
# duplicated here near-verbatim rather than factored into a shared library
# -- this repo's existing pattern (see that script's own header comment).
#
# This script only needs helm, curl, and python3 -- no docker, no cosign.

set -euo pipefail

RUN_ID="${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}-$(date +%s)"
REPOSITORY="helm/smoke-test-chart"
CHART_NAME="smoke-test-chart"
# Must be valid SemVer (Helm's --version flag rejects anything else). A
# single dot-free "live-verify-<run-id>" prerelease identifier is valid
# under semver 2.0.0 (hyphens are permitted within an alphanumeric
# identifier; the "no leading zero" rule only applies to identifiers made
# up entirely of digits, which this one is not).
VERSION="0.1.0-live-verify-${RUN_ID}"

ROOT_DIR=""
BASE_URL=""
REPO_TOKEN=""
PUSHED_DIGEST=""

fail() {
  printf 'helm-oci-live-verify: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'helm-oci-live-verify: WARNING: %s\n' "$*" >&2
}

# delete_pushed_manifest tries to delete the throwaway manifest/tag this
# script pushed. It NEVER fails the job: a 400 UNSUPPORTED means the live
# instance's -delete-enabled flag is off (confirmed ON for this instance
# today, but this stays tolerant exactly like keyless-signing-live-verify.sh
# in case that ever changes) -- this script only warns loudly so a leftover
# chart is not a silent surprise.
delete_pushed_manifest() {
  local status=""
  local body_file="${ROOT_DIR}/manifest-delete-status.json"

  status="$(curl -sS -o "${body_file}" -w '%{http_code}' --max-time 20 \
    -X DELETE -H "Authorization: Bearer ${REPO_TOKEN}" \
    "${BASE_URL}/v2/${REPOSITORY}/manifests/${PUSHED_DIGEST}" 2>/dev/null || true)"

  case "${status}" in
    202)
      printf 'helm-oci-live-verify: deleted the pushed test manifest %s/%s@%s from the live registry\n' "${REPOSITORY}" "${VERSION}" "${PUSHED_DIGEST}"
      ;;
    400)
      warn "DELETE /v2/${REPOSITORY}/manifests/${PUSHED_DIGEST} returned 400 (the live instance's -delete-enabled flag is most likely off) -- the test chart ${REPOSITORY}:${VERSION} was left behind on the live registry and needs manual cleanup"
      ;;
    *)
      warn "DELETE /v2/${REPOSITORY}/manifests/${PUSHED_DIGEST} returned '${status}' -- the test chart ${REPOSITORY}:${VERSION} may have been left behind on the live registry and needs manual cleanup"
      ;;
  esac
}

cleanup() {
  if [[ "${KEEP_ARTIFACTS:-false}" == "true" ]]; then
    if [[ -n "${PUSHED_DIGEST}" ]]; then
      warn "KEEP_ARTIFACTS=true -- skipping cleanup on purpose. Left on the live registry for manual inspection:"
      warn "  - test chart: ${REPOSITORY}:${VERSION} (${PUSHED_DIGEST}) -- DELETE ${BASE_URL}/v2/${REPOSITORY}/manifests/${PUSHED_DIGEST}"
      warn "Remember to clean this up manually (or re-run this workflow with keep_artifacts=false, which reuses the same repository path and will not remove a prior run's differently-tagged chart)."
    fi
  else
    if [[ -n "${PUSHED_DIGEST}" ]]; then
      delete_pushed_manifest
    fi
  fi

  if [[ -n "${LIVE_REGISTRY_HOST:-}" ]]; then
    helm registry logout "${LIVE_REGISTRY_HOST}" >/dev/null 2>&1 || true
  fi

  if [[ -n "${ROOT_DIR}" ]]; then
    rm -rf "${ROOT_DIR}"
  fi
}

trap cleanup EXIT

# bearer_token exchanges Basic credentials for a Bearer access token via
# GET /auth/token, identical to keyless-signing-live-verify.sh's own helper
# against the same live host. Deliberately never echoes the raw /auth/token
# response on failure: a derived Bearer token is not a value GitHub Actions'
# secret masking knows to redact, and this script talks to a real, live
# registry, so a leaked short-lived credential is a real (if bounded)
# exposure worth avoiding.
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

# tags_list_contains queries GET /v2/<repo>/tags/list directly (not via
# helm/curl exit codes alone) and checks the pushed VERSION is really in the
# registry's own tag list -- regixtry's own record of what it stored, per
# internal/app/regixtry/queries.go's TagsResult{name, tags: []string}.
tags_list_contains() {
  local token="$1"
  local want="$2"
  local status=""
  local body_file="${ROOT_DIR}/tags-list.json"

  status="$(curl -sS -o "${body_file}" -w '%{http_code}' --max-time 20 \
    -H "Authorization: Bearer ${token}" \
    "${BASE_URL}/v2/${REPOSITORY}/tags/list")"
  [[ "${status}" == "200" ]] || fail "GET /v2/${REPOSITORY}/tags/list returned ${status}, expected 200, body: $(cat "${body_file}")"

  python3 - "${body_file}" "${want}" <<'PY'
import json
import sys

path, want = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as fh:
    doc = json.load(fh)
tags = doc.get("tags") or []
sys.exit(0 if want in tags else 1)
PY
}

# build_chart writes a minimal, genuinely throwaway Helm chart -- just
# enough for a valid `helm package`/`helm push`/`helm pull` round trip, not
# a chart meant to ever be installed anywhere. values.yaml carries a
# run-unique marker so the round-trip check below can prove the pulled
# content is really this run's bytes, not a stale cache.
build_chart() {
  local chart_dir="$1"

  mkdir -p "${chart_dir}/templates"

  cat >"${chart_dir}/Chart.yaml" <<EOF
apiVersion: v2
name: ${CHART_NAME}
description: >-
  Throwaway smoke-test chart pushed by helm-oci-live-verify.sh to prove
  regixtry can host Helm charts as generic OCI artifacts, alongside
  container images, with zero regixtry-side configuration. Not a real
  chart; deleted from the live registry at the end of every run.
type: application
version: 0.0.0
EOF

  cat >"${chart_dir}/values.yaml" <<EOF
runMarker: "${RUN_ID}"
EOF

  cat >"${chart_dir}/templates/configmap.yaml" <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: smoke-test-chart
data:
  hello: world
EOF
}

main() {
  command -v helm >/dev/null 2>&1 || fail "missing required command: helm"
  command -v curl >/dev/null 2>&1 || fail "missing required command: curl"
  command -v python3 >/dev/null 2>&1 || fail "missing required command: python3"

  [[ -n "${LIVE_REGISTRY_HOST:-}" ]] || fail "LIVE_REGISTRY_HOST must be set (from secrets.LIVE_REGISTRY_HOST)"
  [[ -n "${LIVE_REGISTRY_USERNAME:-}" ]] || fail "LIVE_REGISTRY_USERNAME must be set (from secrets.LIVE_REGISTRY_USERNAME)"
  [[ -n "${LIVE_REGISTRY_PASSWORD:-}" ]] || fail "LIVE_REGISTRY_PASSWORD must be set (from secrets.LIVE_REGISTRY_PASSWORD)"

  BASE_URL="https://${LIVE_REGISTRY_HOST}"
  ROOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-helm-oci-live-verify.XXXXXX")"

  printf 'target: %s (repository %s, version %s)\n' "${BASE_URL}" "${REPOSITORY}" "${VERSION}"

  local chart_dir="${ROOT_DIR}/chart"
  build_chart "${chart_dir}"

  # `helm registry login` populates the credential store helm's own OCI
  # client reads for both `helm push` and `helm pull` below -- neither
  # command needs its own separate auth flag after this. The password is
  # piped via stdin, never interpolated into the command line or printed.
  printf '%s\n' "${LIVE_REGISTRY_PASSWORD}" | helm registry login "${LIVE_REGISTRY_HOST}" \
    --username "${LIVE_REGISTRY_USERNAME}" --password-stdin

  printf 'packaging %s version %s\n' "${CHART_NAME}" "${VERSION}"
  helm package "${chart_dir}" \
    --version "${VERSION}" \
    --app-version "${VERSION}" \
    --destination "${ROOT_DIR}"

  local chart_archive="${ROOT_DIR}/${CHART_NAME}-${VERSION}.tgz"
  [[ -f "${chart_archive}" ]] || fail "helm package did not produce the expected archive: ${chart_archive}"

  printf 'pushing %s to oci://%s/helm\n' "${chart_archive}" "${LIVE_REGISTRY_HOST}"
  helm push "${chart_archive}" "oci://${LIVE_REGISTRY_HOST}/helm"

  local repo_token=""
  repo_token="$(bearer_token "${LIVE_REGISTRY_USERNAME}" "${LIVE_REGISTRY_PASSWORD}" "repository:${REPOSITORY}:pull,push,delete")"
  REPO_TOKEN="${repo_token}"

  printf 'confirming %s is really in GET /v2/%s/tags/list\n' "${VERSION}" "${REPOSITORY}"
  tags_list_contains "${repo_token}" "${VERSION}" || fail "pushed version ${VERSION} not found in GET ${BASE_URL}/v2/${REPOSITORY}/tags/list -- helm push may not have actually landed as a real OCI artifact"

  local digest=""
  digest="$(manifest_digest "${repo_token}" "${VERSION}")"
  PUSHED_DIGEST="${digest}"
  printf 'pushed %s/%s@%s\n' "${REPOSITORY}" "${VERSION}" "${digest}"

  # Full round trip: pull the chart back down (not just `helm show`, which
  # can be satisfied by partial/cached lookups in some client versions) and
  # check the untarred values.yaml for this run's marker -- proof the bytes
  # that came back are genuinely what was just pushed.
  local pull_dir="${ROOT_DIR}/pulled"
  mkdir -p "${pull_dir}"
  printf 'pulling oci://%s/%s --version %s\n' "${LIVE_REGISTRY_HOST}" "${REPOSITORY}" "${VERSION}"
  helm pull "oci://${LIVE_REGISTRY_HOST}/${REPOSITORY}" \
    --version "${VERSION}" \
    --destination "${pull_dir}" \
    --untar

  local pulled_values="${pull_dir}/${CHART_NAME}/values.yaml"
  [[ -f "${pulled_values}" ]] || fail "helm pull did not produce the expected file: ${pulled_values}"
  grep -q "${RUN_ID}" "${pulled_values}" || fail "round-tripped values.yaml does not contain this run's marker (${RUN_ID}) -- content did not come back correctly"

  printf 'helm-oci-live-verify: round trip passed against %s. Root: %s\n' "${BASE_URL}" "${ROOT_DIR}"
}

main "$@"
