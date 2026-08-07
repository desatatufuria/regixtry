#!/usr/bin/env bash

set -euo pipefail

SCRIPT_NAME="install.sh"
DEFAULT_BIN_NAME="regixtry"
DEFAULT_RELEASES_API_URL="https://api.github.com/repos/desatatufuria/workspace/releases"
DEFAULT_RELEASES_PAGE_URL="https://github.com/desatatufuria/workspace/releases"

INSTALL_DIR="${REGISTRY_INSTALL_DIR:-}"
REF="${REGISTRY_INSTALL_REF:-}"
RELEASES_API_URL="${REGISTRY_INSTALL_RELEASES_API_URL:-$DEFAULT_RELEASES_API_URL}"
RELEASES_PAGE_URL="${REGISTRY_INSTALL_RELEASES_PAGE_URL:-$DEFAULT_RELEASES_PAGE_URL}"
TARGET_OS="${REGISTRY_INSTALL_OS:-}"
TARGET_ARCH="${REGISTRY_INSTALL_ARCH:-}"
INSTALLER_MODE="${REGISTRY_INSTALL_MODE:-}"
BOOTSTRAP_PUBLIC_URL="${REGISTRY_INSTALL_PUBLIC_URL:-http://127.0.0.1:5000}"
BOOTSTRAP_ADDR="${REGISTRY_INSTALL_ADDR:-127.0.0.1:5000}"
BOOTSTRAP_STORAGE_ROOT="${REGISTRY_INSTALL_STORAGE_ROOT:-}"
BOOTSTRAP_STATE_PATH="${REGISTRY_INSTALL_STATE_PATH:-}"
BOOTSTRAP_UNIT_PATH="${REGISTRY_INSTALL_UNIT_PATH:-}"
BOOTSTRAP_SERVICE_NAME="${REGISTRY_INSTALL_SERVICE_NAME:-regixtry}"
BOOTSTRAP_ROLLBACK=0
TMP_DIR=""

log() {
  printf '[regixtry-install] %s\n' "$*"
}

manual_guidance() {
  cat >&2 <<EOF
[regixtry-install] Manual options:
[regixtry-install] - Download a verified Linux release from: ${RELEASES_PAGE_URL}
[regixtry-install] - Or build from source manually with: git clone https://github.com/desatatufuria/workspace.git && cd workspace && go build -o regixtry ./cmd/regixtry
EOF
}

deferred_mode_guidance() {
  cat <<EOF
Deferred automated paths:
- Postgres-auth deployment: manual today, automated later.
- Container deployment: manual today, automated later.
EOF
}

fail() {
  printf '[regixtry-install] ERROR: %s\n' "$*" >&2
  exit 1
}

fail_with_guidance() {
  printf '[regixtry-install] ERROR: %s\n' "$*" >&2
  manual_guidance
  exit 1
}

