BIN_DIR := bin
GO      ?= go
GOFLAGS ?=

SOR    := $(BIN_DIR)/sor
CLI    := $(BIN_DIR)/cli
SAMPLE := $(BIN_DIR)/sample-app

# Runtime knobs. Override on the command line (make run-sor RETENTION=1h),
# from the environment (ALERTS=alert.sample-app.conf make run-sor), or
# persistently in an untracked .env file with the same KEY=value lines.
-include .env
LISTEN      ?= :4318
GRPC        ?= :4319
RETENTION   ?= 3h
MAX_SAMPLES ?= 1000000
ALERTS      ?=
SOR_URL     ?= http://localhost$(LISTEN)
SAMPLE_NS   ?= iam
SAMPLE_SVC  ?= iam-api
SAMPLE_RPS  ?= 20
SAMPLE_CONF ?=
WEBUI_PORT  ?= 8080
SOR_GRPC    ?= http://localhost$(GRPC)

.PHONY: all build sor cli sample-app test test-race test-one vet fmt tidy check proto run-sor run-cli run-sample-app webui webui-test run-webui clean help

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

check: fmt vet test-race webui-test ## Format, vet, race-test, and test the web UI (pre-commit gate)

proto: webui ## Regenerate Go and TypeScript from proto/ (needs buf, protoc-gen-go, protoc-gen-go-grpc)
	buf lint && buf generate

run-sor: sor ## Run the system of record
	$(SOR) -listen $(LISTEN) -grpc $(GRPC) -retention $(RETENTION) -max-samples $(MAX_SAMPLES) $(if $(ALERTS),-alerts $(ALERTS))

run-cli: cli ## Run the CLI against SOR_URL
	$(CLI) -sor $(SOR_URL)

run-sample-app: sample-app ## Emit demo metrics/logs to the SoR (SAMPLE_NS, SAMPLE_SVC, SAMPLE_RPS, or SAMPLE_CONF for several services)
	$(SAMPLE) -endpoint localhost$(LISTEN) -namespace $(SAMPLE_NS) -service $(SAMPLE_SVC) -rps $(SAMPLE_RPS) $(if $(SAMPLE_CONF),-config $(SAMPLE_CONF))

webui: webui/node_modules ## Install web UI dependencies

webui/node_modules: webui/package.json
	cd webui && npm install

webui-test: webui ## Typecheck and test the web UI
	cd webui && npm run typecheck && npm test

run-webui: webui ## Run the web dashboard against SOR_GRPC on WEBUI_PORT
	cd webui && PORT=$(WEBUI_PORT) SOR_GRPC=$(SOR_GRPC) npm start

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
