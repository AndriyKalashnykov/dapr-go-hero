.DEFAULT_GOAL := help
SHELL          := /bin/bash

APP_NAME       := dapr-go-hero
CURRENTTAG     := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
GOFLAGS        := -mod=mod

# mise shim directory MUST come first so recipes resolve mise-managed tools
# (golangci-lint, gitleaks, actionlint, shellcheck, trivy, act, hadolint, kind,
# protoc, protoc-gen-go, protoc-gen-go-grpc, etc.) without needing `mise activate`.
# ~/.local/bin retained for go-installed tools (gosec, govulncheck) and as a
# fallback inside the act runner container.
export PATH := $(HOME)/.local/share/mise/shims:$(HOME)/.local/bin:$(PATH)

# === Dapr Versions (pinned) ===
# All binary tooling (gosec, govulncheck, golangci-lint, gitleaks, actionlint,
# shellcheck, trivy, act, hadolint, kind, plus codegen tools) is pinned in
# .mise.toml. Only Docker image tags and Dapr runtime/component versions
# remain here.
# renovate: datasource=github-releases depName=dapr/dapr
DAPR_RUNTIME_VERSION          := 1.17.6

# === K8s / Docker Versions (pinned) ===
# kindest/node is pinned by digest because Docker tags are mutable. The KinD
# release notes say which kindest/node release goes with which KIND_VERSION;
# bump in lockstep when bumping kind in .mise.toml.
# renovate: datasource=docker depName=kindest/node
KIND_NODE_IMAGE               := kindest/node:v1.35.1@sha256:05d7bcdefbda08b4e038f644c4df690cdac3fba8b06f8289f30e10026720a1ab
# cloud-provider-kind is consumed as a Docker image (host-side LB controller),
# NOT as a CLI on PATH, so it stays Makefile-pinned rather than mise-managed.
# renovate: datasource=github-releases depName=kubernetes-sigs/cloud-provider-kind
CLOUD_PROVIDER_KIND_VERSION   := 0.10.0
# renovate: datasource=docker depName=minlag/mermaid-cli
MERMAID_CLI_VERSION           := 11.12.0
# renovate: datasource=docker depName=plantuml/plantuml
PLANTUML_VERSION              := 1.2026.2
# act runner image — pinned to avoid silently consuming :act-latest.
# renovate: datasource=docker depName=catthehacker/ubuntu versioning=loose
ACT_UBUNTU_VERSION            := act-24.04

# === Docker / K8s Config ===
CLUSTER_NAME   := $(APP_NAME)
REGISTRY       ?= $(APP_NAME)
TAG            ?= dev
NS_INFRA       := dapr-go-hero
NS_INVENTORY   := dapr-go-hero-inventory
NS_PRODUCTS    := dapr-go-hero-products

# === kubectl context safety ===
# Pinning the context per recipe protects against parallel `make` invocations
# from sibling KinD-using projects that overwrite the global current-context
# via `kubectl config use-context`. Failure mode without this is silent
# ("namespaces not found"). Always invoke as $(KUBECTL) in recipes.
KUBECTL        := kubectl --context=kind-$(CLUSTER_NAME)

# === Go Version Management ===
GO_VERSION := $(shell grep -oP '^go \K[0-9.]+' go.mod)

#help: @ List available tasks
help:
	@echo "Usage: make COMMAND"
	@echo "Commands :"
	@grep -E '[a-zA-Z\.\-]+:.*?@ .*$$' $(MAKEFILE_LIST)| tr -d '#' | awk 'BEGIN {FS = ":.*?@ "}; {printf "\033[32m%-25s\033[0m - %s\n", $$1, $$2}'

# =============================================================================
# deps — tool installation (idempotent, no sudo, mise-driven)
# =============================================================================
#
# Strategy: mise owns all binary tools (golangci-lint, gitleaks, actionlint,
# shellcheck, trivy, act, hadolint, kind, protoc, protoc-gen-go,
# protoc-gen-go-grpc, plus Go and Node toolchains). Only gosec and govulncheck
# stay as `go install` because they aren't carried by the mise registry.
#
# In CI (where $CI is set) the curl-bootstrap is skipped — jdx/mise-action
# handles installer bootstrap — but `mise install --yes` MUST still run so any
# tools added to .mise.toml since the last cache-hit are materialized.