usage() {
  cat <<EOF
Install the regixtry binary from verified GitHub Release assets and choose
either a binary-only install or a Linux + systemd daemon/service bootstrap.

Usage:
  ${SCRIPT_NAME} [--ref <release-tag>] [--dir <install-dir>] [installer options] [bootstrap options] [--help]

Options:
  --ref <release-tag>  Install a specific release tag. Defaults to the latest release.
  --dir <path>         Install the binary into this directory.
  --mode <mode>        Installer mode after download. Supported: binary-only or daemon-sqlite.
  --public-url <url>   Public URL passed to regixtry bootstrap. Defaults to http://127.0.0.1:5000.
  --addr <addr>        Listen address passed to regixtry bootstrap. Defaults to 127.0.0.1:5000.
  --storage-root <path>
                       Storage root passed to regixtry bootstrap.
  --state-path <path>  Receipt path passed to regixtry bootstrap.
  --unit-path <path>   Systemd unit path passed to regixtry bootstrap.
  --service <name>     Systemd service name passed to regixtry bootstrap. Defaults to regixtry.
  --rollback           Run bootstrap rollback after installing the verified binary.
  --help               Show this help output.

Environment overrides:
  REGISTRY_INSTALL_REF                Default release tag when --ref is not provided.
  REGISTRY_INSTALL_DIR                Default install directory when --dir is not provided.
  REGISTRY_INSTALL_RELEASES_API_URL   Override the release API base URL.
  REGISTRY_INSTALL_RELEASES_PAGE_URL  Override the release downloads page URL.
  REGISTRY_INSTALL_MODE               Default installer mode when --mode is not provided.
  REGISTRY_INSTALL_PUBLIC_URL         Default bootstrap public URL when --public-url is not provided.
  REGISTRY_INSTALL_ADDR               Default bootstrap listen address when --addr is not provided.
  REGISTRY_INSTALL_STORAGE_ROOT       Default bootstrap storage root when --storage-root is not provided.
  REGISTRY_INSTALL_STATE_PATH         Default bootstrap receipt path when --state-path is not provided.
  REGISTRY_INSTALL_UNIT_PATH          Default bootstrap systemd unit path when --unit-path is not provided.
  REGISTRY_INSTALL_SERVICE_NAME       Default bootstrap systemd service name when --service is not provided.

Examples:
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --ref v1.2.3
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --mode binary-only
  curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --mode daemon-sqlite --public-url https://regixtry.example.com

$(deferred_mode_guidance)
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
      --mode)
        [[ $# -ge 2 ]] || fail "--mode requires a value"
        INSTALLER_MODE="$2"
        shift 2
        ;;
      --public-url)
        [[ $# -ge 2 ]] || fail "--public-url requires a value"
        BOOTSTRAP_PUBLIC_URL="$2"
        shift 2
        ;;
      --addr)
        [[ $# -ge 2 ]] || fail "--addr requires a value"
        BOOTSTRAP_ADDR="$2"
        shift 2
        ;;
      --storage-root)
        [[ $# -ge 2 ]] || fail "--storage-root requires a value"
        BOOTSTRAP_STORAGE_ROOT="$2"
        shift 2
        ;;
      --state-path)
        [[ $# -ge 2 ]] || fail "--state-path requires a value"
        BOOTSTRAP_STATE_PATH="$2"
        shift 2
        ;;
      --unit-path)
        [[ $# -ge 2 ]] || fail "--unit-path requires a value"
        BOOTSTRAP_UNIT_PATH="$2"
        shift 2
        ;;
      --service)
        [[ $# -ge 2 ]] || fail "--service requires a value"
        BOOTSTRAP_SERVICE_NAME="$2"
        shift 2
        ;;
      --rollback)
        BOOTSTRAP_ROLLBACK=1
        shift
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

validate_installer_mode() {
  local mode="$1"

  case "${mode}" in
    binary-only|daemon-sqlite)
      ;;
    *)
      return 1
      ;;
  esac
}

unsupported_mode_guidance() {
  local current_mode="$1"
  local regixtry_binary="$2"

  cat >&2 <<EOF
[regixtry-install] ERROR: unsupported installer mode: ${current_mode}
[regixtry-install] Supported installer modes in this slice:
[regixtry-install] - binary-only
[regixtry-install] - daemon-sqlite (binary + daemon/service on Linux + systemd)
[regixtry-install] The verified regixtry binary remains installed at ${regixtry_binary}
EOF
  deferred_mode_guidance >&2
  exit 1
}

missing_mode_guidance() {
  local regixtry_binary="$1"

  cat >&2 <<EOF
[regixtry-install] ERROR: no installer mode was selected and no controlling TTY is available
[regixtry-install] Non-interactive runs must set --mode or REGISTRY_INSTALL_MODE to binary-only or daemon-sqlite
[regixtry-install] The verified regixtry binary remains installed at ${regixtry_binary}
EOF
  deferred_mode_guidance >&2
  exit 1
}

binary_only_rollback_guidance() {
  local regixtry_binary="$1"

  cat >&2 <<EOF
[regixtry-install] ERROR: --rollback only applies to daemon-sqlite bootstrap artifacts in this slice
[regixtry-install] Use --mode daemon-sqlite --rollback to remove generated service/runtime artifacts
[regixtry-install] The verified regixtry binary remains installed at ${regixtry_binary}
EOF
  exit 1
}

prompt_installer_mode() {
  local choice=""

  exec 3<>/dev/tty || return 1
  while true; do
    cat >&3 <<'EOF'
[regixtry-install] Choose deployment mode:
[regixtry-install] 1) binary only
[regixtry-install] 2) binary + daemon/service (Linux + systemd only)
[regixtry-install] Deferred automated paths:
[regixtry-install] - Postgres-auth deployment: manual today, automated later.
[regixtry-install] - Container deployment: manual today, automated later.
EOF
    printf '[regixtry-install] Enter choice [1-2]: ' >&3
    if ! IFS= read -r choice <&3; then
      exec 3>&-
      return 1
    fi

    case "${choice}" in
      1)
        printf 'binary-only\n'
        exec 3>&-
        return 0
        ;;
      2)
        printf 'daemon-sqlite\n'
        exec 3>&-
        return 0
        ;;
      *)
        printf '[regixtry-install] Invalid choice: %s\n' "${choice}" >&3
        ;;
    esac
  done
}

resolve_installer_mode() {
  local regixtry_binary="$1"
  local resolved_mode="${INSTALLER_MODE}"

  if [[ -n "${resolved_mode}" ]]; then
    validate_installer_mode "${resolved_mode}" || unsupported_mode_guidance "${resolved_mode}" "${regixtry_binary}"
  else
    if ! resolved_mode="$(prompt_installer_mode)"; then
      missing_mode_guidance "${regixtry_binary}"
    fi
  fi

  if [[ "${BOOTSTRAP_ROLLBACK}" == "1" && "${resolved_mode}" != "daemon-sqlite" ]]; then
    binary_only_rollback_guidance "${regixtry_binary}"
  fi

  printf '%s\n' "${resolved_mode}"
}

