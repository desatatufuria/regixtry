# syntax=docker/dockerfile:1.7

FROM golang:1.26-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/regixtry ./cmd/regixtry

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/regixtry /usr/local/bin/regixtry

EXPOSE 5000

ENTRYPOINT ["/usr/local/bin/regixtry"]
CMD ["serve", "-addr", "0.0.0.0:5000", "-storage-root", "/var/lib/regixtry"]
