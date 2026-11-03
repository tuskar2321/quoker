# Quoker developer commands. See CLAUDE.md.
# Tools are expected on PATH; override to use another binary:
#   make gen BUF=/path/to/buf
BUF           ?= buf
GOLANGCI_LINT ?= golangci-lint
ENV_FILE      ?= .env.local

.PHONY: gen lint-proto test lint vet run-core run-worker dev

gen: ## Generate Go code from proto into gen/
	$(BUF) lint
	$(BUF) generate

lint-proto:
	$(BUF) lint

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run

run-core: $(ENV_FILE)
	set -a && . ./$(ENV_FILE) && set +a && go run ./cmd/core

run-worker:
	@echo "cmd/worker is not implemented yet" >&2; exit 1

dev:
	@echo "local compose (core + worker + fake Telegram) is not implemented yet" >&2; exit 1

$(ENV_FILE):
	@echo "$(ENV_FILE) not found: cp .env.example $(ENV_FILE)" >&2; exit 1
