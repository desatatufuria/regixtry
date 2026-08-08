#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=""
SCRIPT_PATH="${SCRIPT_PATH:-./install.sh}"
KEEP_ROOT="${KEEP_ROOT:-0}"
RELEASE_DIST_DIR="${RELEASE_DIST_DIR:-}"
SERVER_PID=""
SERVER_PORT=""
ORIGINAL_PATH="${PATH}"

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi

  if [[ "${KEEP_ROOT}" != "1" ]]; then
    rm -rf "${ROOT_DIR}"
  fi
}

trap cleanup EXIT

fail() {
  printf 'install-release-smoke: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file="$1"
  local expected="$2"
  grep -F "${expected}" "${file}" >/dev/null || fail "expected '${expected}' in ${file}"
}

assert_not_contains() {
  local file="$1"
  local unexpected="$2"
  if grep -F "${unexpected}" "${file}" >/dev/null; then
    fail "did not expect '${unexpected}' in ${file}"
  fi
}

assert_not_exists() {
  local path="$1"
  [[ ! -e "${path}" ]] || fail "expected path to be absent: ${path}"
}

assert_exists() {
  local path="$1"
  [[ -e "${path}" ]] || fail "expected path to exist: ${path}"
}

assert_executable() {
  local path="$1"
  [[ -x "${path}" ]] || fail "expected path to be executable: ${path}"
}

create_fake_tool_path() {
  local destination="$1"
  local missing="${2:-}"
  local cmd=""
  local source_path=""

  mkdir -p "${destination}"

  for cmd in basename bash cat chmod cp curl cut dirname grep head install mkdir mktemp mv printf pwd rm sed sha256sum tar tr uname; do
    if [[ "${cmd}" == "${missing}" ]]; then
      continue
    fi
    source_path="$(command -v "${cmd}")"
    ln -sf "${source_path}" "${destination}/${cmd}"
  done
}

create_regixtry_tarball() {
  local output="$1"
  local text="$2"
  local workdir="$3"

  mkdir -p "${workdir}/payload"
  cat >"${workdir}/payload/regixtry" <<EOF
#!/usr/bin/env bash
printf '%s\n' '${text}'
EOF
  chmod +x "${workdir}/payload/regixtry"
  tar -C "${workdir}/payload" -czf "${output}" regixtry
}

create_bad_tarball_missing_registry() {
  local output="$1"
  local workdir="$2"

  mkdir -p "${workdir}/missing-regixtry"
  printf 'not-a-binary\n' >"${workdir}/missing-regixtry/README.txt"
  tar -C "${workdir}/missing-regixtry" -czf "${output}" README.txt
}

create_bad_tarball_unexpected_path() {
  local output="$1"
  local workdir="$2"

  mkdir -p "${workdir}/unexpected/bin"
  printf 'wrong path\n' >"${workdir}/unexpected/bin/regixtry"
  tar -C "${workdir}/unexpected" -czf "${output}" bin/regixtry
}

