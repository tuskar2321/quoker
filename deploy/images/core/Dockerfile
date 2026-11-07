# Quoker core image. Build context: repository root.
#   docker build -f deploy/images/core/Dockerfile --build-arg VERSION=1.2.0 -t quoker-core:1.2.0 .
FROM golang:1.26.10-bookworm AS build
ARG VERSION=dev
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/core ./cmd/core \
 # Writable directories for uid 10001 (the final image has no shell).
 && mkdir -p /rootfs/data /rootfs/cache /rootfs/releases /rootfs/backups

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/core /usr/local/bin/core
COPY --from=build --chown=10001:10001 /rootfs/ /
ENV DB_PATH=/data/quoker.db CACHE_DIR=/cache RELEASES_DIR=/releases BACKUP_DIR=/backups
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/core"]
