.DEFAULT_GOAL := help
SHELL          := /bin/bash

APP_NAME       := dapr-go-hero
CURRENTTAG     := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GOFLAGS        := -mod=mod

# Ensure tools installed to ~/.local/bin (hadolint, act, gitleaks, actionlint,
# shellcheck, trivy, mise) are on PATH for every recipe — needed inside the
# act runner container where this path is not preconfigured.
export PATH := $(HOME)/.local/bin:$(PATH)

# === Tool Versions (pinned) ===
# renovate: datasource=github-releases depName=securego/gosec
GOSEC_VERSION                 := 2.25.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION         := 2.11.4
# renovate: datasource=go depName=golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION           := 1.1.4
# renovate: datasource=github-releases depName=zricethezav/gitleaks
GITLEAKS_VERSION              := 8.30.1
# renovate: datasource=github-releases depName=rhysd/actionlint
ACTIONLINT_VERSION            := 1.7.12
# renovate: datasource=github-releases depName=koalaman/shellcheck
SHELLCHECK_VERSION            := 0.11.0
# renovate: datasource=github-releases depName=aquasecurity/trivy
TRIVY_VERSION                 := 0.69.3
# renovate: datasource=github-releases depName=nektos/act
ACT_VERSION                   := 0.2.87
# renovate: datasource=github-releases depName=jdx/mise
MISE_VERSION                  := 2026.4.14

# === Dapr Versions (pinned) ===
# renovate: datasource=github-releases depName=dapr/dapr
DAPR_RUNTIME_VERSION          := 1.17.4

# Codegen tool versions (protoc, protoc-gen-go, protoc-gen-go-grpc) are pinned in .mise.toml

# === K8s / Docker Versions (pinned) ===
# renovate: datasource=github-releases depName=kubernetes-sigs/kind
KIND_VERSION                  := 0.31.0
# Manual: KinD publishes one `kindest/node:<version>` image per KinD release.
# Track KIND_VERSION's release notes — do NOT bump past what that release ships.
# KinD v0.31.0 ships kindest/node:v1.35.1 only.
KIND_NODE_IMAGE               := v1.35.1
# renovate: datasource=github-releases depName=kubernetes-sigs/cloud-provider-kind
CLOUD_PROVIDER_KIND_VERSION   := 0.10.0
# renovate: datasource=github-releases depName=hadolint/hadolint
HADOLINT_VERSION              := 2.14.0
# renovate: datasource=docker depName=minlag/mermaid-cli
MERMAID_CLI_VERSION           := 11.12.0
# renovate: datasource=docker depName=plantuml/plantuml
PLANTUML_VERSION              := 1.2026.2

# === Docker / K8s Config ===
CLUSTER_NAME   := $(APP_NAME)
REGISTRY       ?= $(APP_NAME)
TAG            ?= dev
NS_INFRA       := dapr-go-hero
NS_INVENTORY   := dapr-go-hero-inventory
NS_PRODUCTS    := dapr-go-hero-products

# === Go Version Management ===
GO_VERSION := $(shell grep -oP '^go \K[0-9.]+' go.mod)

#help: @ List available tasks
help:
	@echo "Usage: make COMMAND"
	@echo "Commands :"
	@grep -E '[a-zA-Z\.\-]+:.*?@ .*$$' $(MAKEFILE_LIST)| tr -d '#' | awk 'BEGIN {FS = ":.*?@ "}; {printf "\033[32m%-25s\033[0m - %s\n", $$1, $$2}'

# =============================================================================
# deps — tool installation (idempotent, no sudo, installs to ~/.local/bin)
# =============================================================================