create_bootstrap_stub_tarball() {
  local output="$1"
  local workdir="$2"

  mkdir -p "${workdir}/payload"
  cat >"${workdir}/payload/regixtry" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

write_artifacts() {
  local public_url="$1"
  local addr="$2"
  local storage_root="$3"
  local state_path="$4"
  local unit_path="$5"
  local service_name="$6"
  local env_path

  env_path="$(dirname "${state_path}")/regixtry.env"
  mkdir -p "$(dirname "${state_path}")" "$(dirname "${unit_path}")" "${storage_root}/content"
  : >"${storage_root}/metadata.db"
  cat >"${env_path}" <<ARTIFACTS
REGISTRY_PUBLIC_URL=${public_url}
REGISTRY_ADDR=${addr}
REGISTRY_STORAGE_ROOT=${storage_root}
ARTIFACTS
  cat >"${unit_path}" <<ARTIFACTS
[Unit]
Description=Regixtry smoke stub

[Service]
EnvironmentFile=${env_path}
ExecStart=/usr/local/bin/regixtry serve

[Install]
WantedBy=multi-user.target
ARTIFACTS
  cat >"${state_path}" <<ARTIFACTS
{
  "mode": "daemon-sqlite",
  "service_name": "${service_name}",
  "paths": [
    "${env_path}",
    "${unit_path}",
    "${storage_root}/metadata.db",
    "${storage_root}/content",
    "${state_path}"
  ]
}
ARTIFACTS
}

remove_artifacts() {
  local storage_root="$1"
  local state_path="$2"
  local unit_path="$3"
  local env_path

  env_path="$(dirname "${state_path}")/regixtry.env"
  rm -f "${env_path}" "${unit_path}" "${storage_root}/metadata.db" "${state_path}"
  rm -rf "${storage_root}/content"
}

if [[ $# -eq 0 ]]; then
  fail 'expected subcommand: serve, tui, bootstrap, or bootstrap-admin'
fi

command_name="$1"
shift

case "${command_name}" in
  bootstrap)
    mode=""
    public_url=""
    addr=""
    storage_root=""
    state_path=""
    unit_path=""
    service_name="regixtry"
    rollback="0"

    while [[ $# -gt 0 ]]; do
      case "$1" in
        --mode)
          mode="$2"
          shift 2
          ;;
        --public-url)
          public_url="$2"
          shift 2
          ;;
        --addr)
          addr="$2"
          shift 2
          ;;
        --storage-root)
          storage_root="$2"
          shift 2
          ;;
        --state-path)
          state_path="$2"
          shift 2
          ;;
        --unit-path)
          unit_path="$2"
          shift 2
          ;;
        --service)
          service_name="$2"
          shift 2
          ;;
        --rollback)
          rollback="1"
          shift
          ;;
        *)
          fail "unknown bootstrap arg: $1"
          ;;
      esac
    done

    [[ "${mode}" == "daemon-sqlite" ]] || fail "unsupported mode \"${mode}\": only daemon-sqlite is supported"

    if [[ -n "${BOOTSTRAP_STUB_LOG:-}" ]]; then
      printf 'mode=%s public_url=%s addr=%s storage_root=%s state_path=%s unit_path=%s service=%s rollback=%s\n' \
        "${mode}" "${public_url}" "${addr}" "${storage_root}" "${state_path}" "${unit_path}" "${service_name}" "${rollback}" >>"${BOOTSTRAP_STUB_LOG}"
    fi

    if [[ "${rollback}" == "1" ]]; then
      remove_artifacts "${storage_root}" "${state_path}" "${unit_path}"
      exit 0
    fi

    case "${BOOTSTRAP_STUB_OUTCOME:-success}" in
      success)
        write_artifacts "${public_url}" "${addr}" "${storage_root}" "${state_path}" "${unit_path}" "${service_name}"
        ;;
      unsupported-distro)
        fail 'unsupported Linux distribution "alpine": Alpine host bootstrap is deferred'
        ;;
      start-failure)
        write_artifacts "${public_url}" "${addr}" "${storage_root}" "${state_path}" "${unit_path}" "${service_name}"
        remove_artifacts "${storage_root}" "${state_path}" "${unit_path}"
        fail "systemctl enable --now ${service_name}.service: exit status 1"
        ;;
      *)
        fail "unknown stub outcome: ${BOOTSTRAP_STUB_OUTCOME:-}"
        ;;
    esac
    ;;
  *)
    fail "unknown subcommand \"${command_name}\""
    ;;
esac
EOF
  chmod +x "${workdir}/payload/regixtry"
  tar -C "${workdir}/payload" -czf "${output}" regixtry
}

