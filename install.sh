#!/usr/bin/env bash

set -euo pipefail

SCRIPT_NAME="install.sh"
DEFAULT_BIN_NAME="registry"
DEFAULT_RELEASES_API_URL="https://api.github.com/repos/desatatufuria/workspace/releases"
DEFAULT_RELEASES_PAGE_URL="https://github.com/desatatufuria/workspace/releases"

INSTALL_DIR="${REGISTRY_INSTALL_DIR:-}"
REF="${REGISTRY_INSTALL_REF:-}"
RELEASES_API_URL="${REGISTRY_INSTALL_RELEASES_API_URL:-$DEFAULT_RELEASES_API_URL}"
RELEASES_PAGE_URL="${REGISTRY_INSTALL_RELEASES_PAGE_URL:-$DEFAULT_RELEASES_PAGE_URL}"
TARGET_OS="${REGISTRY_INSTALL_OS:-}"
TARGET_ARCH="${REGISTRY_INSTALL_ARCH:-}"
TMP_DIR=""

log() {
  printf '[registry-install] %s\n' "$*"
}

manual_guidance() {
  cat >&2 <<EOF
[registry-install] Manual options:
[registry-install] - Download a verified Linux release from: ${RELEASES_PAGE_URL}
[registry-install] - Or build from source manually with: git clone https://github.com/desatatufuria/workspace.git && cd workspace && go build -o registry ./cmd/registry
EOF
}

fail() {
  printf '[registry-install] ERROR: %s\n' "$*" >&2
  exit 1
}

fail_with_guidance() {
  printf '[registry-install] ERROR: %s\n' "$*" >&2
  manual_guidance
  exit 1
}

usage() {
  cat <<EOF
Install the registry binary from verified GitHub Release assets.

Usage:
  ${SCRIPT_NAME} [--ref <release-tag>] [--dir <install-dir>] [--help]

Options:
  --ref <release-tag>  Install a specific release tag. Defaults to the latest release.
  --dir <path>         Install the binary into this directory.
  --help               Show this help output.

Environment overrides:
  REGISTRY_INSTALL_REF                Default release tag when --ref is not provided.
  REGISTRY_INSTALL_DIR                Default install directory when --dir is not provided.
  REGISTRY_INSTALL_RELEASES_API_URL   Override the release API base URL.
  REGISTRY_INSTALL_RELEASES_PAGE_URL  Override the release downloads page URL.

Examples:
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --ref v1.2.3
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --dir "$HOME/.local/bin"
EOF
}

cleanup() {
  if [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]]; then
    rm -rf "${TMP_DIR}"
  fi
}

trap cleanup EXIT

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    fail_with_guidance "missing required command: ${cmd}"
  fi
}

resolve_install_dir() {
  if [[ -n "${INSTALL_DIR}" ]]; then
    printf '%s\n' "${INSTALL_DIR}"
    return
  fi

  if [[ -w "/usr/local/bin" ]]; then
    printf '/usr/local/bin\n'
    return
  fi

  printf '%s\n' "${HOME}/.local/bin"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --ref)
        [[ $# -ge 2 ]] || fail "--ref requires a value"
        REF="$2"
        shift 2
        ;;
      --dir)
        [[ $# -ge 2 ]] || fail "--dir requires a value"
        INSTALL_DIR="$2"
        shift 2
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done
}

validate_ref() {
  if [[ -z "${REF}" ]]; then
    return
  fi

  if [[ ! "${REF}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    fail_with_guidance "invalid release ref: ${REF}"
  fi
}

ensure_install_dir() {
  local dir="$1"

  if [[ -e "${dir}" && ! -d "${dir}" ]]; then
    fail "install path exists and is not a directory: ${dir}"
  fi

  mkdir -p "${dir}"

  if [[ ! -w "${dir}" ]]; then
    fail "install directory is not writable: ${dir}"
  fi
}

normalize_os() {
  local raw="${TARGET_OS:-$(uname -s)}"

  case "${raw}" in
    Linux|linux)
      printf 'linux\n'
      ;;
    *)
      fail_with_guidance "automated release installation is only available for Linux in this slice"
      ;;
  esac
}

normalize_arch() {
  local raw="${TARGET_ARCH:-$(uname -m)}"

  case "${raw}" in
    x86_64|amd64)
      printf 'amd64\n'
      ;;
    aarch64|arm64)
      printf 'arm64\n'
      ;;
    *)
      fail_with_guidance "unsupported Linux architecture: ${raw}"
      ;;
  esac
}

fetch_release_metadata() {
  local release_file="$1"
  local endpoint="${RELEASES_API_URL}/latest"

  if [[ -n "${REF}" ]]; then
    endpoint="${RELEASES_API_URL}/tags/${REF}"
  fi

  if ! curl -fsSL "${endpoint}" -o "${release_file}"; then
    fail_with_guidance "failed to resolve release metadata from ${endpoint}"
  fi
}

json_compact() {
  local input_file="$1"
  tr '\n' ' ' <"${input_file}"
}

extract_tag_name() {
  local release_file="$1"
  local compact_json=""

  compact_json="$(json_compact "${release_file}")"
  printf '%s' "${compact_json}" \
    | grep -oE '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' \
    | head -n 1 \
    | sed -E 's/^"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/'
}