#deps: @ Install required tool dependencies (idempotent)
deps:
	@# Install mise if not present (local development only, CI uses jdx/mise-action)
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
	@command -v gosec >/dev/null 2>&1 || { echo "Installing gosec v$(GOSEC_VERSION)..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@v$(GOSEC_VERSION); }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint v$(GOLANGCI_LINT_VERSION)..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION); }
	@command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck v$(GOVULNCHECK_VERSION)..."; \
		go install golang.org/x/vuln/cmd/govulncheck@v$(GOVULNCHECK_VERSION); }

#deps-act: @ Install act for running GitHub Actions locally
deps-act: deps
	@command -v act >/dev/null 2>&1 || { echo "Installing act v$(ACT_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sSfL https://raw.githubusercontent.com/nektos/act/v$(ACT_VERSION)/install.sh | \
			bash -s -- -b $$HOME/.local/bin v$(ACT_VERSION); \
	}

#deps-hadolint: @ Install hadolint for Dockerfile linting
deps-hadolint:
	@command -v hadolint >/dev/null 2>&1 || { echo "Installing hadolint v$(HADOLINT_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sSfL -o $$HOME/.local/bin/hadolint "https://github.com/hadolint/hadolint/releases/download/v$(HADOLINT_VERSION)/hadolint-Linux-x86_64"; \
		chmod +x $$HOME/.local/bin/hadolint; \
	}

#deps-gitleaks: @ Install gitleaks for secret scanning
deps-gitleaks:
	@command -v gitleaks >/dev/null 2>&1 || { echo "Installing gitleaks v$(GITLEAKS_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sSfL -o /tmp/gitleaks.tar.gz "https://github.com/zricethezav/gitleaks/releases/download/v$(GITLEAKS_VERSION)/gitleaks_$(GITLEAKS_VERSION)_linux_x64.tar.gz"; \
		tar -xzf /tmp/gitleaks.tar.gz -C /tmp gitleaks; \
		install -m 755 /tmp/gitleaks $$HOME/.local/bin/gitleaks; \
		rm -f /tmp/gitleaks.tar.gz /tmp/gitleaks; \
	}

#deps-actionlint: @ Install actionlint for GitHub Actions workflow linting
deps-actionlint:
	@command -v actionlint >/dev/null 2>&1 || { echo "Installing actionlint v$(ACTIONLINT_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sSfL -o /tmp/actionlint.tar.gz "https://github.com/rhysd/actionlint/releases/download/v$(ACTIONLINT_VERSION)/actionlint_$(ACTIONLINT_VERSION)_linux_amd64.tar.gz"; \
		tar -xzf /tmp/actionlint.tar.gz -C /tmp actionlint; \
		install -m 755 /tmp/actionlint $$HOME/.local/bin/actionlint; \
		rm -f /tmp/actionlint.tar.gz /tmp/actionlint; \
	}

#deps-shellcheck: @ Install shellcheck (used by actionlint for workflow run-step linting)
deps-shellcheck:
	@command -v shellcheck >/dev/null 2>&1 || { echo "Installing shellcheck v$(SHELLCHECK_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sSfL -o /tmp/shellcheck.tar.xz "https://github.com/koalaman/shellcheck/releases/download/v$(SHELLCHECK_VERSION)/shellcheck-v$(SHELLCHECK_VERSION).linux.x86_64.tar.xz"; \
		tar -xJf /tmp/shellcheck.tar.xz -C /tmp; \
		install -m 755 /tmp/shellcheck-v$(SHELLCHECK_VERSION)/shellcheck $$HOME/.local/bin/shellcheck; \
		rm -rf /tmp/shellcheck.tar.xz /tmp/shellcheck-v$(SHELLCHECK_VERSION); \
	}

#deps-trivy: @ Install Trivy for CVE / secret / misconfiguration scanning
deps-trivy:
	@command -v trivy >/dev/null 2>&1 || { echo "Installing trivy v$(TRIVY_VERSION)..."; \
		mkdir -p $$HOME/.local/bin; \
		curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | \
			sh -s -- -b $$HOME/.local/bin v$(TRIVY_VERSION); \
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
		go mod tidy; \
	fi
	@echo "=== Pruning complete ==="

#deps-prune-check: @ Verify no prunable dependencies (CI gate)
deps-prune-check:
	@if [ -f go.mod ]; then \
		go mod tidy; \
		if ! git diff --exit-code go.mod go.sum >/dev/null 2>&1; then \
			echo "ERROR: go.mod/go.sum not tidy. Run 'go mod tidy' and stage the changes:"; \
			git diff --name-status go.mod go.sum; \
			exit 1; \
		fi; \
	fi
	@echo "No prunable dependencies found."

# =============================================================================
# clean, build, run, test, format
# =============================================================================

#clean: @ Remove build artifacts
clean:
	@rm -f ./cmd/inventory/main ./cmd/products/products
	@echo "Build artifacts removed."

#format: @ Auto-format Go source (gofmt + goimports via golangci-lint --fix)
format: deps
	@gofmt -w .
	@golangci-lint run --fix ./... 2>/dev/null || true

#build: @ Build inventory and products binaries
build: deps
	@export GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && \
		go build -a -o ./cmd/inventory/main ./cmd/inventory/main.go
	@export GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && \
		go build -a -o ./cmd/products/products ./cmd/products/main.go

#run: @ Run inventory service with Dapr (default: SDK HTTP mode)
run: deps
	@dapr run --app-id inventory --config ./config.yaml --resources-path ./components --app-protocol http --app-port 3002 --dapr-http-port 3500 -- go run cmd/inventory/main.go

#test: @ Run unit tests (fast, no external deps)
test: deps
	@export GOFLAGS=$(GOFLAGS) && go test -race -v ./...

#integration-test: @ Run integration tests (real PostgreSQL + Dapr-state stub + gRPC server)
integration-test: deps
	@command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required for integration-test"; exit 1; }
	@export GOFLAGS=$(GOFLAGS) && go test -race -tags=integration -v ./...

#update: @ Update dependencies to latest versions
update: deps
	@export GOFLAGS=$(GOFLAGS) && go get -u ./... && go mod tidy

# =============================================================================
# lint / sec / vulncheck / secrets / trivy / lint-ci / mermaid-lint
# Composed into `static-check` below.
# =============================================================================

#lint: @ Run golangci-lint (includes gocritic, gosec via .golangci.yml)
lint: deps
	@golangci-lint run ./...

#sec: @ Run gosec security scanner
sec: deps
	@gosec -exclude-dir=proto ./...

#vulncheck: @ Check for known vulnerabilities in dependencies (govulncheck)
vulncheck: deps
	@govulncheck ./...

#secrets: @ Scan for hardcoded secrets (gitleaks)
secrets: deps-gitleaks
	@gitleaks detect --source . --verbose --redact --no-banner

#trivy-fs: @ Scan filesystem for vulnerabilities, secrets, and misconfigurations
trivy-fs: deps-trivy
	@trivy fs --scanners vuln,secret,misconfig --severity CRITICAL,HIGH --exit-code 1 .

#trivy-config: @ Scan K8s manifests for security misconfigurations (KSV-*)
trivy-config: deps-trivy
	@trivy config --severity CRITICAL,HIGH --exit-code 1 k8s/

#lint-ci: @ Lint GitHub Actions workflows (actionlint + shellcheck)
lint-ci: deps-actionlint deps-shellcheck
	@actionlint

#mermaid-lint: @ Validate Mermaid diagrams in markdown files (Docker-based, portable)
mermaid-lint:
	@command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required for mermaid-lint"; exit 1; }
	@set -euo pipefail; \
	MD_FILES=$$(grep -lF '```mermaid' README.md CLAUDE.md docs/*.md 2>/dev/null || true); \
	if [ -z "$$MD_FILES" ]; then \
		echo "No Mermaid blocks found — skipping."; \
		exit 0; \
	fi; \
	FAILED=0; \
	for md in $$MD_FILES; do \
		echo "Validating Mermaid blocks in $$md..."; \
		LOG=$$(mktemp); \
		if docker run --rm -v "$$PWD:/data" \
			minlag/mermaid-cli:$(MERMAID_CLI_VERSION) \
			-i "/data/$$md" -o "/tmp/$$(basename $$md .md).svg" >"$$LOG" 2>&1; then \
			echo "  ✓ All blocks rendered cleanly."; \
		else \
			echo "  ✗ Parse error in $$md:"; \
			sed 's/^/    /' "$$LOG"; \
			FAILED=$$((FAILED + 1)); \
		fi; \
		rm -f "$$LOG"; \
	done; \
	if [ "$$FAILED" -gt 0 ]; then \
		echo "Mermaid lint: $$FAILED file(s) had parse errors."; \
		exit 1; \
	fi

DIAGRAM_DIR := docs/diagrams
DIAGRAM_SRC := $(wildcard $(DIAGRAM_DIR)/*.puml)
DIAGRAM_OUT := $(patsubst $(DIAGRAM_DIR)/%.puml,$(DIAGRAM_DIR)/out/%.png,$(DIAGRAM_SRC))

#diagrams: @ Render PlantUML architecture diagrams to PNG (pinned Docker image)
diagrams: $(DIAGRAM_OUT)

$(DIAGRAM_DIR)/out/%.png: $(DIAGRAM_DIR)/%.puml
	@command -v docker >/dev/null 2>&1 || { echo "ERROR: docker required for PlantUML render"; exit 1; }
	@mkdir -p $(DIAGRAM_DIR)/out
	@docker run --rm --user "$$(id -u):$$(id -g)" \
		-v "$(CURDIR)/$(DIAGRAM_DIR):/work" -w /work \
		-e HOME=/tmp -e _JAVA_OPTIONS=-Duser.home=/tmp \
		plantuml/plantuml:$(PLANTUML_VERSION) \
		-tpng -o out $(notdir $<)

#diagrams-clean: @ Remove rendered PlantUML artifacts
diagrams-clean:
	@rm -rf $(DIAGRAM_DIR)/out

#diagrams-check: @ Verify rendered diagrams match current .puml sources (CI drift gate)
diagrams-check:
	@if [ -z "$(DIAGRAM_SRC)" ]; then echo "No .puml sources — skipping."; exit 0; fi
	@$(MAKE) --no-print-directory diagrams
	@git diff --exit-code -- $(DIAGRAM_DIR)/out >/dev/null 2>&1 || { \
		echo "ERROR: PlantUML output drift. Run 'make diagrams' and stage $(DIAGRAM_DIR)/out."; \
		git diff --name-status -- $(DIAGRAM_DIR)/out; \
		exit 1; \
	}

#static-check: @ Composite quality gate (lint-ci + lint + sec + vulncheck + secrets + docker-lint + trivy-fs + trivy-config + mermaid-lint + diagrams-check + deps-prune-check)
static-check: deps lint-ci lint sec vulncheck secrets docker-lint trivy-fs trivy-config mermaid-lint diagrams-check deps-prune-check
	@echo "Static check passed."

# =============================================================================
# CI orchestration
# =============================================================================

#ci: @ Run full local CI pipeline (format → static-check → test → integration-test → build)
ci: deps format static-check test integration-test build
	@echo "Local CI pipeline passed."

#ci-run: @ Run GitHub Actions workflow locally using act (jobs serialized, isolated artifact server)
ci-run: deps-act
	@docker container prune -f 2>/dev/null || true
	@ACT_PORT=$$(shuf -i 40000-59999 -n 1); \
	ARTIFACT_PATH=$$(mktemp -d -t act-artifacts.XXXXXX); \
	for j in docker-lint static-check build test integration-test; do \
		echo "==== act push --job $$j ===="; \
		act push --job $$j --container-architecture linux/amd64 \
			--artifact-server-port "$$ACT_PORT" \
			--artifact-server-path "$$ARTIFACT_PATH" || exit 1; \
	done

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

# =============================================================================
# Run modes (local Dapr client comparison)
# =============================================================================

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

# =============================================================================
# Renovate
# =============================================================================

#renovate-bootstrap: @ Install Node via mise (version pinned in .mise.toml) for Renovate
renovate-bootstrap:
	@command -v mise >/dev/null 2>&1 || { \
		echo "Error: mise is required. Run 'make deps' to install it."; exit 1; \
	}
	@mise install node --yes
	@command -v pnpm >/dev/null 2>&1 || corepack enable pnpm 2>/dev/null || true

#renovate-validate: @ Validate Renovate configuration
renovate-validate: renovate-bootstrap
	@npx --yes renovate --platform=local

# =============================================================================
# Code generation
# =============================================================================

#generate-env: @ Regenerate .env from pkg/config defaults
generate-env:
	@go run ./cmd/generate-env

#proto-gen: @ Regenerate gRPC code from .proto files (uses mise-pinned tool versions)
proto-gen: deps
	@command -v mise >/dev/null 2>&1 || { echo "Error: mise required. See https://mise.jdx.dev"; exit 1; }
	@PROTOC_INC="$$(dirname $$(dirname $$(mise which protoc)))/include"; \
		mise exec -- protoc -I"$$PROTOC_INC" -I. \
			--go_out=. --go-grpc_out=. \
			--go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
			proto/products/products.proto
	@echo "Protobuf code regenerated."

# =============================================================================
# Docker
# =============================================================================

#docker-build: @ Build container images for both services
docker-build:
	@DOCKER_BUILDKIT=1 docker build -t $(REGISTRY)/inventory:$(TAG) -f Dockerfile.inventory .
	@DOCKER_BUILDKIT=1 docker build -t $(REGISTRY)/products:$(TAG) -f Dockerfile.products .

#docker-push: @ Push container images to registry
docker-push:
	@docker push $(REGISTRY)/inventory:$(TAG)
	@docker push $(REGISTRY)/products:$(TAG)

#docker-lint: @ Lint Dockerfiles with hadolint
docker-lint: deps-hadolint
	@hadolint Dockerfile.inventory Dockerfile.products

# =============================================================================
# KinD + Kubernetes
# =============================================================================
#
# Naming convention (matches /makefile skill kind-up/down alias convention):
#
#   kind-create     — create cluster + install LoadBalancer controller + install Dapr
#   kind-deploy     — kind-create + k8s-deploy (full stack up)
#   kind-destroy    — delete cluster + stop cloud-provider-kind container
#
#   kind-up         — alias for kind-deploy (docker-compose-style "bring up the stack")
#   kind-down       — alias for kind-destroy (docker-compose-style "tear it down")
#   e2e-setup       — alias for kind-deploy (CI compatibility)
#   e2e-teardown    — alias for kind-destroy (CI compatibility)
#
# LoadBalancer: cloud-provider-kind (kind-team maintained) — replaces MetalLB.
# Runs as a host-side controller (watches Services of type LoadBalancer on the
# 'kind' Docker network and allocates IPs). No in-cluster install, no
# IPAddressPool / L2Advertisement CRDs, no MetalLB release-track drift.

#kind-create: @ Create local KinD cluster with cloud-provider-kind LoadBalancer controller + Dapr
kind-create:
	@command -v kind >/dev/null 2>&1 || { echo "Error: kind not installed. Run: go install sigs.k8s.io/kind@v$(KIND_VERSION)"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl not installed. See: https://kubernetes.io/docs/tasks/tools/"; exit 1; }
	@command -v dapr >/dev/null 2>&1 || { echo "Error: dapr CLI not installed. See https://docs.dapr.io/getting-started/install-dapr-cli/"; exit 1; }
	@if kind get clusters 2>/dev/null | grep -q "^$(CLUSTER_NAME)$$"; then \
		echo "KinD cluster '$(CLUSTER_NAME)' already exists — switching context..."; \
		kubectl config use-context kind-$(CLUSTER_NAME); \
	else \
		kind create cluster --config=kind-config.yaml --name $(CLUSTER_NAME) \
			--image=kindest/node:$(KIND_NODE_IMAGE) --wait 60s; \
	fi
	@echo "=== Installing cloud-provider-kind (host-side LoadBalancer controller) ==="
	@docker rm -f cloud-provider-kind >/dev/null 2>&1 || true
	@docker run --rm -d \
		--name cloud-provider-kind \
		--network kind \
		-v /var/run/docker.sock:/var/run/docker.sock \
		registry.k8s.io/cloud-provider-kind/cloud-controller-manager:v$(CLOUD_PROVIDER_KIND_VERSION) >/dev/null
	@echo "=== Installing Dapr (runtime v$(DAPR_RUNTIME_VERSION)) ==="
	@dapr init -k --runtime-version $(DAPR_RUNTIME_VERSION) --wait --timeout 300 2>/dev/null || true
	@echo "=== Installing Dapr Dashboard ==="
	@dapr dashboard -k -p 0 2>/dev/null &
	@echo "=== KinD cluster ready ==="

#kind-destroy: @ Delete KinD cluster and stop cloud-provider-kind
kind-destroy:
	@docker rm -f cloud-provider-kind 2>/dev/null || true
	@kind delete cluster --name $(CLUSTER_NAME) 2>/dev/null || true

#kind-deploy: @ Create cluster, build/load images, apply manifests (full stack up)
kind-deploy: kind-create k8s-deploy

#kind-up: @ Bring the whole stack up — alias for kind-deploy (docker-compose-style)
kind-up: kind-deploy

#kind-down: @ Tear the whole stack down — alias for kind-destroy (docker-compose-style)
kind-down: kind-destroy

#k8s-deploy: @ Build images, load into KinD, and deploy all manifests
k8s-deploy: docker-build
	@kind load docker-image $(REGISTRY)/inventory:$(TAG) --name $(CLUSTER_NAME)
	@kind load docker-image $(REGISTRY)/products:$(TAG) --name $(CLUSTER_NAME)
	@echo "=== Applying K8s manifests ==="
	@kubectl apply -f k8s/namespace.yaml
	@kubectl apply -f k8s/rbac.yaml
	@kubectl apply -f k8s/redis.yaml
	@kubectl apply -f k8s/postgres.yaml
	@kubectl apply -f k8s/zipkin.yaml
	@kubectl apply -f k8s/dapr/
	@kubectl apply -f k8s/inventory-deployment.yaml
	@kubectl apply -f k8s/inventory-service.yaml
	@kubectl apply -f k8s/products-deployment.yaml
	@kubectl apply -f k8s/products-service.yaml
	@echo "=== Waiting for pods ==="
	@kubectl rollout status deployment/redis -n $(NS_INFRA) --timeout=120s
	@kubectl rollout status deployment/postgres -n $(NS_INFRA) --timeout=120s
	@kubectl rollout status deployment/inventory -n $(NS_INVENTORY) --timeout=180s
	@kubectl rollout status deployment/products -n $(NS_PRODUCTS) --timeout=180s

#k8s-undeploy: @ Remove all K8s manifests
k8s-undeploy:
	@kubectl delete -f k8s/products-service.yaml --ignore-not-found
	@kubectl delete -f k8s/products-deployment.yaml --ignore-not-found
	@kubectl delete -f k8s/inventory-service.yaml --ignore-not-found
	@kubectl delete -f k8s/inventory-deployment.yaml --ignore-not-found
	@kubectl delete -f k8s/dapr/ --ignore-not-found
	@kubectl delete -f k8s/zipkin.yaml --ignore-not-found
	@kubectl delete -f k8s/postgres.yaml --ignore-not-found
	@kubectl delete -f k8s/redis.yaml --ignore-not-found
	@kubectl delete -f k8s/rbac.yaml --ignore-not-found
	@kubectl delete -f k8s/namespace.yaml --ignore-not-found

#k8s-status: @ Show pod and service status across all namespaces
k8s-status:
	@echo "=== Infrastructure ($(NS_INFRA)) ==="
	@kubectl get pods,svc -n $(NS_INFRA) 2>/dev/null || true
	@echo "=== Inventory ($(NS_INVENTORY)) ==="
	@kubectl get pods,svc -n $(NS_INVENTORY) 2>/dev/null || true
	@echo "=== Products ($(NS_PRODUCTS)) ==="
	@kubectl get pods,svc -n $(NS_PRODUCTS) 2>/dev/null || true

# =============================================================================
# E2E
# =============================================================================

#e2e: @ Run end-to-end tests against running K8s cluster (default: sdk-http mode)
e2e:
	@./tests/e2e.sh

#e2e-all: @ Run end-to-end tests against all 4 client modes (sdk-http, sdk-grpc, custom-http, custom-grpc)
e2e-all:
	@./tests/e2e.sh all

#e2e-setup: @ Alias for kind-deploy (create cluster + deploy everything)
e2e-setup: kind-deploy

#e2e-teardown: @ Alias for kind-destroy with prior k8s-undeploy (full teardown)
e2e-teardown: k8s-undeploy kind-destroy

.PHONY: help \
	deps deps-act deps-hadolint deps-gitleaks deps-actionlint deps-shellcheck deps-trivy deps-check deps-prune deps-prune-check \
	clean format build run test integration-test update \
	lint sec vulncheck secrets trivy-fs trivy-config lint-ci mermaid-lint \
	diagrams diagrams-clean diagrams-check \
	static-check ci ci-run release \
	dapr-test run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc run-products \
	send-widget send-gadget send-thingamajig send-all \
	get-widget get-gadget get-thingamajig get-all \
	renovate-bootstrap renovate-validate \
	generate-env proto-gen \
	docker-build docker-push docker-lint \
	kind-create kind-destroy kind-deploy kind-up kind-down \
	k8s-deploy k8s-undeploy k8s-status \
	e2e e2e-all e2e-setup e2e-teardown
