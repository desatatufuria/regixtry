#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=""
SCRIPT_PATH="${SCRIPT_PATH:-./install.sh}"
KEEP_ROOT="${KEEP_ROOT:-0}"
RELEASE_DIST_DIR="${RELEASE_DIST_DIR:-}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
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
  grep -F -- "${expected}" "${file}" >/dev/null || fail "expected '${expected}' in ${file}"
}

assert_not_contains() {
  local file="$1"
  local unexpected="$2"
  if grep -F -- "${unexpected}" "${file}" >/dev/null; then
    fail "did not expect '${unexpected}' in ${file}"
  fi
}

assert_contains_one_of() {
  local file="$1"
  shift
  local candidate=""

  for candidate in "$@"; do
    if grep -F -- "${candidate}" "${file}" >/dev/null; then
      return
    fi
  done

  fail "expected one of [$*] in ${file}"
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

create_binary_tarball() {
  local output="$1"
  local binary_path="$2"
  local workdir="$3"

  mkdir -p "${workdir}/payload"
  cp "${binary_path}" "${workdir}/payload/regixtry"
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

build_local_release_assets() {
  local fixtures_root="$1"
  local web_root="$2"
  local amd64_asset="$3"
  local arm64_asset="$4"
  local checksum_asset="$5"
  local build_dir="${fixtures_root}/build"

  mkdir -p "${build_dir}"

  (
    cd "${REPO_ROOT}" && \
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "${build_dir}/regixtry-amd64" ./cmd/regixtry && \
      CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "${build_dir}/regixtry-arm64" ./cmd/regixtry
  )

  create_binary_tarball "${web_root}/downloads/good/${amd64_asset}" "${build_dir}/regixtry-amd64" "${fixtures_root}/good-amd64"
  create_binary_tarball "${web_root}/downloads/good/${arm64_asset}" "${build_dir}/regixtry-arm64" "${fixtures_root}/good-arm64"
  sha256sum "${web_root}/downloads/good/${amd64_asset}" "${web_root}/downloads/good/${arm64_asset}" | sed "s#${web_root}/downloads/good/##" >"${web_root}/downloads/good/${checksum_asset}"
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

  mkdir -p \
    "${web_root}/api/good/tags" \
    "${web_root}/api/missing-checksum/tags" \
    "${web_root}/api/missing-asset/tags" \
    "${web_root}/api/checksum-mismatch/tags" \
    "${web_root}/api/bad-archive-missing-entry/tags" \
    "${web_root}/api/bad-archive-unexpected-path/tags" \
    "${web_root}/downloads/good" \
    "${web_root}/downloads/missing-asset" \
    "${web_root}/downloads/checksum-mismatch" \
    "${web_root}/downloads/bad-archive-missing-entry" \
    "${web_root}/downloads/bad-archive-unexpected-path" \
    "${fixtures_root}"

  printf 'ok\n' >"${web_root}/healthz"

  if [[ -n "${RELEASE_DIST_DIR}" ]]; then
    local resolved_assets=()
    mapfile -t resolved_assets < <(resolve_release_dist_assets "${RELEASE_DIST_DIR}")
    amd64_asset="$(basename "${resolved_assets[0]}")"
    arm64_asset="$(basename "${resolved_assets[1]}")"
    checksum_asset="$(basename "${resolved_assets[2]}")"
    tag="$(printf '%s' "${amd64_asset}" | sed -E 's/^regixtry_(.+)_linux_amd64\.tar\.gz$/v\1/')"
    prepare_good_downloads_from_dist "${RELEASE_DIST_DIR}" "${web_root}"
  else
    build_local_release_assets "${fixtures_root}" "${web_root}" "${amd64_asset}" "${arm64_asset}" "${checksum_asset}"
  fi

  cp "${web_root}/downloads/good/${amd64_asset}" "${web_root}/downloads/checksum-mismatch/${amd64_asset}"
  printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "${amd64_asset}" >"${web_root}/downloads/checksum-mismatch/${checksum_asset}"

  cp "${web_root}/downloads/good/${arm64_asset}" "${web_root}/downloads/missing-asset/${arm64_asset}"
  sha256sum "${web_root}/downloads/missing-asset/${arm64_asset}" | sed "s#${web_root}/downloads/missing-asset/##" >"${web_root}/downloads/missing-asset/${checksum_asset}"

  create_bad_tarball_missing_registry "${web_root}/downloads/bad-archive-missing-entry/${amd64_asset}" "${fixtures_root}/bad-missing-entry"
  create_bad_tarball_unexpected_path "${web_root}/downloads/bad-archive-unexpected-path/${amd64_asset}" "${fixtures_root}/bad-unexpected-path"
  sha256sum "${web_root}/downloads/bad-archive-missing-entry/${amd64_asset}" | sed "s#${web_root}/downloads/bad-archive-missing-entry/##" >"${web_root}/downloads/bad-archive-missing-entry/${checksum_asset}"
  sha256sum "${web_root}/downloads/bad-archive-unexpected-path/${amd64_asset}" | sed "s#${web_root}/downloads/bad-archive-unexpected-path/##" >"${web_root}/downloads/bad-archive-unexpected-path/${checksum_asset}"

  write_release_json "${web_root}/api/good/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${amd64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${arm64_asset}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${checksum_asset}"
  cp "${web_root}/api/good/latest" "${web_root}/api/good/tags/${tag}"

  write_release_json "${web_root}/api/missing-checksum/latest" "${tag}" \
    "http://127.0.0.1:${SERVER_PORT}/downloads/good/${amd64_asset}"
  cp "${web_root}/api/missing-checksum/latest" "${web_root}/api/missing-checksum/tags/${tag}"

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

prepare_install_env() {
  local scenario="$1"
  local api_scope="$2"
  local arch="$3"

  export HOME="${ROOT_DIR}/home-${scenario}"
  mkdir -p "${HOME}"
  export PATH="${ORIGINAL_PATH}"
  export REGISTRY_INSTALL_RELEASES_API_URL="http://127.0.0.1:${SERVER_PORT}/api/${api_scope}"
  export REGISTRY_INSTALL_RELEASES_PAGE_URL="https://example.invalid/releases"
  export REGISTRY_INSTALL_OS="linux"
  export REGISTRY_INSTALL_ARCH="${arch}"
}

run_missing_command_case() {
  local command_name="$1"
  local tool_path="${ROOT_DIR}/tool-path-${command_name}"
  local install_dir="${ROOT_DIR}/missing-${command_name}/bin"
  local log_file="${ROOT_DIR}/missing-${command_name}.log"

  create_fake_tool_path "${tool_path}" "${command_name}"

  prepare_install_env "missing-${command_name}" good amd64
  export PATH="${tool_path}"

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected missing command scenario for ${command_name} to fail"
  assert_contains "${log_file}" "missing required command: ${command_name}"

  export PATH="${ORIGINAL_PATH}"
}

run_success_case() {
  local scenario="$1"
  local arch="$2"
  local install_dir="$3"
  local log_file="${ROOT_DIR}/${scenario}.log"

  mkdir -p "$(dirname "${install_dir}")"
  prepare_install_env "${scenario}" good "${arch}"

  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1

  assert_exists "${install_dir}/regixtry"
  assert_executable "${install_dir}/regixtry"
  assert_contains "${log_file}" "Installed regixtry to ${install_dir}/regixtry"
  assert_contains "${log_file}" "Binary placement is complete. Continue with the installed lifecycle commands:"
  assert_contains "${log_file}" "sudo ${install_dir}/regixtry setup --mode daemon-sqlite --public-url http://127.0.0.1:5000"
  assert_contains "${log_file}" "sudo ${install_dir}/regixtry uninstall"
  assert_not_contains "${log_file}" "Choose deployment mode"
  assert_not_contains "${log_file}" "Applied bootstrap mode daemon-sqlite"
}

run_failure_case() {
  local scenario="$1"
  local api_scope="$2"
  local arch="$3"
  shift 3
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local log_file="${ROOT_DIR}/${scenario}.log"

  prepare_install_env "${scenario}" "${api_scope}" "${arch}"

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" "$@" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected ${scenario} to fail"
  assert_not_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "Manual options:"
}

run_unsupported_target_case() {
  local install_dir="${ROOT_DIR}/unsupported-target/bin"
  local log_file="${ROOT_DIR}/unsupported-target.log"

  prepare_install_env unsupported-target good amd64
  export REGISTRY_INSTALL_OS="darwin"

  set +e
  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${log_file}" 2>&1
  local exit_code=$?
  set -e

  [[ ${exit_code} -ne 0 ]] || fail "expected unsupported target case to fail"
  assert_not_exists "${install_dir}/regixtry"
  assert_contains "${log_file}" "automated release installation is only available for Linux in this slice"
  assert_contains "${log_file}" "Manual options:"
}

run_lifecycle_cases() {
  local scenario="binary-owned-lifecycle"
  local install_dir="${ROOT_DIR}/${scenario}/bin"
  local install_log="${ROOT_DIR}/${scenario}.install.log"
  local setup_log="${ROOT_DIR}/${scenario}.setup.log"
  local unsupported_log="${ROOT_DIR}/${scenario}.unsupported.log"
  local upgrade_log="${ROOT_DIR}/${scenario}.upgrade.log"
  local uninstall_log="${ROOT_DIR}/${scenario}.uninstall.log"
  local lifecycle_root="${ROOT_DIR}/${scenario}/lifecycle"
  local storage_root="${lifecycle_root}/storage"
  local state_dir="${lifecycle_root}/etc/regixtry"
  local provenance_path="${state_dir}/regixtry-lifecycle-state.json"
  local bootstrap_state_path="${state_dir}/bootstrap-state.json"
  local managed_existing="${storage_root}/content"
  local managed_missing="${storage_root}/metadata.db"
  local managed_bin="${ROOT_DIR}/${scenario}/managed-bin/regixtry"

  prepare_install_env "${scenario}" good amd64
  bash "${SCRIPT_PATH}" --dir "${install_dir}" >"${install_log}" 2>&1

  assert_contains "${install_log}" "Binary placement is complete. Continue with the installed lifecycle commands:"
  assert_not_contains "${install_log}" "Applied bootstrap mode daemon-sqlite"

  "${install_dir}/regixtry" setup --mode binary-only >"${setup_log}" 2>&1
  assert_contains "${setup_log}" "Binary placement is complete, but setup is not yet complete."
  assert_contains "${setup_log}" "run: sudo ${install_dir}/regixtry setup --mode daemon-sqlite --public-url \"<url>\""

  set +e
  "${install_dir}/regixtry" upgrade >"${upgrade_log}" 2>&1
  local upgrade_exit=$?
  set -e
  [[ ${upgrade_exit} -ne 0 ]] || fail "expected upgrade to stay deferred"
  assert_contains "${upgrade_log}" "upgrade is deferred for this slice"

  mkdir -p "${storage_root}" "${state_dir}" "$(dirname "${managed_bin}")"
  mkdir -p "${managed_existing}"
  : >"${managed_bin}"
  : >"${bootstrap_state_path}"
  cat >"${provenance_path}" <<EOF
{
  "version": 1,
  "mode": "daemon-sqlite",
  "installed_bin": "${managed_bin}",
  "service_name": "regixtry",
  "state_path": "${provenance_path}",
  "managed_paths": [
    "${managed_existing}",
    "${managed_missing}",
    "${bootstrap_state_path}"
  ]
}
EOF

  set +e
  "${install_dir}/regixtry" uninstall --state-path "${provenance_path}" >"${uninstall_log}" 2>&1
  local uninstall_exit=$?
  set -e
  [[ ${uninstall_exit} -ne 0 ]] || fail "expected uninstall to report service drift on a non-systemd smoke host"
  assert_contains "${uninstall_log}" "Uninstall report:"
  assert_contains_one_of "${uninstall_log}" "- service regixtry.service: failed" "- service regixtry.service: removed"
  assert_contains "${uninstall_log}" "- ${managed_existing}: removed (path removed)"
  assert_contains "${uninstall_log}" "- ${managed_missing}: missing (path already absent)"
  assert_contains "${uninstall_log}" "- ${managed_bin}: removed (path removed)"
  assert_not_exists "${managed_existing}"
  assert_not_exists "${bootstrap_state_path}"
  assert_not_exists "${provenance_path}"
  assert_not_exists "${managed_bin}"

  set +e
  "${install_dir}/regixtry" setup \
    --mode daemon-sqlite \
    --public-url "http://127.0.0.1:${SERVER_PORT}" \
    --addr "127.0.0.1:5120" \
    --storage-root "${ROOT_DIR}/${scenario}/unsupported-storage" \
    --state-path "${ROOT_DIR}/${scenario}/unsupported-etc/bootstrap-state.json" \
    --unit-path "${ROOT_DIR}/${scenario}/unsupported-systemd/regixtry.service" \
    --service regixtry >"${unsupported_log}" 2>&1
  local unsupported_exit=$?
  set -e
  [[ ${unsupported_exit} -ne 0 ]] || fail "expected daemon-sqlite setup to fail on the smoke host when lifecycle requirements are not met"
  assert_contains_one_of "${unsupported_log}" \
    "unsupported Linux distribution" \
    "systemd runtime not detected" \
    "systemctl daemon-reload" \
    "systemctl enable --now regixtry.service" \
    "registry readiness probe failed"
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

  run_success_case "good-install-amd64" amd64 "${ROOT_DIR}/good-install-amd64/bin"
  run_success_case "good-install-arm64" arm64 "${ROOT_DIR}/good-install-arm64/bin"
  run_lifecycle_cases
  run_unsupported_target_case

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
