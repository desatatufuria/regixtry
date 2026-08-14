# Installation

## From a Linux release

`install.sh` downloads release metadata from GitHub Releases, fetches the tarball and its checksum file, validates the SHA-256, requires the archive to contain only the `regixtry` binary, and installs it as an executable.

```bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/regixtry/main/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/regixtry/main/install.sh | bash -s -- --ref <release-tag>
curl -fsSL https://raw.githubusercontent.com/desatatufuria/regixtry/main/install.sh | bash -s -- --dir "$HOME/.local/bin"
```

Requires `curl`, `tar`, `sha256sum`, `install`, and `mktemp`. The install directory defaults to `/usr/local/bin` when writable, otherwise `$HOME/.local/bin`. Only Linux `amd64` and `arm64` are resolved.

## From source

```bash
git clone https://github.com/desatatufuria/regixtry.git
cd regixtry
go build -o regixtry ./cmd/regixtry
```

The Go version declared in `go.mod` is `1.26.0`.

## Linux + systemd setup

```bash
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url http://127.0.0.1:5000 \
  --runtime-tls-mode local-http \
  --addr 127.0.0.1:5000
```

Setup creates the env file, the systemd unit, `metadata.db`, `content/`, the bootstrap receipt, and the lifecycle provenance record. Defaults are `/var/lib/regixtry`, `/etc/regixtry/bootstrap-state.json`, and `/etc/systemd/system/regixtry.service`.

### Built-in Trivy feature migration

The setup-managed env file stays base-only. It records `REGISTRY_ADDR`, `REGISTRY_PUBLIC_URL`, `REGISTRY_STORAGE_ROOT`, `REGISTRY_DATABASE_PATH`, `REGISTRY_SERVICE_NAME`, and optional auth/TLS inputs, but it no longer stores long-term Trivy settings.

For one migration slice, operators may still pass legacy `setup --trivy-*` flags. Setup imports only the shared scheduling knobs into authoritative feature state when no Trivy feature state exists yet, then operators should use `regixtry feature ...` for future changes. Legacy `service_url`, `binary_path`, and `cache_dir` values become migration evidence only; Regixtry will not execute them.

Example:

```bash
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url https://registry.example.com \
  --runtime-tls-mode reverse-proxy \
  --trivy-enabled \
  --trivy-schedule-enabled \
  --trivy-interval 6h \
  --trivy-timeout 20m \
  --trivy-max-concurrency 2

regixtry feature configure trivy \
  -registry-reachable-url https://registry.internal:5443 \
  -enabled \
  -schedule-enabled \
  -interval 6h \
  -timeout 20m \
  -max-concurrency 2

regixtry feature install trivy -version 0.57.1

regixtry feature status trivy

regixtry feature upgrade trivy -version 0.58.0
regixtry feature rollback trivy
```

`feature install` and `feature upgrade` now emit staged progress so long-running managed-runtime work does not look hung. After installation, operators can use `regixtry feature list` for a table view of `CURRENT`, `LATEST`, and `UPDATE`, and `regixtry feature status trivy` for the same request-scoped latest-version awareness with an `unknown` fallback.

Operational commands:

```bash
sudo systemctl status regixtry
sudo systemctl start regixtry
sudo systemctl stop regixtry
sudo systemctl restart regixtry
sudo journalctl -u regixtry
```

`reverse-proxy` requires the proxy to terminate TLS while Regixtry listens over plain HTTP. `direct-tls` requires both the certificate and key PEM files.

## Uninstall and upgrade

```bash
sudo /absolute/path/to/regixtry uninstall
sudo /absolute/path/to/regixtry upgrade --yes
```

Uninstall uses the persisted provenance record and reports removed, missing, skipped, or failed items. It does not remove unrecorded drift.
