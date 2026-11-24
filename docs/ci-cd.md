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

## Live verification workflows

`.github/workflows/helm-oci-live-verify.yml`, `.github/workflows/keyless-signing-live-verify.yml` and `.github/workflows/keyless-signing-smoke.yml` are `workflow_dispatch`-only. The first two run against a real, already-running registry using the `LIVE_REGISTRY_HOST`, `LIVE_REGISTRY_USERNAME` and `LIVE_REGISTRY_PASSWORD` secrets, which are a production **admin** credential. The smoke workflow uses no registry secrets but mints a real OIDC identity token that signs permanent public Rekor entries as this repository. All three run `docs/verification/scripts/*.sh` from the dispatched ref, so whoever can dispatch a workflow on a branch they control can change what runs with those credentials.

**Ref guard (in code): a safeguard against accidental dispatch only.** Each workflow starts with a `ref-guard` job that fails with an explicit error unless the run was dispatched from `develop` or a `v*` tag; the real job `needs` it (and repeats the same condition in its own `if:`). This stops a careless dispatch from a feature branch. It is **not** a security control against a malicious repository writer: GitHub reads the workflow file from the dispatched ref, so anyone who can push a branch can delete `ref-guard`, `needs` and `if:` from their copy and dispatch from that branch with the repository-level secrets (and the OIDC token) in scope. The keyless workflows keep `permissions: id-token: write` because cosign needs it to obtain a Fulcio certificate; the guard job opts out with `permissions: {}`, and the rest of each workflow stays at `contents: read`.

**Repository owner action (the real control; cannot be done from code).** Move the `LIVE_REGISTRY_HOST`, `LIVE_REGISTRY_USERNAME` and `LIVE_REGISTRY_PASSWORD` secrets from repository-level secrets into a GitHub Environment configured with required reviewers and a deployment branch/tag rule restricting it to `develop` and `v*` tags. Only after the owner has created that environment, add an `environment:` key naming it to the two live-verify jobs. The code deliberately does not set `environment:` today: GitHub auto-creates an unprotected environment for an unknown name, which would look protected without being so. Until this is done the secrets stay repository-level and nothing in the workflow files protects them from a repository writer. Also prefer a dedicated, least-privilege credential for these workflows over a full admin principal where the verification scripts allow it.

**Other settings to configure on GitHub.** Branch protection on `develop` (required reviews, no direct pushes by untrusted writers) limits what the guard's allowed refs can contain. Tag protection (a ruleset) for `v*` matters for these workflows, since a writer could otherwise push a `v*` tag on an arbitrary commit, and equally for `release.yml`, which publishes a release for any pushed `v*` tag.

## Azure Pipelines starter (`.pipelines/azure-pipelines.yml`)

The checked-in pipeline runs on a self-hosted pool (`dtf-deploy`) and declares `pr: none`, so pull requests never trigger it: a self-hosted agent must not run untrusted PR code. Only pushes to `develop` trigger it. Pull-request and fork-build settings, and approval checks on the pool, are configured in Azure DevOps rather than in this file; keep fork builds disabled for this pipeline.

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

With auth enabled, pushes need an existing project (project enforcement is on by default then, off when anonymous): the first path segment of the image name (`team` above) must have been created by a global admin, and the CI user or robot needs a role on it (a project role, or an exact repository grant). See [users.md](users.md#projects-and-project-roles).

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
