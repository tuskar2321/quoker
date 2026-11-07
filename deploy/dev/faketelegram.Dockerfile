# Fake Telegram Bot API for the local stand (`make dev`). Build context: repository root.
#   docker build -f deploy/dev/faketelegram.Dockerfile -t quoker-faketelegram .
FROM golang:1.26.10-bookworm AS build
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/faketelegram ./cmd/faketelegram
COPY internal ./internal
RUN go build -trimpath -ldflags "-s -w" -o /out/faketelegram ./cmd/faketelegram

# Dev only. Runs as root on purpose: it binds 443 inside the container and writes
# the CA volume. The distroless base ships the system CA bundle the fake prepends.
# distroless publishes no version tags; dev-only image.
# hadolint ignore=DL3007
FROM gcr.io/distroless/static-debian12:latest
COPY --from=build /out/faketelegram /usr/local/bin/faketelegram
ENTRYPOINT ["/usr/local/bin/faketelegram"]
