GO ?= go
CONFIG ?= config/agent.yml
BINARY ?= dist/fluid-agent-debian

.PHONY: deps dev build build-linux test fmt lint

deps:
	$(GO) mod download
	$(GO) mod tidy

dev:
	@test -f env.secrets || (echo "Missing env.secrets. Create it first." && exit 1)
	@set -a; . ./env.secrets; set +a; $(GO) run ./cmd -config $(CONFIG)

build:
	$(GO) build ./...

build-linux:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

lint:
	@echo "No linter configured for this module yet."