#deps: @ Install required tool dependencies (idempotent, mise-driven)
deps:
	@if [ -z "$$CI" ] && ! command -v mise >/dev/null 2>&1; then \
		echo "Installing mise..."; \
		curl -fsSL https://mise.run | sh; \
		echo ""; \
		echo "mise installed. Activate it in your shell:"; \
		echo '  echo '\''eval "$$(mise activate)"'\'' >> ~/.bashrc'; \
		echo "Then re-run 'make deps'."; \
		exit 0; \
	fi
	@command -v mise >/dev/null 2>&1 || { echo "Error: mise is required. Install via https://mise.jdx.dev"; exit 1; }
	@mise install --yes

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
		go build -a -o ./cmd/inventory/main ./cmd/inventory
	@export GOFLAGS=$(GOFLAGS) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 && \
		go build -a -o ./cmd/products/products ./cmd/products

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
secrets: deps
	@gitleaks detect --source . --verbose --redact --no-banner

#trivy-fs: @ Scan filesystem for vulnerabilities, secrets, and misconfigurations
trivy-fs: deps
	@trivy fs --scanners vuln,secret,misconfig --severity CRITICAL,HIGH --exit-code 1 .

#trivy-config: @ Scan K8s manifests for security misconfigurations (KSV-*)
trivy-config: deps
	@trivy config --severity CRITICAL,HIGH --exit-code 1 k8s/

#lint-ci: @ Lint GitHub Actions workflows (actionlint + shellcheck)
lint-ci: deps
	@actionlint

#mermaid-lint: @ Validate Mermaid diagrams in markdown files (Docker-based, portable)
mermaid-lint:
	@command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required for mermaid-lint"; exit 1; }
	@# Pre-pull with retry so a Docker Hub flake doesn't masquerade as a parse error
	@set -o pipefail; \
	for attempt in 1 2 3; do \
		if docker pull -q minlag/mermaid-cli:$(MERMAID_CLI_VERSION) >/dev/null 2>&1; then \
			break; \
		fi; \
		[ "$$attempt" = 3 ] && { echo "ERROR: docker pull minlag/mermaid-cli:$(MERMAID_CLI_VERSION) failed after 3 attempts"; exit 1; }; \
		sleep $$((attempt * 2)); \
	done
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
		if docker run --rm -v "$$PWD:/data:ro" \
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
ci-run: deps
	@docker container prune -f 2>/dev/null || true
	@ACT_PORT=$$(shuf -i 40000-59999 -n 1); \
	ARTIFACT_PATH=$$(mktemp -d -t act-artifacts.XXXXXX); \
	for j in changes static-check build test integration-test; do \
		echo "==== act push --job $$j ===="; \
		act push --job $$j \
			--pull=false \
			-P ubuntu-24.04=catthehacker/ubuntu:$(ACT_UBUNTU_VERSION) \
			--container-architecture linux/amd64 \
			--artifact-server-port "$$ACT_PORT" \
			--artifact-server-path "$$ARTIFACT_PATH" || exit 1; \
	done

#ci-run-tag: @ Exercise the tag-gated docker job under act (cosign + push will fail without OIDC + GHCR auth — expected)
ci-run-tag: deps
	@docker container prune -f 2>/dev/null || true
	@TAG="$$(git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0-test)"; \
		EVT=$$(mktemp -t act-tag-event.XXXXXX.json); \
		printf '{"ref":"refs/tags/%s","ref_type":"tag"}' "$$TAG" > "$$EVT"; \
		echo "==== act push --job docker (tag=$$TAG, expect failure at GHCR push / cosign — no creds under act) ===="; \
		act push --job docker \
			--pull=false \
			--eventpath "$$EVT" \
			-P ubuntu-24.04=catthehacker/ubuntu:$(ACT_UBUNTU_VERSION) \
			--container-architecture linux/amd64 || true; \
		rm -f "$$EVT"

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
generate-env: deps
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
docker-lint: deps
	@hadolint Dockerfile.inventory Dockerfile.products

# Services covered by the docker-smoke-test target. Override per-CI-runner
# in the workflow matrix: `SERVICES=inventory make docker-smoke-test`.
SERVICES ?= inventory products

