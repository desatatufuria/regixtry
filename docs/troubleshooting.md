# Troubleshooting

## The service does not start

**Symptom:** `systemctl` reports a failure.

**Diagnosis:**

```bash
systemctl status regixtry
journalctl -u regixtry -n 100 --no-pager
```

Check the public URL, TLS paths, DSN, permissions, and whether an admin exists when auth is enabled.

## Port already in use

**Symptom:** setup reports that the local bind address is busy.

```bash
ss -ltnp | grep ':5000'
```

Change `-addr` or stop the process using the port.

## `docker login` fails

Confirm that `REGISTRY_PUBLIC_URL` matches the URL Docker is using and that `/auth/token` is reachable. Verify the user is enabled, the password is correct, and an initial admin exists.

```bash
curl -i https://registry.example.com/v2/
curl -u USERNAME:PASSWORD 'https://registry.example.com/auth/token?scope=repository:team/image:pull'
```

## `push` returns `UNAUTHORIZED` or `DENIED`

The token must carry push scope, and the user must hold a writer/admin grant for that repository. Check `GET /admin/v1/users/{id}/grants`.

## `push` returns an invalid digest/manifest

The blob must be completed with the correct SHA-256 digest. A manifest is rejected if it references missing blobs. Review the `POST`, `PATCH`, `PUT` sequence and the `digest` parameter.

## Database unreachable

Check `-db`, `REGISTRY_DATABASE_PATH`, directory permissions, and that the SQLite file is owned by the service user. For auth, validate the DSN and PostgreSQL connectivity.

## TLS fails

The certificate and key must both exist and be provided together. `http` cannot be combined with TLS inputs. For `direct-tls`, use `https://` in `-public-url` and trust the CA from Docker/curl.

## The TUI does not load admin data

Use `-api-base-url` with an absolute HTTP/HTTPS URL and valid credentials. The TUI inspects locally, but unimplemented administrative mutations are shown as unavailable.
