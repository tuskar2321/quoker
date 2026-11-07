# Quoker developer commands. See CLAUDE.md.
# Tools are expected on PATH; override to use another binary:
#   make gen BUF=/path/to/buf
BUF           ?= buf
GOLANGCI_LINT ?= golangci-lint
ENV_FILE      ?= .env.local

# Local stand (make dev, deploy/dev/compose.yml). Dummy identifiers for the stand,
# not secrets. DEV_WORKER_ID is 64 hex characters: in dev the worker sends its id as
# the certificate fingerprint, and the core only accepts 64-hex fingerprints.
DEV_BOT_TOKEN ?= 1000001:DEV-TOKEN-NOT-A-SECRET
DEV_WORKER_ID ?= d6bf8293c3c8229a62e6c749dfae4b722701c3bd6038292abfccfe71438e779e
export DEV_BOT_TOKEN DEV_WORKER_ID
DEV_COMPOSE   := docker compose -f deploy/dev/compose.yml

.PHONY: gen lint-proto build test lint vet run-core run-worker dev dev-logs dev-down dev-reset

gen: ## Generate Go code from proto into gen/
	$(BUF) lint
	$(BUF) generate

lint-proto:
	$(BUF) lint

build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run

run-core: $(ENV_FILE)
	set -a && . ./$(ENV_FILE) && set +a && go run ./cmd/core

run-worker: $(ENV_FILE)
	set -a && . ./$(ENV_FILE) && set +a && go run ./cmd/worker

dev: ## Start core, worker and the fake Telegram API in docker compose, then seed them
	$(DEV_COMPOSE) up -d --build --wait
	sh deploy/dev/seed.sh

dev-logs: ## Follow the logs of the local stand
	$(DEV_COMPOSE) logs -f --tail=100

dev-down: ## Stop the local stand, keep its data
	$(DEV_COMPOSE) down

dev-reset: ## Stop the local stand and delete its data (fresh database on the next make dev)
	$(DEV_COMPOSE) down -v

$(ENV_FILE):
	@echo "$(ENV_FILE) not found: cp .env.example $(ENV_FILE)" >&2; exit 1
