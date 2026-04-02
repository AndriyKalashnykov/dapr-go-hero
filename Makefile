.DEFAULT_GOAL := help

APP_NAME       := dapr-go-hero
CURRENTTAG     := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
NEWTAG         ?= $(shell bash -c 'read -p "Please provide a new tag (current tag - $(CURRENTTAG)): " newtag; echo $$newtag')
GOFLAGS        := -mod=mod

# === Tool Versions (pinned) ===
GOSEC_VERSION         := v2.25.0
GOCRITIC_VERSION      := v0.14.3
GOLANGCI_LINT_VERSION := v2.11.1
ACT_VERSION           := 0.2.86
NVM_VERSION           := 0.40.4
GVM_SHA               := dd652539fa4b771840846f8319fad303c7d0a8d2 # v1.0.22

# === Go Version Management ===
GO_VERSIONS := $(shell find . -name 'go.mod' -exec grep -oP '^go \K[0-9.]+' {} \; | sort -uV)
GO_VERSION  := $(shell grep -oP '^go \K[0-9.]+' go.mod)

# Helper: run a command under the correct Go version via gvm (local) or directly (CI)
HAS_GVM := $(shell [ -s "$$HOME/.gvm/scripts/gvm" ] && echo true || echo false)
define go-exec
$(if $(filter true,$(HAS_GVM)),bash -c '. $$GVM_ROOT/scripts/gvm && gvm use go$(GO_VERSION) >/dev/null && $(1)',bash -c '$(1)')
endef

#help: @ List available tasks
help:
	@echo "Usage: make COMMAND"
	@echo "Commands :"
	@grep -E '[a-zA-Z\.\-]+:.*?@ .*$$' $(MAKEFILE_LIST)| tr -d '#' | awk 'BEGIN {FS = ":.*?@ "}; {printf "\033[32m%-25s\033[0m - %s\n", $$1, $$2}'

#deps: @ Install required tool dependencies (idempotent)
deps:
	@# Install gvm if not present (local development only, CI uses actions/setup-go)
	@if [ -z "$$CI" ] && [ ! -s "$$HOME/.gvm/scripts/gvm" ]; then \
		echo "Installing gvm (Go Version Manager)..."; \
		curl -s -S -L https://raw.githubusercontent.com/moovweb/gvm/$(GVM_SHA)/binscripts/gvm-installer | bash -s $(GVM_SHA); \
		echo ""; \
		echo "gvm installed. Please restart your shell or run:"; \
		echo "  source $$HOME/.gvm/scripts/gvm"; \
		echo "Then re-run 'make deps' to install Go $(GO_VERSION) via gvm."; \
		exit 0; \
	fi
	@if [ "$(HAS_GVM)" = "true" ]; then \
		for v in $(GO_VERSIONS); do \
			bash -c '. $$GVM_ROOT/scripts/gvm && gvm list' 2>/dev/null | grep -q "go$$v" || { \
				echo "Installing Go $$v via gvm..."; \
				bash -c '. $$GVM_ROOT/scripts/gvm && gvm install go'"$$v"' -B'; \
			}; \
		done; \
	else \
		command -v go >/dev/null 2>&1 || { echo "Error: Go required. Install gvm from https://github.com/moovweb/gvm or Go from https://go.dev/dl/"; exit 1; }; \
	fi
	@$(call go-exec,command -v gosec) >/dev/null 2>&1 || { echo "Installing gosec $(GOSEC_VERSION)..."; \
		$(call go-exec,go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)); }
	@$(call go-exec,command -v golangci-lint) >/dev/null 2>&1 || { echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		$(call go-exec,go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)); }
	@$(call go-exec,command -v gocritic) >/dev/null 2>&1 || { echo "Installing gocritic $(GOCRITIC_VERSION)..."; \
		$(call go-exec,go install -v github.com/go-critic/go-critic/cmd/gocritic@$(GOCRITIC_VERSION)); }

#deps-act: @ Install act for running GitHub Actions locally
deps-act: deps
	@command -v act >/dev/null 2>&1 || { echo "Installing act $(ACT_VERSION)..."; \
		curl -sSfL https://raw.githubusercontent.com/nektos/act/v$(ACT_VERSION)/install.sh | sudo bash -s -- -b /usr/local/bin v$(ACT_VERSION); \
	}

#deps-check: @ Show required Go versions and gvm status
deps-check:
	@echo "Go versions required: $(GO_VERSIONS)"
	@echo "Primary Go version:   $(GO_VERSION)"
	@command -v gvm >/dev/null 2>&1 && { \
		bash -c '. $$GVM_ROOT/scripts/gvm && gvm list'; \
	} || echo "gvm not installed — install from https://github.com/moovweb/gvm"

#deps-prune: @ Remove unused and redundant dependencies
deps-prune:
	@echo "=== Dependency Pruning ==="
	@if [ -f go.mod ]; then \
		echo "--- Go: running go mod tidy ---"; \
		$(call go-exec,go mod tidy); \
	fi
	@echo "=== Pruning complete ==="

