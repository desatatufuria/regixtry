# Desarrollo

## Estructura

- `cmd/regixtry`: entrypoint y parsing CLI.
- `internal/domain`: invariantes de auth y registry.
- `internal/app`: casos de uso.
- `internal/ports`: interfaces.
- `internal/infra`: adapters y lifecycle Linux.
- `internal/protocol/http`: API.
- `internal/tui`: Bubble Tea.

## Build y tests

```bash
```

El Dockerfile usa `CGO_ENABLED=0`, compila `./cmd/regixtry` y produce una imagen Debian slim con `ca-certificates`.

## Añadir un endpoint

El routing se registra en `internal/protocol/http/router.go`. La lógica debe permanecer en `internal/app` y las invariantes en `internal/domain`; el handler solo traduce HTTP, auth y errores. Agregar tests en `internal/protocol/http/` y tests de aplicación/dominio cuando cambie comportamiento.

## Modificar la TUI

`internal/tui/model.go` contiene pantallas, teclas y rendering; `admin_client.go` contiene el cliente HTTP administrativo. La TUI no debe leer storage directamente para decisiones de registry ni fabricar una identidad autenticada.

## Tests y evidencia

Los tests unitarios cubren dominio, stores, router, auth, lifecycle y TUI. Los scripts de evidencia están bajo `docs/verification/scripts/`. La evidencia manual no equivale por sí sola a una garantía de despliegue general.