resolve_release_dist_assets() {
  local dist_dir="$1"
  local amd64=()
  local arm64=()
  local checksums=()

  shopt -s nullglob
  amd64=("${dist_dir}"/regixtry_*_linux_amd64.tar.gz)
  arm64=("${dist_dir}"/regixtry_*_linux_arm64.tar.gz)
  checksums=("${dist_dir}"/regixtry_*_checksums.txt)
  shopt -u nullglob

  [[ ${#amd64[@]} -eq 1 ]] || fail "expected exactly one amd64 archive in ${dist_dir}, found ${#amd64[@]}"
  [[ ${#arm64[@]} -eq 1 ]] || fail "expected exactly one arm64 archive in ${dist_dir}, found ${#arm64[@]}"
  [[ ${#checksums[@]} -eq 1 ]] || fail "expected exactly one checksum file in ${dist_dir}, found ${#checksums[@]}"

  printf '%s\n%s\n%s\n' "${amd64[0]}" "${arm64[0]}" "${checksums[0]}"
}

prepare_good_downloads_from_dist() {
  local dist_dir="$1"
  local web_root="$2"
  local resolved_assets=()
  local amd64_src=""
  local arm64_src=""
  local checksum_src=""
  local amd64_asset=""
  local arm64_asset=""
  local checksum_asset=""

  mapfile -t resolved_assets < <(resolve_release_dist_assets "${dist_dir}")
  amd64_src="${resolved_assets[0]}"
  arm64_src="${resolved_assets[1]}"
  checksum_src="${resolved_assets[2]}"
  amd64_asset="$(basename "${amd64_src}")"
  arm64_asset="$(basename "${arm64_src}")"
  checksum_asset="$(basename "${checksum_src}")"

  cp "${amd64_src}" "${web_root}/downloads/good/${amd64_asset}"
  cp "${arm64_src}" "${web_root}/downloads/good/${arm64_asset}"
  cp "${checksum_src}" "${web_root}/downloads/good/${checksum_asset}"

  grep -F "${amd64_asset}" "${web_root}/downloads/good/${checksum_asset}" >/dev/null || fail "checksum file in ${dist_dir} does not contain ${amd64_asset}"
  grep -F "${arm64_asset}" "${web_root}/downloads/good/${checksum_asset}" >/dev/null || fail "checksum file in ${dist_dir} does not contain ${arm64_asset}"
}

write_release_json() {
  local output="$1"
  local tag="$2"
  shift 2

  {
    printf '{\n'
    printf '  "tag_name": "%s",\n' "${tag}"
    printf '  "assets": [\n'

    local index=0
    local asset_count=$#
    local asset_url=""
    for asset_url in "$@"; do
      index=$((index + 1))
      printf '    {"browser_download_url": "%s"}' "${asset_url}"
      if [[ ${index} -lt ${asset_count} ]]; then
        printf ','
      fi
      printf '\n'
    done

    printf '  ]\n'
    printf '}\n'
  } >"${output}"
}

reserve_server_port() {
  SERVER_PORT="$(python3 - <<'PY'
import socket
sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
)"
}

start_server() {
  python3 -m http.server "${SERVER_PORT}" --bind 127.0.0.1 --directory "${ROOT_DIR}/web" >/dev/null 2>&1 &
  SERVER_PID="$!"

  for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${SERVER_PORT}/healthz" >/dev/null 2>&1; then
      return
    fi
    sleep 0.2
  done

  fail "fixture HTTP server did not start"
}

prepare_fixtures() {
  local web_root="${ROOT_DIR}/web"
  local fixtures_root="${ROOT_DIR}/fixtures"
  local tag="v1.2.3"
  local version="1.2.3"
  local amd64_asset="regixtry_${version}_linux_amd64.tar.gz"
  local arm64_asset="regixtry_${version}_linux_arm64.tar.gz"
  local checksum_asset="regixtry_${version}_checksums.txt"

  mkdir -p "${web_root}/api/good/tags" "${web_root}/api/bootstrap-good/tags" "${web_root}/api/bootstrap-unsupported-distro/tags" "${web_root}/api/bootstrap-start-failure/tags" "${web_root}/api/missing-checksum/tags" "${web_root}/api/missing-asset/tags" "${web_root}/api/checksum-mismatch/tags" "${web_root}/api/bad-archive-missing-entry/tags" "${web_root}/api/bad-archive-unexpected-path/tags" "${web_root}/downloads/good" "${web_root}/downloads/bootstrap-good" "${web_root}/downloads/bootstrap-unsupported-distro" "${web_root}/downloads/bootstrap-start-failure" "${web_root}/downloads/missing-asset" "${web_root}/downloads/checksum-mismatch" "${web_root}/downloads/bad-archive-missing-entry" "${web_root}/downloads/bad-archive-unexpected-path" "${fixtures_root}"

  printf 'ok\n' >"${web_root}/healthz"

  if [[ -n "${RELEASE_DIST_DIR}" ]]; then
    local resolved_assets=()
    if ! mapfile -t resolved_assets < <(resolve_release_dist_assets "${RELEASE_DIST_DIR}"); then
      fail "failed to resolve release assets from ${RELEASE_DIST_DIR}"
    fi
    if [[ ${#resolved_assets[@]} -ne 3 ]]; then
      fail "expected 3 resolved release assets from ${RELEASE_DIST_DIR}, found ${#resolved_assets[@]}"
    fi
    amd64_asset="$(basename "${resolved_assets[0]}")"
    arm64_asset="$(basename "${resolved_assets[1]}")"
    checksum_asset="$(basename "${resolved_assets[2]}")"
    tag="$(printf '%s' "${amd64_asset}" | sed -E 's/^regixtry_(.+)_linux_amd64\.tar\.gz$/v\1/')"
  fi

  create_regixtry_tarball "${web_root}/downloads/good/${amd64_asset}" "regixtry fixture amd64" "${fixtures_root}/good-amd64"
  create_regixtry_tarball "${web_root}/downloads/good/${arm64_asset}" "regixtry fixture arm64" "${fixtures_root}/good-arm64"
  sha256sum "${web_root}/downloads/good/${amd64_asset}" "${web_root}/downloads/good/${arm64_asset}" | sed "s#${web_root}/downloads/good/##" >"${web_root}/downloads/good/${checksum_asset}"

  create_regixtry_tarball "${web_root}/downloads/missing-asset/${arm64_asset}" "regixtry fixture arm64" "${fixtures_root}/missing-asset-arm64"
  create_regixtry_tarball "${web_root}/downloads/checksum-mismatch/${amd64_asset}" "regixtry mismatch amd64" "${fixtures_root}/mismatch-amd64"
  create_bootstrap_stub_tarball "${web_root}/downloads/bootstrap-good/${amd64_asset}" "${fixtures_root}/bootstrap-good"
  create_bootstrap_stub_tarball "${web_root}/downloads/bootstrap-unsupported-distro/${amd64_asset}" "${fixtures_root}/bootstrap-unsupported-distro"
  create_bootstrap_stub_tarball "${web_root}/downloads/bootstrap-start-failure/${amd64_asset}" "${fixtures_root}/bootstrap-start-failure"
  create_bad_tarball_missing_registry "${web_root}/downloads/bad-archive-missing-entry/${amd64_asset}" "${fixtures_root}/bad-missing-entry"
  create_bad_tarball_unexpected_path "${web_root}/downloads/bad-archive-unexpected-path/${amd64_asset}" "${fixtures_root}/bad-unexpected-path"

  printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "${amd64_asset}" >"${web_root}/downloads/checksum-mismatch/${checksum_asset}"
  sha256sum "${web_root}/downloads/bad-archive-missing-entry/${amd64_asset}" | sed "s#${web_root}/downloads/bad-archive-missing-entry/##" >"${web_root}/downloads/bad-archive-missing-entry/${checksum_asset}"
  sha256sum "${web_root}/downloads/bad-archive-unexpected-path/${amd64_asset}" | sed "s#${web_root}/downloads/bad-archive-unexpected-path/##" >"${web_root}/downloads/bad-archive-unexpected-path/${checksum_asset}"
  sha256sum "${web_root}/downloads/missing-asset/${arm64_asset}" | sed "s#${web_root}/downloads/missing-asset/##" >"${web_root}/downloads/missing-asset/${checksum_asset}"
  sha256sum "${web_root}/downloads/bootstrap-good/${amd64_asset}" | sed "s#${web_root}/downloads/bootstrap-good/##" >"${web_root}/downloads/bootstrap-good/${checksum_asset}"
  sha256sum "${web_root}/downloads/bootstrap-unsupported-distro/${amd64_asset}" | sed "s#${web_root}/downloads/bootstrap-unsupported-distro/##" >"${web_root}/downloads/bootstrap-unsupported-distro/${checksum_asset}"
  sha256sum "${web_root}/downloads/bootstrap-start-failure/${amd64_asset}" | sed "s#${web_root}/downloads/bootstrap-start-failure/##" >"${web_root}/downloads/bootstrap-start-failure/${checksum_asset}"

  write_release_json "${web_root}/api/good/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${arm64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${checksum_asset}"
  cp "${web_root}/api/good/latest" "${web_root}/api/good/tags/${tag}"

  write_release_json "${web_root}/api/missing-checksum/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${amd64_asset}"
  cp "${web_root}/api/missing-checksum/latest" "${web_root}/api/missing-checksum/tags/${tag}"

  write_release_json "${web_root}/api/bootstrap-good/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-good/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-good/${checksum_asset}"
  cp "${web_root}/api/bootstrap-good/latest" "${web_root}/api/bootstrap-good/tags/${tag}"

  write_release_json "${web_root}/api/bootstrap-unsupported-distro/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-unsupported-distro/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-unsupported-distro/${checksum_asset}"
  cp "${web_root}/api/bootstrap-unsupported-distro/latest" "${web_root}/api/bootstrap-unsupported-distro/tags/${tag}"

  write_release_json "${web_root}/api/bootstrap-start-failure/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-start-failure/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bootstrap-start-failure/${checksum_asset}"
  cp "${web_root}/api/bootstrap-start-failure/latest" "${web_root}/api/bootstrap-start-failure/tags/${tag}"

  write_release_json "${web_root}/api/missing-asset/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/missing-asset/${checksum_asset}"
  cp "${web_root}/api/missing-asset/latest" "${web_root}/api/missing-asset/tags/${tag}"

  write_release_json "${web_root}/api/checksum-mismatch/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/checksum-mismatch/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/checksum-mismatch/${checksum_asset}"
  cp "${web_root}/api/checksum-mismatch/latest" "${web_root}/api/checksum-mismatch/tags/${tag}"

  write_release_json "${web_root}/api/bad-archive-missing-entry/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bad-archive-missing-entry/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bad-archive-missing-entry/${checksum_asset}"
  cp "${web_root}/api/bad-archive-missing-entry/latest" "${web_root}/api/bad-archive-missing-entry/tags/${tag}"

  write_release_json "${web_root}/api/bad-archive-unexpected-path/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bad-archive-unexpected-path/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/bad-archive-unexpected-path/${checksum_asset}"
  cp "${web_root}/api/bad-archive-unexpected-path/latest" "${web_root}/api/bad-archive-unexpected-path/tags/${tag}"
}

run_success_case() {
  local scenario="$1"
  local api_scope="$2"
  local arch="$3"
  local install_dir="$4"
  local log_file="${ROOT_DIR}/${scenario}.log"
  local run_log="${ROOT_DIR}/${scenario}.run.log"

  mkdir -p "$(dirname "${install_dir}")"
  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="${arch}"

  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_executable "${install_dir}/regixtry"
  assert_contains "${log_file}" "Installed regixtry to ${install_dir}/regixtry"

  if [[ -n "${RELEASE_DIST_DIR}" && "${arch}" == "amd64" ]]; then
    set +e
    "${install_dir}/regixtry" >"${run_log}" 2>&1
    local exit_code=$?
    set -e
    [[ ${exit_code} -ne 0 ]] || fail "expected installed amd64 release binary to exit non-zero without a subcommand"
    assert_contains "${run_log}" "expected subcommand: serve, tui, bootstrap, or bootstrap-admin"
  elif [[ -z "${RELEASE_DIST_DIR}" ]]; then
    assert_contains "${install_dir}/regixtry" "regixtry fixture ${arch}"
  fi
}

run_installer_with_pty() {
  local log_file="$1"
  local pty_input="$2"
  shift 2

  PTY_INPUT="${pty_input}" python3 - "${SCRIPT_PATH}" "${log_file}" "$@" <<'PY'
import errno
import os
import pty
import subprocess
import sys

script_path = sys.argv[1]
log_file = sys.argv[2]
args = sys.argv[3:]
input_data = os.environ.get("PTY_INPUT", "")

master, slave = pty.openpty()
proc = subprocess.Popen(["bash", script_path, *args], stdin=slave, stdout=slave, stderr=slave, env=os.environ.copy(), close_fds=True)
os.close(slave)

if input_data:
    os.write(master, input_data.encode())

chunks = []
while True:
    try:
        data = os.read(master, 4096)
        if data:
            chunks.append(data)
            continue
    except OSError as exc:
        if exc.errno != errno.EIO:
            raise

    if proc.poll() is not None:
        break

proc.wait()
os.close(master)

with open(log_file, "wb") as fh:
    for chunk in chunks:
        fh.write(chunk)

sys.exit(proc.returncode)
PY
}

run_missing_command_case() {
  local command_name="$1"
  local tool_path="${ROOT_DIR}/tool-path-${command_name}"
  local install_dir="${ROOT_DIR}/missing-${command_name}/bin"
  local log_file="${ROOT_DIR}/missing-${command_name}.log"

  export PATH="${ORIGINAL_PATH}"
  create_fake_tool_path "${tool_path}" "${command_name}"

  export HOME="${ROOT_DIR}/home-missing-${command_name}"
  mkdir -p "${HOME}"
  export PATH="${tool_path}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected missing command scenario for ${command_name} to fail"
  assert_contains "${log_file}" "missing required command: ${command_name}"

  export PATH="${ORIGINAL_PATH}"
}

run_failure_case() {
  local scenario="$1"
  local api_scope="$2"
  local arch="$3"
  shift 3
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="${arch}"

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" "$@" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected ${scenario} to fail"
  assert_not_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "Manual options:"
}

run_bootstrap_success_case() {
  local scenario="$1"
  local api_scope="$2"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local storage_root="${ROOT_DIR}/${scenario}/storage"
  local state_path="${ROOT_DIR}/${scenario}/etc/bootstrap-state.json"
  local unit_path="${ROOT_DIR}/${scenario}/systemd/regixtry.service"
  local log_file="${ROOT_DIR}/${scenario}.log"
  local stub_log="${ROOT_DIR}/${scenario}.bootstrap.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  export BOOTSTRAP_STUB_OUTCOME="success"
  export BOOTSTRAP_STUB_LOG="${stub_log}"

  bash "${SCRIPT_PATH}" \
    --dir "${install_dir}" \
    --mode daemon-sqlite \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5110" \
    --storage-root "${storage_root}" \
    --state-path "${state_path}" \
    --unit-path "${unit_path}" \
    --service regixtry >"${log_file}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_executable "${install_dir}/regixtry"
  assert_exists "${storage_root}/metadata.db"
  assert_exists "${storage_root}/content"
  assert_exists "${state_path}"
  assert_exists "${unit_path}"
  assert_exists "$(dirname "${state_path}")/regixtry.env"
  assert_contains "${log_file}" "Applied bootstrap mode daemon-sqlite"
  assert_contains "${stub_log}" "mode=daemon-sqlite"
  assert_contains "${stub_log}" "storage_root=${storage_root}"
}

run_bootstrap_failure_case() {
  local scenario="$1"
  local api_scope="$2"
  local outcome="$3"
  local expected_message="$4"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local storage_root="${ROOT_DIR}/${scenario}/storage"
  local state_path="${ROOT_DIR}/${scenario}/etc/bootstrap-state.json"
  local unit_path="${ROOT_DIR}/${scenario}/systemd/regixtry.service"
  local log_file="${ROOT_DIR}/${scenario}.log"
  local stub_log="${ROOT_DIR}/${scenario}.bootstrap.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  export BOOTSTRAP_STUB_OUTCOME="${outcome}"
  export BOOTSTRAP_STUB_LOG="${stub_log}"

  set +e
  bash "${SCRIPT_PATH}" \
    --dir "${install_dir}" \
    --mode daemon-sqlite \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5111" \
    --storage-root "${storage_root}" \
    --state-path "${state_path}" \
    --unit-path "${unit_path}" \
    --service regixtry >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected ${scenario} to fail"
  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "bootstrap command failed; verified regixtry binary remains installed"
  assert_contains "${log_file}" "${expected_message}"
  assert_contains "${stub_log}" "mode=daemon-sqlite"
  assert_not_exists "${storage_root}/metadata.db"
  assert_not_exists "${storage_root}/content"
  assert_not_exists "${state_path}"
  assert_not_exists "${unit_path}"
  assert_not_exists "$(dirname "${state_path}")/regixtry.env"
}

run_bootstrap_rollback_case() {
  local scenario="$1"
  local api_scope="$2"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local storage_root="${ROOT_DIR}/${scenario}/storage"
  local state_path="${ROOT_DIR}/${scenario}/etc/bootstrap-state.json"
  local unit_path="${ROOT_DIR}/${scenario}/systemd/regixtry.service"
  local apply_log="${ROOT_DIR}/${scenario}.apply.log"
  local rollback_log="${ROOT_DIR}/${scenario}.rollback.log"
  local stub_log="${ROOT_DIR}/${scenario}.bootstrap.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  export BOOTSTRAP_STUB_OUTCOME="success"
  export BOOTSTRAP_STUB_LOG="${stub_log}"

  bash "${SCRIPT_PATH}" \
    --dir "${install_dir}" \
    --mode daemon-sqlite \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5112" \
    --storage-root "${storage_root}" \
    --state-path "${state_path}" \
    --unit-path "${unit_path}" \
    --service regixtry >"${apply_log}" 2>&1

  assert_exists "${state_path}"
  assert_exists "${unit_path}"
  assert_exists "${storage_root}/metadata.db"
  assert_exists "${storage_root}/content"

  bash "${SCRIPT_PATH}" \
    --dir "${install_dir}" \
    --mode daemon-sqlite \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5112" \
    --storage-root "${storage_root}" \
    --state-path "${state_path}" \
    --unit-path "${unit_path}" \
    --service regixtry \
    --rollback >"${rollback_log}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_contains "${rollback_log}" "Rolled back bootstrap artifacts"
  assert_not_exists "${storage_root}/metadata.db"
  assert_not_exists "${storage_root}/content"
  assert_not_exists "${state_path}"
  assert_not_exists "${unit_path}"
  assert_not_exists "$(dirname "${state_path}")/regixtry.env"
  assert_contains "${stub_log}" "rollback=1"
}

run_binary_only_mode_flag_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  unset REGISTRY_INSTALL_MODE

  bash "${SCRIPT_PATH}" --dir "${install_dir}" --mode binary-only >"${log_file}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "Completed binary-only install"
  assert_contains "${log_file}" "Deferred automated paths:"
  assert_contains "${log_file}" "Postgres-auth deployment: manual today, automated later."
  assert_contains "${log_file}" "Container deployment: manual today, automated later."
  assert_not_contains "${log_file}" "Choose deployment mode"
  assert_not_contains "${log_file}" "Applied bootstrap mode daemon-sqlite"
}

run_binary_only_mode_env_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  export REGISTRY_INSTALL_MODE="binary-only"

  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "Completed binary-only install"
  assert_not_contains "${log_file}" "Choose deployment mode"
  unset REGISTRY_INSTALL_MODE
}

run_interactive_binary_only_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  unset REGISTRY_INSTALL_MODE

  run_installer_with_pty "${log_file}" $'1\n' --dir "${install_dir}"

  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "Choose deployment mode"
  assert_contains "${log_file}" "1) binary only"
  assert_contains "${log_file}" "2) binary + daemon/service (Linux + systemd only)"
  assert_contains "${log_file}" "Completed binary-only install"
  assert_not_contains "${log_file}" "Applied bootstrap mode daemon-sqlite"
}

run_interactive_bootstrap_success_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local storage_root="${ROOT_DIR}/${scenario}/storage"
  local state_path="${ROOT_DIR}/${scenario}/etc/bootstrap-state.json"
  local unit_path="${ROOT_DIR}/${scenario}/systemd/regixtry.service"
  local log_file="${ROOT_DIR}/${scenario}.log"
  local stub_log="${ROOT_DIR}/${scenario}.bootstrap.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/bootstrap-good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  export BOOTSTRAP_STUB_OUTCOME="success"
  export BOOTSTRAP_STUB_LOG="${stub_log}"
  unset REGISTRY_INSTALL_MODE

  run_installer_with_pty "${log_file}" $'2\n' \
    --dir "${install_dir}" \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5113" \
    --storage-root "${storage_root}" \
    --state-path "${state_path}" \
    --unit-path "${unit_path}" \
    --service regixtry

  assert_exists "${install_dir}/regixtry"
  assert_exists "${storage_root}/metadata.db"
  assert_exists "${storage_root}/content"
  assert_exists "${state_path}"
  assert_exists "${unit_path}"
  assert_contains "${log_file}" "Choose deployment mode"
  assert_contains "${log_file}" "Applied bootstrap mode daemon-sqlite"
  assert_contains "${stub_log}" "mode=daemon-sqlite"
}

run_missing_mode_without_tty_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  unset REGISTRY_INSTALL_MODE

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected ${scenario} to fail"
  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "no installer mode was selected and no controlling TTY is available"
  assert_contains "${log_file}" "Non-interactive runs must set --mode or REGISTRY_INSTALL_MODE"
  assert_contains "${log_file}" "The verified regixtry binary remains installed"
}

run_unsupported_mode_case() {
  local scenario="$1"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/good"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="amd64"
  unset REGISTRY_INSTALL_MODE

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" --mode postgres-auth >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected ${scenario} to fail"
  assert_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "unsupported installer mode: postgres-auth"
  assert_contains "${log_file}" "Supported installer modes in this slice:"
  assert_contains "${log_file}" "Postgres-auth deployment: manual today, automated later."
}

main() {
  if [[ $# -eq 1 && "$1" == "--release-dist" ]]; then
    fail "usage: $0 [--release-dist <dist-dir>] [root-dir]"
  fi

  if [[ $# -ge 2 && "$1" == "--release-dist" ]]; then
    RELEASE_DIST_DIR="$2"
    shift 2
  fi

  if [[ $# -gt 1 ]]; then
    fail "usage: $0 [--release-dist <dist-dir>] [root-dir]"
  fi

  if [[ $# -eq 1 ]]; then
    ROOT_DIR="$1"
  else
    ROOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/regixtry-install-smoke.XXXXXX")"
  fi

  mkdir -p "${ROOT_DIR}"

  if [[ -n "${RELEASE_DIST_DIR}" && ! -d "${RELEASE_DIST_DIR}" ]]; then
    fail "release dist directory does not exist: ${RELEASE_DIST_DIR}"
  fi

  reserve_server_port
  prepare_fixtures
  start_server

  run_missing_command_case curl
  run_missing_command_case tar
  run_missing_command_case sha256sum

  run_interactive_binary_only_case "interactive-binary-only"
  run_interactive_bootstrap_success_case "interactive-bootstrap-success"
  run_binary_only_mode_flag_case "mode-flag-binary-only"
  run_binary_only_mode_env_case "mode-env-binary-only"
  run_missing_mode_without_tty_case "missing-mode-without-tty"
  run_unsupported_mode_case "unsupported-mode"

  run_bootstrap_success_case "bootstrap-success" bootstrap-good
  run_bootstrap_failure_case "bootstrap-unsupported-distro" bootstrap-unsupported-distro unsupported-distro 'unsupported Linux distribution "alpine": Alpine host bootstrap is deferred'
  run_bootstrap_failure_case "bootstrap-start-failure" bootstrap-start-failure start-failure 'systemctl enable --now regixtry.service: exit status 1'
  run_bootstrap_rollback_case "bootstrap-rollback" bootstrap-good

  run_failure_case "malformed-ref" good amd64 --ref "bad/ref"
  assert_contains "${ROOT_DIR}/malformed-ref.log" "invalid release ref"

  run_failure_case "missing-checksum" missing-checksum amd64
  assert_contains "${ROOT_DIR}/missing-checksum.log" "checksum asset"

  run_failure_case "bad-archive-missing-entry" bad-archive-missing-entry amd64
  assert_contains "${ROOT_DIR}/bad-archive-missing-entry.log" "archive must contain exactly one regixtry entry"

  run_failure_case "bad-archive-unexpected-path" bad-archive-unexpected-path amd64
  assert_contains "${ROOT_DIR}/bad-archive-unexpected-path.log" "archive must contain exactly one regixtry entry"

  run_failure_case "checksum-mismatch" checksum-mismatch amd64
  assert_contains "${ROOT_DIR}/checksum-mismatch.log" "checksum verification failed"

  run_failure_case "missing-release-asset" missing-asset amd64
  assert_contains "${ROOT_DIR}/missing-release-asset.log" "release asset"

  printf 'Installer release smoke scenarios passed. Root: %s\n' "${ROOT_DIR}"
}

main "$@"
