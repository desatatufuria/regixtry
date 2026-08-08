# Operación

## Servicio

Para instalaciones gestionadas:

```bash
systemctl status regixtry
systemctl restart regixtry
journalctl -u regixtry -f
```

El servidor registra método, path, status, duración y challenge cuando existe. No hay endpoint de métricas ni health dedicado; la comprobación de readiness usa `/v2/` y acepta `200` o `401`.

## Backup

El estado de registry requiere conservar conjuntamente `metadata.db` y el directorio `content`. PostgreSQL contiene usuarios, grants y tokens cuando auth está habilitada. ⚠️ No se ha podido confirmar a partir del código actual un comando de backup/restore integrado; use herramientas externas de SQLite/PostgreSQL según su política operativa.

## TLS

`local-http` no cifra. `reverse-proxy` delega TLS a un proxy. `direct-tls` usa `-tls-cert-file` y `-tls-key-file`. La URL pública debe coincidir con el esquema y el realm derivado.

## Upgrade y rollback

`upgrade` usa la provenance del setup, ejecuta preflight, soporta `--ref` y `--yes`, y muestra progreso. El rollback de bootstrap elimina artefactos generados y detiene el servicio. Revisar la salida del comando y `journalctl` antes de repetir.

## Limpieza

No hay garbage collector ni política de retención implementada. No borrar manualmente blobs sin correlacionar metadata: los manifests los referencian por digest.
