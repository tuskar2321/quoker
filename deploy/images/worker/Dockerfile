# Quoker worker image: the Go binary, yt-dlp (official PyInstaller onedir build, which bundles
# Python and curl_cffi for browser impersonation that TikTok requires) and ffmpeg.
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
ARG TARGETARCH
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl unzip \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /dl
# The onedir build (a .zip) runs in place; the single-file build would unpack itself
# into /tmp on every call, and /tmp in the container is a small tmpfs.
RUN case "${TARGETARCH:-amd64}" in \
      amd64) asset=yt-dlp_linux.zip; exe=yt-dlp_linux ;; \
      arm64) asset=yt-dlp_linux_aarch64.zip; exe=yt-dlp_linux_aarch64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && base="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}" \
 && curl -fsSL --retry 3 -o "${asset}" "${base}/${asset}" \
 && curl -fsSL --retry 3 -o SHA2-256SUMS "${base}/SHA2-256SUMS" \
 && sha256sum --check --ignore-missing --strict SHA2-256SUMS \
 && mkdir /opt/yt-dlp \
 && unzip -q "${asset}" -d /opt/yt-dlp \
 && mv "/opt/yt-dlp/${exe}" /opt/yt-dlp/yt-dlp \
 && chmod -R a+rX,go-w /opt/yt-dlp \
 && /opt/yt-dlp/yt-dlp --version

FROM debian:12.15-slim
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 10001 quoker \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin quoker \
 && mkdir -p /tmp/quoker \
 && chown 10001:10001 /tmp/quoker
COPY --from=ytdlp /opt/yt-dlp /opt/yt-dlp
COPY --from=build /out/worker /usr/local/bin/worker
RUN ln -s /opt/yt-dlp/yt-dlp /usr/local/bin/yt-dlp
ENV TMP_DIR=/tmp/quoker YTDLP_BIN=/usr/local/bin/yt-dlp HEALTH_ADDR=127.0.0.1:8090
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/worker"]
