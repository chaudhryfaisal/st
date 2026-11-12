# syntax=docker/dockerfile:1

# ---- frontend: build the Vue SPA into static/dist ----
# Runs on the builder's own architecture; the output is platform independent.
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN --mount=type=cache,target=/root/.npm npm --prefix frontend ci
COPY frontend ./frontend
# vite outputs to ../static/dist (see frontend/vite config) -> /src/static/dist
RUN npm --prefix frontend run build

# ---- build: cross-compile the single binary (embeds static/dist) ----
# Compiles natively on the builder and cross-compiles for TARGETARCH, so
# multi-arch images don't need QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETOS
ARG TARGETARCH
# release tag; falls back to the git description, then "docker"
ARG VERSION=
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# overwrite the committed embed placeholder with the freshly built SPA
COPY --from=frontend /src/static/dist ./static/dist
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION:-$(git describe --tags --always 2>/dev/null || echo docker)}" \
      -o /tmp/cloud-torrent

# ---- run: minimal scratch image ----
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /tmp/cloud-torrent /cloud-torrent
# CWD is "/" so the default ./downloads resolves to /downloads (the volume);
# the config lives in the volume too, so settings persist across restarts.
EXPOSE 3000
VOLUME ["/downloads"]
ENTRYPOINT ["/cloud-torrent"]
CMD ["--port", "3000", "--config-path", "/downloads/cloud-torrent.json"]
