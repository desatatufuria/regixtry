# Instalación

## Desde release Linux

`install.sh` descarga metadata de GitHub Releases, obtiene el tarball y el archivo de checksums, valida SHA-256, exige que el archivo contenga únicamente `regixtry` y lo instala como ejecutable.

```bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --ref <release-tag>
curl -fsSL https://raw.githubusercontent.com/desatatufuria/workspace/main/install.sh | bash -s -- --dir "$HOME/.local/bin"
```

Requiere `curl`, `tar`, `sha256sum`, `install` y `mktemp`. La ruta automática es `/usr/local/bin` si es escribible; en otro caso `$HOME/.local/bin`. Solo se resuelven Linux `amd64` y `arm64`.

## Desde fuente

```bash
git clone https://github.com/desatatufuria/workspace.git
cd workspace
go build -o regixtry ./cmd/regixtry
```

La versión Go declarada en `go.mod` es `1.26.0`.

## Setup Linux + systemd

```bash
sudo /absolute/path/to/regixtry setup --mode daemon-sqlite \
  --public-url http://127.0.0.1:5000 \
  --runtime-tls-mode local-http \
  --addr 127.0.0.1:5000
```

El setup crea el env file, la unidad systemd, `metadata.db`, `content/`, el recibo de bootstrap y la provenance de lifecycle. Los defaults son `/var/lib/regixtry`, `/etc/regixtry/bootstrap-state.json` y `/etc/systemd/system/regixtry.service`.

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

Comandos operativos:

```bash
sudo systemctl status regixtry
sudo systemctl start regixtry
sudo systemctl stop regixtry
sudo systemctl restart regixtry
sudo journalctl -u regixtry
```

`reverse-proxy` requiere que el proxy termine TLS y que Regixtry escuche normalmente por HTTP. `direct-tls` requiere ambos archivos PEM de certificado y clave.

## Uninstall y upgrade

```bash
sudo /absolute/path/to/regixtry uninstall
sudo /absolute/path/to/regixtry upgrade --yes
```

El uninstall utiliza la provenance persistida y reporta elementos eliminados, ausentes, omitidos o fallidos. No elimina drift no registrado.