#deps-prune-check: @ Verify no prunable dependencies (CI gate)
deps-prune-check:
	@FOUND=0; \
	if [ -f go.mod ]; then \
		$(call go-exec,go mod tidy); \
		if ! git diff --exit-code go.mod go.sum >/dev/null 2>&1; then \
			echo "ERROR: go.mod/go.sum not tidy. Run 'go mod tidy'."; FOUND=1; git checkout go.mod go.sum; \
		fi; \
	fi; \
	if [ $$FOUND -ne 0 ]; then exit 1; fi; \
	echo "No prunable dependencies found."

#clean: @ Remove build artifacts
clean:
	@rm -f ./cmd/inventory/main ./cmd/products/products
	@echo "Build artifacts removed."

#lint: @ Run golangci-lint
lint: deps
	@$(call go-exec,golangci-lint run ./...)

#critic: @ Run gocritic with all checks enabled
critic: deps
	@$(call go-exec,gocritic check -enableAll ./...)

#sec: @ Run gosec security scanner
sec: deps
	@$(call go-exec,gosec -exclude-dir=proto ./...)

#test: @ Run tests
test: deps
	@$(call go-exec,export GOFLAGS=$(GOFLAGS) && go test -race -v ./...)

#build: @ Build inventory and products binaries
build: deps
	@$(call go-exec,export GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && go build -a -o ./cmd/inventory/main ./cmd/inventory/main.go)
	@$(call go-exec,export GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && go build -a -o ./cmd/products/products ./cmd/products/main.go)

#run: @ Run inventory service with Dapr (default: SDK HTTP mode)
run:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#update: @ Update dependencies to latest versions
update: deps
	@$(call go-exec,export GOFLAGS=$(GOFLAGS) && go get -u ./... && go mod tidy)

#ci: @ Run full local CI pipeline
ci: deps lint critic sec test build
	@echo "Local CI pipeline passed."

#ci-run: @ Run GitHub Actions workflow locally using act
ci-run: deps-act
	@act push --container-architecture linux/amd64 \
		--artifact-server-path /tmp/act-artifacts

#release: @ Create and push a new tag
release: build
	@bash -c 'newtag="$(NEWTAG)" && \
		echo "$$newtag" | grep -qE "^v[0-9]+\.[0-9]+\.[0-9]+$$" || { echo "Error: Tag must match vN.N.N (got: $$newtag)"; exit 1; } && \
		echo -n "Are you sure to create and push $$newtag tag? [y/N] " && read ans && [ "$${ans:-N}" = y ] && \
		git add -A && \
		git commit -a -s -m "Cut $$newtag release" && \
		git tag $$newtag && \
		git push origin $$newtag && \
		git push && \
		echo "Done."'

#dapr-test: @ Run inventory with Dapr sidecar only (no app)
dapr-test:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- sleep 6000

#run-custom-http: @ Run inventory with custom HTTP client
run-custom-http:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- go run cmd/inventory/main.go http

#run-custom-grpc: @ Run inventory with custom gRPC client
run-custom-grpc:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4001 --dapr-http-port 3500 -- go run cmd/inventory/main.go grpc

#run-sdk-http: @ Run inventory with Go SDK HTTP client
run-sdk-http:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-sdk-grpc: @ Run inventory with Go SDK gRPC client
run-sdk-grpc:
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-products: @ Run Products gRPC service
run-products:
	@dapr run --app-id products --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 50151 -- go run cmd/products/main.go

#send-widget: @ Publish widget event to Dapr PubSub
send-widget:
	@cat messages/widget.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/widget.json

#send-gadget: @ Publish gadget event to Dapr PubSub
send-gadget:
	@cat messages/gadget.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/gadget.json

#send-thingamajig: @ Publish thingamajig event to Dapr PubSub
send-thingamajig:
	@cat messages/thingamajig.json | jq
	@curl -s http://localhost:3500/v1.0/publish/pubsub/inventory -H Content-Type:application/cloudevents+json --data @messages/thingamajig.json

#send-all: @ Publish all three event types
send-all: send-widget send-gadget send-thingamajig

#get-widget: @ Fetch widget from REST API
get-widget:
	@curl -s http://localhost:3000/v1/widgets/widget | jq

#get-gadget: @ Fetch gadget from REST API
get-gadget:
	@curl -s http://localhost:3000/v1/gadgets/gadget | jq

#get-thingamajig: @ Fetch product from REST API
get-thingamajig:
	@curl -s http://localhost:3000/v1/products/thingamajig | jq

#get-all: @ Fetch all three items from REST API
get-all: get-widget get-gadget get-thingamajig

# === Renovate ===

#deps-renovate: @ Install nvm and npm for Renovate
deps-renovate:
	@command -v node >/dev/null 2>&1 || { \
		echo "Installing nvm $(NVM_VERSION)..."; \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v$(NVM_VERSION)/install.sh | bash; \
		export NVM_DIR="$$HOME/.nvm"; \
		[ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh"; \
		nvm install --lts; \
	}

#renovate-validate: @ Validate Renovate configuration
renovate-validate: deps-renovate
	@npx --yes renovate --platform=local

.PHONY: help deps deps-act deps-check deps-prune deps-prune-check deps-renovate \
	clean lint critic sec test build run update ci ci-run release \
	dapr-test run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc run-products \
	send-widget send-gadget send-thingamajig send-all \
	get-widget get-gadget get-thingamajig get-all \
	renovate-validate
