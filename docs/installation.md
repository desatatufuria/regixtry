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

### Optional Trivy rescan defaults

The managed env file now also records the optional Trivy rescan settings used to seed runtime defaults:

- `REGISTRY_TRIVY_ENABLED="false"`
- `REGISTRY_TRIVY_SCHEDULE_ENABLED="false"`
- `REGISTRY_TRIVY_INTERVAL="24h0m0s"`
- `REGISTRY_TRIVY_TIMEOUT="15m0s"`
- `REGISTRY_TRIVY_CACHE_DIR="<storage-root>/trivy-cache"`
- `REGISTRY_TRIVY_BINARY_PATH="trivy"`
- `REGISTRY_TRIVY_MAX_CONCURRENCY="1"`

Operators can override them during setup/bootstrap with `--trivy-*` flags. The admin API becomes the authoritative mutable surface after first boot.

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