#docker-smoke-test: @ Boot each image briefly and assert the binary stays up (Gate 3 of /harden-image-pipeline)
# Both binaries depend on Dapr+Postgres+Redis to do anything useful, so a
# health-endpoint smoke test isn't viable. The boot-marker pattern works
# instead: each binary fails fast with a recognizable log line if its
# expected env is missing, OR keeps a connection retry loop alive
# (logged "Retrying Dapr client connection..."). Either case proves the
# Go runtime started, the binary's USER 10001 has read/exec permission on
# the entrypoint, libc deps resolve, and the static binary's ENTRYPOINT
# is correctly wired. We assert log activity within 10s and a still-alive
# container after 12s — this catches the regression class the gate exists
# to catch (image-build broke the binary, missing libc, USER perm error).
#
# Override SERVICES in CI matrix: `SERVICES=inventory make docker-smoke-test`.
# Override REGISTRY/TAG to point at differently-tagged images.
docker-smoke-test:
	@command -v docker >/dev/null 2>&1 || { echo "ERROR: docker required"; exit 1; }
	@FAIL=0; \
	for svc in $(SERVICES); do \
		echo "=== Smoke: $${svc} ==="; \
		docker rm -f $${svc}-smoke >/dev/null 2>&1 || true; \
		docker run -d --name=$${svc}-smoke "$(REGISTRY)/$${svc}:$(TAG)" >/dev/null; \
		end=$$(( $$(date +%s) + 10 )); \
		seen=0; \
		while [ $$(date +%s) -lt $$end ]; do \
			if [ "$$(docker logs $${svc}-smoke 2>&1 | wc -c)" -gt 0 ]; then seen=1; break; fi; \
			sleep 1; \
		done; \
		sleep 2; \
		if [ "$$seen" = "0" ]; then \
			echo "  FAIL: $${svc} produced no log output within 10s"; \
			FAIL=1; \
		elif [ -z "$$(docker ps -q -f name=$${svc}-smoke)" ]; then \
			echo "  FAIL: $${svc} container exited before 12s — startup crash"; \
			docker logs $${svc}-smoke 2>&1 | sed 's/^/    /'; \
			FAIL=1; \
		else \
			echo "  PASS: $${svc} booted (logging) and stayed up"; \
		fi; \
		docker rm -f $${svc}-smoke >/dev/null 2>&1 || true; \
	done; \
	exit $$FAIL

# =============================================================================
# KinD + Kubernetes
# =============================================================================
#
# Naming convention (matches /makefile skill kind-up/down alias convention):
#
#   kind-create     — create cluster + install LoadBalancer controller + install Dapr
#   kind-deploy     — kind-create + k8s-deploy (full stack up)
#   kind-destroy    — delete cluster + stop cloud-provider-kind container + prune
#                     orphan kindccm-* sidecars
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
kind-create: deps
	@command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl not installed. See: https://kubernetes.io/docs/tasks/tools/"; exit 1; }
	@command -v dapr >/dev/null 2>&1 || { echo "Error: dapr CLI not installed. See https://docs.dapr.io/getting-started/install-dapr-cli/"; exit 1; }
	@if kind get clusters 2>/dev/null | grep -q "^$(CLUSTER_NAME)$$"; then \
		echo "KinD cluster '$(CLUSTER_NAME)' already exists — switching context..."; \
		kubectl config use-context kind-$(CLUSTER_NAME); \
	else \
		kind create cluster --config=kind-config.yaml --name $(CLUSTER_NAME) \
			--image=$(KIND_NODE_IMAGE) --wait 60s; \
	fi
	@$(MAKE) --no-print-directory kind-bootstrap

#kind-bootstrap: @ Install cloud-provider-kind LoadBalancer + Dapr into existing KinD cluster (used by CI after helm/kind-action)
kind-bootstrap:
	@command -v dapr >/dev/null 2>&1 || { echo "Error: dapr CLI not installed. See https://docs.dapr.io/getting-started/install-dapr-cli/"; exit 1; }
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

