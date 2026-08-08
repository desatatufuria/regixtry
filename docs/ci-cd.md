# CI/CD

Los ejemplos usan el login Docker estándar. Con auth habilitada, el registry implementa el challenge Bearer y `/auth/token`, por lo que no se requiere un endpoint adicional en el pipeline.

## GitHub Actions

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

`docker/build-push-action` puede reemplazar los dos últimos pasos si el runner y el daemon soportan BuildKit:

```yaml
      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: registry.example.com/team/app:${{ github.sha }}
```

## Azure DevOps

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

Crear `REGISTRY_USERNAME` y `REGISTRY_PASSWORD` como variables secretas de la pipeline. El repositorio no contiene una Service Connection específica.

## Bash

```bash
set -euo pipefail
REGISTRY=registry.example.com
IMAGE=team/app
TAG="${GIT_COMMIT:?}"
printf '%s\n' "$REGISTRY_PASSWORD" | docker login "$REGISTRY" -u "$REGISTRY_USERNAME" --password-stdin
docker push "$REGISTRY/$IMAGE:$TAG"
```
