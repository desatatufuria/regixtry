# TUI Reference

Iniciar:

```bash
regixtry tui
```

Para una vista no interactiva:

```bash
regixtry tui -snapshot
```

## Pantallas

La consola muestra repositorios, tags, manifest, blobs y uploads. Con `-api-base-url` puede autenticarse contra el admin API y mostrar usuarios, grants y admin tokens.

| Tecla | Pantalla | Acción |
| --- | --- | --- |
| `q`, `Ctrl+C` | todas | salir |
| `Tab` | inspección | abrir administración |
| `↑`/`k` | listas | mover selección arriba |
| `↓`/`j` | listas | mover selección abajo |
| `Enter` | repositorios/tags | abrir siguiente detalle |
| `Esc`, `Backspace` | detalles | volver |
| `b` | manifest | ver blobs |
| `u` | manifest | ver uploads |
| `d`, `x` | manifest/blobs/uploads | mostrar acción no disponible |
| `l` | admin autenticado | logout |
| `r` | usuarios admin | recargar |
| `e`, `d` | usuarios admin | preparar enable/disable |
| `g` | detalle admin | cargar grants |
| `t` | detalle admin | cargar tokens |

El login admin usa usuario/password en memoria y obtiene un Bearer token por `/auth/token`. La sesión expira cuando vence el token. La TUI no persiste credenciales y no implementa todas las mutaciones administrativas.
