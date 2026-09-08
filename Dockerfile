# syntax=docker/dockerfile:1.7

# runtime-base carries every hardening concern (non-root user, storage
# ownership, volume/port convention, HEALTHCHECK, entrypoint defaults) so the
# release image (binary copied from the GoReleaser build context) and the dev
# image (compiled in-Dockerfile for docker-compose.yml) share one definition
# and cannot drift from each other.
FROM debian:bookworm-slim AS runtime-base

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -o 65532 -g 65532 -m 0755 /var/lib/regixtry

VOLUME ["/var/lib/regixtry"]

EXPOSE 5000

USER 65532:65532

# Exec form needs no shell, so the image ships no curl/wget: `regixtry
# healthcheck` treats HTTP 200 or 401 as healthy (401 covers auth-enabled
# deployments, where /v2/ answers 401 + WWW-Authenticate).
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/regixtry", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/regixtry"]
# -public-url has no built-in default (only REGISTRY_PUBLIC_URL); serve
# refuses to start without one, so it must be set explicitly here for the
# image to become reachable with no command override, per the
# Default-Serve-Entrypoint requirement.
CMD ["serve", "-addr", "0.0.0.0:5000", "-storage-root", "/var/lib/regixtry", "-public-url", "http://127.0.0.1:5000"]

# release is the image GoReleaser publishes: it never compiles Go, it only
# copies the already-built, already-tested binary GoReleaser places at the
# root of this stage's per-architecture build context.
FROM runtime-base AS release

COPY regixtry /usr/local/bin/regixtry

# build compiles the binary for the dev image only. GoReleaser's
# `--target=release` build never reaches this stage.
FROM golang:1.26-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/regixtry ./cmd/regixtry

# dev stays LAST so bare `docker build .` and the unchanged
# docker-compose.yml keep today's implicit-default-target behavior.
FROM runtime-base AS dev

COPY --from=build /out/regixtry /usr/local/bin/regixtry