#kind-destroy: @ Delete KinD cluster, stop cloud-provider-kind, prune kindccm orphans
kind-destroy:
	@docker rm -f cloud-provider-kind 2>/dev/null || true
	@# Prune orphan kindccm-<hash> Envoy sidecars cloud-provider-kind spawns per
	@# LoadBalancer Service. They survive `kind delete cluster` and silently
	@# inherit kind-network IPs into the next run, producing first-curl resets.
	@ORPHANS=$$(docker ps -aq --filter name=kindccm- 2>/dev/null); \
	if [ -n "$$ORPHANS" ]; then \
		echo "=== Pruning kindccm-* orphan sidecars ==="; \
		docker rm -f $$ORPHANS >/dev/null 2>&1 || true; \
	fi
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
	@$(KUBECTL) apply -f k8s/namespace.yaml
	@$(KUBECTL) apply -f k8s/rbac.yaml
	@$(KUBECTL) apply -f k8s/redis.yaml
	@$(KUBECTL) apply -f k8s/postgres.yaml
	@$(KUBECTL) apply -f k8s/zipkin.yaml
	@$(KUBECTL) apply -f k8s/dapr/
	@$(KUBECTL) apply -f k8s/inventory-deployment.yaml
	@$(KUBECTL) apply -f k8s/inventory-service.yaml
	@$(KUBECTL) apply -f k8s/products-deployment.yaml
	@$(KUBECTL) apply -f k8s/products-service.yaml
	@echo "=== Waiting for pods ==="
	@$(KUBECTL) rollout status deployment/redis -n $(NS_INFRA) --timeout=120s
	@$(KUBECTL) rollout status deployment/postgres -n $(NS_INFRA) --timeout=120s
	@$(KUBECTL) rollout status deployment/inventory -n $(NS_INVENTORY) --timeout=180s
	@$(KUBECTL) rollout status deployment/products -n $(NS_PRODUCTS) --timeout=180s

#k8s-undeploy: @ Remove all K8s manifests
k8s-undeploy:
	@$(KUBECTL) delete -f k8s/products-service.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/products-deployment.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/inventory-service.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/inventory-deployment.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/dapr/ --ignore-not-found
	@$(KUBECTL) delete -f k8s/zipkin.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/postgres.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/redis.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/rbac.yaml --ignore-not-found
	@$(KUBECTL) delete -f k8s/namespace.yaml --ignore-not-found

#k8s-status: @ Show pod and service status across all namespaces
k8s-status:
	@echo "=== Infrastructure ($(NS_INFRA)) ==="
	@$(KUBECTL) get pods,svc -n $(NS_INFRA) 2>/dev/null || true
	@echo "=== Inventory ($(NS_INVENTORY)) ==="
	@$(KUBECTL) get pods,svc -n $(NS_INVENTORY) 2>/dev/null || true
	@echo "=== Products ($(NS_PRODUCTS)) ==="
	@$(KUBECTL) get pods,svc -n $(NS_PRODUCTS) 2>/dev/null || true

# =============================================================================
# E2E
# =============================================================================

#e2e: @ Run end-to-end tests against running K8s cluster (default: sdk-http mode)
e2e:
	@KIND_CLUSTER_NAME=$(CLUSTER_NAME) ./tests/e2e.sh

#e2e-all: @ Run end-to-end tests against all 4 client modes (sdk-http, sdk-grpc, custom-http, custom-grpc)
e2e-all:
	@KIND_CLUSTER_NAME=$(CLUSTER_NAME) ./tests/e2e.sh all

#e2e-setup: @ Alias for kind-deploy (create cluster + deploy everything)
e2e-setup: kind-deploy

#e2e-teardown: @ Alias for kind-destroy with prior k8s-undeploy (full teardown)
e2e-teardown: k8s-undeploy kind-destroy

.PHONY: help \
	deps deps-check deps-prune deps-prune-check \
	clean format build run test integration-test update \
	lint sec vulncheck secrets trivy-fs trivy-config lint-ci mermaid-lint \
	diagrams diagrams-clean diagrams-check \
	static-check ci ci-run ci-run-tag release \
	dapr-test run-custom-http run-custom-grpc run-sdk-http run-sdk-grpc run-products \
	send-widget send-gadget send-thingamajig send-all \
	get-widget get-gadget get-thingamajig get-all \
	renovate-bootstrap renovate-validate \
	generate-env proto-gen \
	docker-build docker-push docker-lint docker-smoke-test \
	kind-create kind-bootstrap kind-destroy kind-deploy kind-up kind-down \
	k8s-deploy k8s-undeploy k8s-status \
	e2e e2e-all e2e-setup e2e-teardown