print_binary_only_success() {
  local regixtry_binary="$1"

  log "Completed binary-only install with ${regixtry_binary}"
  log "Next steps: run '${regixtry_binary} serve -addr ${BOOTSTRAP_ADDR} -public-url ${BOOTSTRAP_PUBLIC_URL} -storage-root ./data -db ./data/metadata.db -service ${BOOTSTRAP_SERVICE_NAME}' when you are ready"
  log "Linux + systemd daemon/service automation remains available through '--mode daemon-sqlite'"
  while IFS= read -r line; do
    [[ -n "${line}" ]] || continue
    log "${line}"
  done < <(deferred_mode_guidance)
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
  if [[ ${#entries[@]} -ne 1 || "${entries[0]}" != "regixtry" ]]; then
    fail_with_guidance "archive must contain exactly one regixtry entry named regixtry"
  fi
}

extract_regixtry_binary() {
  local archive_file="$1"
  local destination="$2"
  local extract_dir="${TMP_DIR}/extract"

  mkdir -p "${extract_dir}"
  if ! tar -xzf "${archive_file}" -C "${extract_dir}" regixtry; then
    fail_with_guidance "failed to extract regixtry from ${archive_file}"
  fi

  install -m 0755 "${extract_dir}/regixtry" "${destination}/${DEFAULT_BIN_NAME}"
}

run_bootstrap() {
  local regixtry_binary="$1"
  local bootstrap_mode="daemon-sqlite"
  local bootstrap_args=("bootstrap" "--mode" "${bootstrap_mode}" "--public-url" "${BOOTSTRAP_PUBLIC_URL}" "--addr" "${BOOTSTRAP_ADDR}" "--service" "${BOOTSTRAP_SERVICE_NAME}")

  if [[ -n "${BOOTSTRAP_STORAGE_ROOT}" ]]; then
    bootstrap_args+=("--storage-root" "${BOOTSTRAP_STORAGE_ROOT}")
  fi
  if [[ -n "${BOOTSTRAP_STATE_PATH}" ]]; then
    bootstrap_args+=("--state-path" "${BOOTSTRAP_STATE_PATH}")
  fi
  if [[ -n "${BOOTSTRAP_UNIT_PATH}" ]]; then
    bootstrap_args+=("--unit-path" "${BOOTSTRAP_UNIT_PATH}")
  fi
  if [[ "${BOOTSTRAP_ROLLBACK}" == "1" ]]; then
    bootstrap_args+=("--rollback")
  fi

  if ! "${regixtry_binary}" "${bootstrap_args[@]}"; then
    printf '[regixtry-install] ERROR: bootstrap command failed; verified regixtry binary remains installed at %s\n' "${regixtry_binary}" >&2
    exit 1
  fi

  if [[ "${BOOTSTRAP_ROLLBACK}" == "1" ]]; then
    log "Rolled back bootstrap artifacts with ${regixtry_binary}"
    return
  fi

  log "Applied bootstrap mode ${bootstrap_mode} with ${regixtry_binary}"
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

  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-install.XXXXXX")"
  release_file="${TMP_DIR}/release.json"

  fetch_release_metadata "${release_file}"

  release_tag="$(extract_tag_name "${release_file}")"
  if [[ -z "${release_tag}" ]]; then
    fail_with_guidance "release metadata did not contain a tag name"
  fi

  archive_url="$(select_release_url "${release_file}" "regixtry_*_${normalized_os}_${normalized_arch}.tar.gz" "release asset")"
  checksums_url="$(select_release_url "${release_file}" "regixtry_*_checksums.txt" "checksum asset")"

  archive_name="${archive_url##*/}"
  checksums_name="${checksums_url##*/}"
  archive_file="${TMP_DIR}/${archive_name}"
  checksums_file="${TMP_DIR}/${checksums_name}"

  log "Resolved release ${release_tag} for ${normalized_os}/${normalized_arch}"
  download_file "${archive_url}" "${archive_file}"
  download_file "${checksums_url}" "${checksums_file}"
  verify_checksum "${archive_file}" "${archive_name}" "${checksums_file}"
  validate_archive "${archive_file}"
  extract_regixtry_binary "${archive_file}" "${INSTALL_DIR}"

  log "Installed ${DEFAULT_BIN_NAME} to ${INSTALL_DIR}/${DEFAULT_BIN_NAME}"

  INSTALLER_MODE="$(resolve_installer_mode "${INSTALL_DIR}/${DEFAULT_BIN_NAME}")"

  case "${INSTALLER_MODE}" in
    binary-only)
      print_binary_only_success "${INSTALL_DIR}/${DEFAULT_BIN_NAME}"
      ;;
    daemon-sqlite)
      run_bootstrap "${INSTALL_DIR}/${DEFAULT_BIN_NAME}"
      ;;
  esac

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