collect_download_urls() {
  local release_file="$1"
  local compact_json=""

  compact_json="$(json_compact "${release_file}")"
  printf '%s' "${compact_json}" \
    | grep -oE '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]+"' \
    | sed -E 's/^"browser_download_url"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/'
}

select_release_url() {
  local release_file="$1"
  local pattern="$2"
  local label="$3"
  local matches=()
  local url=""
  local asset_name=""

  while IFS= read -r url; do
    [[ -n "${url}" ]] || continue
    asset_name="${url##*/}"
    if [[ "${asset_name}" == ${pattern} ]]; then
      matches+=("${url}")
    fi
  done < <(collect_download_urls "${release_file}")

  if [[ ${#matches[@]} -eq 0 ]]; then
    fail_with_guidance "${label} was not found in the release metadata"
  fi

  if [[ ${#matches[@]} -gt 1 ]]; then
    fail_with_guidance "multiple ${label} files matched the release metadata"
  fi

  printf '%s\n' "${matches[0]}"
}

download_file() {
  local url="$1"
  local destination="$2"

  if ! curl -fsSL "${url}" -o "${destination}"; then
    fail_with_guidance "failed to download ${url}"
  fi
}

lookup_checksum() {
  local checksums_file="$1"
  local asset_name="$2"
  local line=""
  local checksum=""
  local filename=""

  while IFS= read -r line; do
    [[ -n "${line}" ]] || continue
    if [[ "${line}" != *"  "* ]]; then
      continue
    fi

    checksum="${line%%  *}"
    filename="${line#*  }"

    if [[ "${filename}" == "${asset_name}" ]]; then
      printf '%s\n' "${checksum}"
      return
    fi
  done <"${checksums_file}"

  fail_with_guidance "checksum asset does not contain an entry for ${asset_name}"
}

verify_checksum() {
  local archive_file="$1"
  local asset_name="$2"
  local checksums_file="$3"
  local checksum=""
  local verification_file="${TMP_DIR}/checksum.verify"

  checksum="$(lookup_checksum "${checksums_file}" "${asset_name}")"
  if [[ ! "${checksum}" =~ ^[0-9a-fA-F]{64}$ ]]; then
    fail_with_guidance "checksum entry for ${asset_name} is malformed"
  fi

  printf '%s  %s\n' "${checksum}" "${archive_file}" >"${verification_file}"
  if ! sha256sum --check --status "${verification_file}"; then
    fail_with_guidance "checksum verification failed for ${asset_name}"
  fi
}

validate_archive() {
  local archive_file="$1"
  local entries=()

  mapfile -t entries < <(tar -tzf "${archive_file}")
  if [[ ${#entries[@]} -ne 1 || "${entries[0]}" != "registry" ]]; then
    fail_with_guidance "archive must contain exactly one registry entry named registry"
  fi
}

extract_registry_binary() {
  local archive_file="$1"
  local destination="$2"
  local extract_dir="${TMP_DIR}/extract"

  mkdir -p "${extract_dir}"
  if ! tar -xzf "${archive_file}" -C "${extract_dir}" registry; then
    fail_with_guidance "failed to extract registry from ${archive_file}"
  fi

  install -m 0755 "${extract_dir}/registry" "${destination}/${DEFAULT_BIN_NAME}"
}

main() {
  local normalized_os=""
  local normalized_arch=""
  local release_file=""
  local release_tag=""
  local archive_url=""
  local checksums_url=""
  local archive_name=""
  local checksums_name=""
  local archive_file=""
  local checksums_file=""

  parse_args "$@"
  validate_ref

  require_cmd curl
  require_cmd tar
  require_cmd sha256sum
  require_cmd install
  require_cmd mktemp

  normalized_os="$(normalize_os)"
  normalized_arch="$(normalize_arch)"
  INSTALL_DIR="$(resolve_install_dir)"
  ensure_install_dir "${INSTALL_DIR}"

  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/registry-install.XXXXXX")"
  release_file="${TMP_DIR}/release.json"

  fetch_release_metadata "${release_file}"

  release_tag="$(extract_tag_name "${release_file}")"
  if [[ -z "${release_tag}" ]]; then
    fail_with_guidance "release metadata did not contain a tag name"
  fi

  archive_url="$(select_release_url "${release_file}" "registry_*_${normalized_os}_${normalized_arch}.tar.gz" "release asset")"
  checksums_url="$(select_release_url "${release_file}" "registry_*_checksums.txt" "checksum asset")"

  archive_name="${archive_url##*/}"
  checksums_name="${checksums_url##*/}"
  archive_file="${TMP_DIR}/${archive_name}"
  checksums_file="${TMP_DIR}/${checksums_name}"

  log "Resolved release ${release_tag} for ${normalized_os}/${normalized_arch}"
  download_file "${archive_url}" "${archive_file}"
  download_file "${checksums_url}" "${checksums_file}"
  verify_checksum "${archive_file}" "${archive_name}" "${checksums_file}"
  validate_archive "${archive_file}"
  extract_registry_binary "${archive_file}" "${INSTALL_DIR}"

  log "Installed ${DEFAULT_BIN_NAME} to ${INSTALL_DIR}/${DEFAULT_BIN_NAME}"

  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
      ;;
    *)
      log "${INSTALL_DIR} is not currently on PATH"
      log "Add this to your shell profile: export PATH=\"${INSTALL_DIR}:\$PATH\""
      ;;
  esac
}

main "$@"
