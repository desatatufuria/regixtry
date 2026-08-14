# CI/CD

This page covers two different pipelines:

- **Regixtry's own release pipeline** — how tagged commits in this repository become published binaries.
- **Consumer pipelines** — how a separate project's CI/CD can push images to a running Regixtry instance.

## Regixtry's release pipeline

Releases are produced by `.github/workflows/release.yml` and driven by [GoReleaser](https://goreleaser.com/) via `.goreleaser.yaml`.

**Trigger:** the workflow runs on push of any tag matching `v*` (e.g. `v1.2.3`).

**Steps:**

1. Checkout with full history (`fetch-depth: 0`, required by GoReleaser for changelog generation).
2. Set up Go using the version pinned in `go.mod`.
3. Run the full test suite as a release gate: `go test ./...`. The release does not proceed if any test fails.
4. Run GoReleaser (`goreleaser release --clean`), which builds and publishes the GitHub Release.
5. Verify release artifacts: the workflow asserts that exactly one `amd64` archive, one `arm64` archive, and one checksum file were produced, and that both archive names appear inside the checksum file.
6. Run an installer smoke test: `docs/verification/scripts/install-release-smoke.sh --release-dist dist`, executed with `RUN_INTERACTIVE_CHOOSER=0` so the installer script runs non-interactively against the freshly built `dist/` output.

**What GoReleaser builds** (per `.goreleaser.yaml`):

- Binary: `regixtry`, built from `./cmd/regixtry`.
- Targets: `linux/amd64` and `linux/arm64` only. `CGO_ENABLED=0`.
- Build flags: `-trimpath`, with `ldflags` stamping `main.buildVersion`, `main.buildCommit`, and `main.buildDate`.
- Archive naming: `regixtry_<version>_<os>_<arch>.tar.gz`, containing only the binary (no extra files).
- Checksum file: `regixtry_<version>_checksums.txt`, SHA-256.

So a release of `v1.2.3` produces `regixtry_1.2.3_linux_amd64.tar.gz`, `regixtry_1.2.3_linux_arm64.tar.gz`, and `regixtry_1.2.3_checksums.txt`, published as GitHub Release assets. `install.sh` (see [installation.md](installation.md)) consumes exactly this naming contract when it resolves and downloads a release for the host's OS/architecture.

To cut a release: push a `v*` tag to the repository. There is no manual dispatch trigger and no pipeline for other operating systems or architectures.

## Consumer pipelines

The examples below assume a standard Docker login. With auth enabled, the registry implements the Bearer challenge and `/auth/token`, so no additional pipeline-side endpoint is required.

### GitHub Actions

```yaml
name: build-and-push
on: [push]
jobs:
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          registry: registry.example.com
          username: ${{ secrets.REGISTRY_USERNAME }}
          password: ${{ secrets.REGISTRY_PASSWORD }}
      - run: docker build -t registry.example.com/team/app:${{ github.sha }} .
      - run: docker push registry.example.com/team/app:${{ github.sha }}
```

`docker/build-push-action` can replace the last two steps if the runner and daemon support BuildKit:

```yaml
      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: registry.example.com/team/app:${{ github.sha }}
```

### Azure DevOps

```yaml
trigger:
- main
pool:
  vmImage: ubuntu-latest
variables:
  REGISTRY: registry.example.com
  IMAGE: team/app
steps:
- checkout: self
- bash: |
    set -euo pipefail
    echo "$(REGISTRY_PASSWORD)" | docker login "$(REGISTRY)" -u "$(REGISTRY_USERNAME)" --password-stdin
    docker build -t "$(REGISTRY)/$(IMAGE):$(Build.SourceVersion)" .
    docker push "$(REGISTRY)/$(IMAGE):$(Build.SourceVersion)"
  env:
    REGISTRY_USERNAME: $(REGISTRY_USERNAME)
    REGISTRY_PASSWORD: $(REGISTRY_PASSWORD)
```

Create `REGISTRY_USERNAME` and `REGISTRY_PASSWORD` as secret pipeline variables. The repository does not contain a specific Service Connection.

### Bash

```bash
set -euo pipefail
REGISTRY=registry.example.com
IMAGE=team/app
TAG="${GIT_COMMIT:?}"
printf '%s\n' "$REGISTRY_PASSWORD" | docker login "$REGISTRY" -u "$REGISTRY_USERNAME" --password-stdin
docker push "$REGISTRY/$IMAGE:$TAG"
```
