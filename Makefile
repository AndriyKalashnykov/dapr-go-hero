.DEFAULT_GOAL := help

APP_NAME       := dapr-go-hero
CURRENTTAG     := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GOFLAGS        := -mod=mod

# === Tool Versions (pinned) ===
GOSEC_VERSION         := 2.25.0
GOLANGCI_LINT_VERSION := 2.11.4
ACT_VERSION           := 0.2.87
NVM_VERSION           := 0.40.4
NODE_VERSION          := 24
MISE_VERSION          := 2026.4.10

# === Go Version Management ===
GO_VERSION := $(shell grep -oP '^go \K[0-9.]+' go.mod)

# Helper: run a command under the correct Go version.
# mise (preferred) auto-activates via shell hook; actions/setup-go handles CI.
# Legacy gvm fallback: only if gvm is activated ($GVM_ROOT set), not CI, and script exists.
HAS_GVM := $(shell [ -z "$$CI" ] && [ -n "$$GVM_ROOT" ] && [ -s "$$GVM_ROOT/scripts/gvm" ] && echo true || echo false)
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
	@# Install mise if not present (local development only, CI uses actions/setup-go)
	@if [ -z "$$CI" ] && ! command -v mise >/dev/null 2>&1; then \
		echo "Installing mise v$(MISE_VERSION)..."; \
		curl -fsSL https://mise.jdx.dev/install.sh | MISE_VERSION=v$(MISE_VERSION) bash; \
		echo ""; \
		echo "mise installed. Activate it in your shell:"; \
		echo '  echo '\''eval "$$(mise activate)"'\'' >> ~/.bashrc'; \
		echo "Then re-run 'make deps'."; \
		exit 0; \
	fi
	@if [ -z "$$CI" ] && command -v mise >/dev/null 2>&1; then \
		mise install --yes; \
	else \
		command -v go >/dev/null 2>&1 || { echo "Error: Go required. Install mise (https://mise.jdx.dev) or Go (https://go.dev/dl/)"; exit 1; }; \
	fi
	@$(call go-exec,command -v gosec) >/dev/null 2>&1 || { echo "Installing gosec v$(GOSEC_VERSION)..."; \
		$(call go-exec,go install github.com/securego/gosec/v2/cmd/gosec@v$(GOSEC_VERSION)); }
	@$(call go-exec,command -v golangci-lint) >/dev/null 2>&1 || { echo "Installing golangci-lint v$(GOLANGCI_LINT_VERSION)..."; \
		$(call go-exec,go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)); }


#deps-act: @ Install act for running GitHub Actions locally
deps-act: deps
	@command -v act >/dev/null 2>&1 || { echo "Installing act $(ACT_VERSION)..."; \
		curl -sSfL https://raw.githubusercontent.com/nektos/act/v$(ACT_VERSION)/install.sh | sudo bash -s -- -b /usr/local/bin v$(ACT_VERSION); \
	}

#deps-check: @ Show Go version and mise tool status
deps-check:
	@echo "Go version (go.mod): $(GO_VERSION)"
	@command -v mise >/dev/null 2>&1 && { \
		mise list; \
	} || echo "mise not installed — run 'make deps' to install"

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

#lint: @ Run golangci-lint (includes gocritic via .golangci.yml)
lint: deps
	@$(call go-exec,golangci-lint run ./...)

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
run: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#update: @ Update dependencies to latest versions
update: deps
	@$(call go-exec,export GOFLAGS=$(GOFLAGS) && go get -u ./... && go mod tidy)

#ci: @ Run full local CI pipeline
ci: deps lint sec test build deps-prune-check
	@echo "Local CI pipeline passed."

#ci-run: @ Run GitHub Actions workflow locally using act
ci-run: deps-act
	@act push --container-architecture linux/amd64 \
		--artifact-server-path /tmp/act-artifacts

#release: @ Create and push a new tag
release: build
	@bash -c 'read -p "New tag (current: $(CURRENTTAG)): " newtag && \
		echo "$$newtag" | grep -qE "^v[0-9]+\.[0-9]+\.[0-9]+$$" || { echo "Error: Tag must match vN.N.N (got: $$newtag)"; exit 1; } && \
		echo -n "Create and push $$newtag? [y/N] " && read ans && [ "$${ans:-N}" = y ] && \
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
run-custom-http: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3001 --dapr-http-port 3500 -- go run cmd/inventory/main.go http

#run-custom-grpc: @ Run inventory with custom gRPC client
run-custom-grpc: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4001 --dapr-http-port 3500 -- go run cmd/inventory/main.go grpc

#run-sdk-http: @ Run inventory with Go SDK HTTP client
run-sdk-http: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-sdk-grpc: @ Run inventory with Go SDK gRPC client
run-sdk-grpc: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol grpc --app-port 4002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#run-products: @ Run Products gRPC service
run-products: deps
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

#renovate-bootstrap: @ Install nvm and npm for Renovate
renovate-bootstrap:
	@command -v node >/dev/null 2>&1 || { \
		echo "Installing nvm $(NVM_VERSION) + Node $(NODE_VERSION)..."; \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v$(NVM_VERSION)/install.sh | bash; \
		export NVM_DIR="$$HOME/.nvm"; \
		[ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh"; \
		nvm install $(NODE_VERSION); \
	}

#renovate-validate: @ Validate Renovate configuration
renovate-validate: renovate-bootstrap
	@npx --yes renovate --platform=local

.PHONY: help deps deps-act deps-check deps-prune deps-prune-check \
	clean lint sec test build run update ci ci-run release \
	dapr-test run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc run-products \
	send-widget send-gadget send-thingamajig send-all \
	get-widget get-gadget get-thingamajig get-all \
	renovate-bootstrap renovate-validate
