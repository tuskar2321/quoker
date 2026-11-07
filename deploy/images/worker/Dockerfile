# Quoker worker image: the Go binary, yt-dlp (zipimport build, run by python3) and ffmpeg.
# Build context: repository root.
#   docker build -f deploy/images/worker/Dockerfile --build-arg VERSION=1.2.0 \
#     --build-arg YTDLP_VERSION=2026.10.26 -t quoker-worker:1.2.0 .
FROM golang:1.26.10-bookworm AS build
ARG VERSION=dev
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/worker ./cmd/worker

FROM debian:12.15-slim AS ytdlp
ARG YTDLP_VERSION=2026.10.26
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /dl
RUN base="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}" \
 && curl -fsSL --retry 3 -o yt-dlp "${base}/yt-dlp" \
 && curl -fsSL --retry 3 -o SHA2-256SUMS "${base}/SHA2-256SUMS" \
 && grep -E ' \*?yt-dlp$' SHA2-256SUMS | sha256sum --check - \
 && chmod 0755 yt-dlp

FROM debian:12.15-slim
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 10001 quoker \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin quoker \
 && mkdir -p /tmp/quoker \
 && chown 10001:10001 /tmp/quoker
COPY --from=ytdlp /dl/yt-dlp /usr/local/bin/yt-dlp
COPY --from=build /out/worker /usr/local/bin/worker
ENV TMP_DIR=/tmp/quoker YTDLP_BIN=/usr/local/bin/yt-dlp HEALTH_ADDR=127.0.0.1:8090
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/worker"]
