# syntax=docker/dockerfile:1
############################
# STEP 1 build executable binary (runs natively on the builder's arch and
# cross-compiles, so multi-arch builds don't need QEMU emulation)
############################
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TAG=dev
ENV CGO_ENABLED=0
RUN apk add --no-cache ca-certificates
WORKDIR /src

# the go.mod has a local `replace` for ./scraper, so it must be present
# before `go mod download`
COPY go.mod go.sum ./
COPY scraper/go.mod ./scraper/
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.VERSION=${TAG}" -o /usr/local/bin/cloud-torrent
############################
# STEP 2 build a small image
############################
FROM alpine
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/local/bin/cloud-torrent /usr/local/bin/cloud-torrent
ENTRYPOINT ["cloud-torrent"]
