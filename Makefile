BIN_DIR := bin
GO      ?= go
GOFLAGS ?=

SOR    := $(BIN_DIR)/sor
CLI    := $(BIN_DIR)/cli
SAMPLE := $(BIN_DIR)/sample-app

# Runtime knobs (override on the command line: make run-sor RETENTION=1h)
LISTEN      ?= :4318
RETENTION   ?= 3h
MAX_SAMPLES ?= 1000000
ALERTS      ?=
SOR_URL     ?= http://localhost$(LISTEN)
SAMPLE_NS   ?= iam
SAMPLE_SVC  ?= iam-api
SAMPLE_RPS  ?= 20

.PHONY: all build sor cli sample-app test test-race test-one vet fmt tidy check run-sor run-cli run-sample-app clean help

all: build ## Build both binaries (default)

build: sor cli sample-app ## Build sor, cli and sample-app into bin/

sor: ## Build the system of record
	$(GO) build $(GOFLAGS) -o $(SOR) ./cmd/sor

cli: ## Build the CLI client
	$(GO) build $(GOFLAGS) -o $(CLI) ./cmd/cli

sample-app: ## Build the demo metrics/logs emitter
	$(GO) build $(GOFLAGS) -o $(SAMPLE) ./cmd/sample-app

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run all tests with the race detector
	$(GO) test ./... -race

test-one: ## Run a single test: make test-one PKG=internal/domain RUN=TestName
	@test -n "$(PKG)" || { echo "PKG is required"; exit 2; }
	@test -n "$(RUN)" || { echo "RUN is required"; exit 2; }
	$(GO) test ./$(PKG)/ -run '$(RUN)' -v

vet: ## Lint with go vet
	$(GO) vet ./...

fmt: ## Format all Go sources
	gofmt -l -w .

tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

check: fmt vet test-race ## Format, vet, and race-test (pre-commit gate)

run-sor: sor ## Run the system of record
	$(SOR) -listen $(LISTEN) -retention $(RETENTION) -max-samples $(MAX_SAMPLES) $(if $(ALERTS),-alerts $(ALERTS))

run-cli: cli ## Run the CLI against SOR_URL
	$(CLI) -sor $(SOR_URL)

run-sample-app: sample-app ## Emit demo metrics/logs to the SoR (SAMPLE_NS, SAMPLE_SVC, SAMPLE_RPS)
	$(SAMPLE) -endpoint localhost$(LISTEN) -namespace $(SAMPLE_NS) -service $(SAMPLE_SVC) -rps $(SAMPLE_RPS)

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
