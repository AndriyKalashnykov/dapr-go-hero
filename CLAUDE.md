# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Educational Go application demonstrating Dapr building blocks (Pub/Sub with routing, Secret Store, State Store, Service Invocation) with four client approaches: custom HTTP, custom gRPC, Go SDK HTTP, and Go SDK gRPC.

## Build, Lint & Test Commands

```bash
make help               # List all available Make targets
make deps               # Install tool dependencies (mise, gosec, golangci-lint) if missing
make clean              # Remove build artifacts
make lint               # Run golangci-lint (includes gocritic via .golangci.yml)
make sec                # Run gosec security scanner (excludes generated proto/ dir)
make test               # Run all tests (go test -race -v ./...)
make build              # Compile both binaries (depends on deps only)
make update             # Update all deps to latest (go get -u ./... && go mod tidy)
make ci                 # Full CI pipeline: deps → lint → sec → test → build → deps-prune-check
make ci-run             # Run GitHub Actions workflow locally via act
make deps-check         # Show Go version and mise tool status
make deps-prune         # Remove unused and redundant dependencies
make deps-prune-check   # Verify no prunable dependencies (CI gate)
make release            # Tag and push a release (runs full build first)
make docker-build       # Build container images (inventory + products)
make docker-lint        # Lint Dockerfiles with hadolint
make kind-up            # Create KinD cluster with MetalLB + Dapr + Dashboard
make kind-down          # Destroy KinD cluster
make k8s-deploy         # Build images, load into KinD, deploy all manifests
make k8s-undeploy       # Remove all K8s manifests
make k8s-status         # Show pod/service status across all namespaces
make e2e                # Run end-to-end tests against running K8s cluster
make e2e-setup          # Full setup: kind-up + k8s-deploy
make e2e-teardown       # Full teardown: k8s-undeploy + kind-down
```

Run a single test:
```bash
go test ./pkg/features/widgets/... -run TestName -v
```

## Running the Application

Prerequisites: `dapr init` (provides Redis), PostgreSQL container, `widgets` table from `tables.sql`.

```bash
# Terminal 1: Products gRPC service
make run-products

# Terminal 2: Inventory service (pick one client mode)
make run-custom-http    # Custom HTTP client (Fiber), app port 3001
make run-custom-grpc    # Custom gRPC client, app port 4001
make run-sdk-http       # Dapr Go SDK HTTP, app port 3002
make run-sdk-grpc       # Dapr Go SDK gRPC, app port 4002

# Terminal 3: Publish events
make send-widget        # → PostgreSQL via widgets service
make send-gadget        # → Redis State Store via gadgets service
make send-thingamajig   # → gRPC Products service
make send-all           # All three

# Query REST API (Fiber on port 3000)
make get-widget         # GET /v1/widgets/widget
make get-gadget         # GET /v1/gadgets/gadget
make get-thingamajig    # GET /v1/products/thingamajig
```

## Architecture

### Two Binaries

- **`cmd/inventory/main.go`** — Main service: wires up all features, starts Fiber HTTP server (port 3000) for public API, plus Dapr callback servers on separate ports for event handling. Uses `oklog/run` for goroutine lifecycle management. Explicit cleanup (no defers before `os.Exit` calls) to satisfy gocritic `exitAfterDefer`.
- **`cmd/products/main.go`** — Standalone gRPC service for product storage (bound to `127.0.0.1:50151`), invoked via Dapr service discovery.

### Feature Packages (`pkg/features/`)

Each feature follows the repository pattern with `interface.go`, `repository/`, and `service/`:

| Feature | Storage | Event Type | Repository Implementation |
|---------|---------|------------|--------------------------|
| **widgets** | PostgreSQL (pgx) | `widget.v1` | SQL with upsert |
| **gadgets** | Redis State Store | `gadget.v1` | Dapr State API |
| **products** | gRPC Products service | `thingamajig.v1` (default route) | Generated gRPC client |

### Dapr Abstraction Layer (`pkg/dapr/`)

Custom wrappers over Dapr's HTTP/gRPC protocols and the Go SDK:
- `client_http.go` / `client_grpc.go` / `client_sdk.go` — Three client implementations for State and Secret stores
- `server_http.go` / `server_grpc.go` — Event handler registration for callbacks (includes `OnBulkTopicEvent` for bulk pub/sub)
- `subscription.go` — Merges subscriptions from all features into a single Dapr response
- `cloudevent.go` — CloudEvent v1.0 envelope decoding

### Event Flow

CloudEvents published to Dapr PubSub → Dapr routes by `event.type` → appropriate callback handler → feature service → repository → storage backend.

### Key Infrastructure

- **Fiber v3** — Public HTTP API router (port 3000), separate from Dapr callback ports
- **pgx v5** — PostgreSQL driver with connection pooling (`pkg/connect/postgres/`)
- **Logging** — `pkg/log/` provides a minimal `logr.LogSink` backed by zap (replaces `go-logr/zapr`). Used via `log.NewLogger(zapLog)` in `cmd/inventory/main.go`
- **Secrets** — PostgreSQL credentials fetched from Dapr Secret Store (`secrets.json` for local dev)
- **Protobuf** — Service definition in `proto/products/products.proto`, generated code committed
- **gRPC** — Uses `grpc.NewClient` (not deprecated `Dial`/`DialContext`) with `insecure.NewCredentials()`

### Port Map

| Port | Purpose |
|------|---------|
| 3000 | Public Fiber HTTP API |
| 3001 | Custom HTTP Dapr callbacks |
| 3002 | SDK HTTP Dapr callbacks |
| 3500 | Dapr sidecar HTTP |
| 4001 | Custom gRPC Dapr callbacks (127.0.0.1 only) |
| 4002 | SDK gRPC Dapr callbacks |
| 50151 | Products gRPC service |

All ports are configurable via environment variables defined in `pkg/config/config.go`.

### Kubernetes Deployment

Services deploy into separate namespaces with per-service RBAC:

| Namespace | Contents |
|-----------|----------|
| `dapr-go-hero` | Infrastructure (Redis, PostgreSQL, Zipkin) |
| `dapr-go-hero-inventory` | Inventory deployment + Dapr components (pubsub, statestore, secretstore, subscription, resiliency, configuration) |
| `dapr-go-hero-products` | Products deployment + Dapr configuration (access control) |

Dapr features demonstrated in K8s manifests (`k8s/dapr/`):
- **Pub/Sub** with content-based routing (`subscription.yaml`)
- **State Store** scoped to inventory (`statestore.yaml`)
- **Secret Store** using K8s native secrets (`secretstore.yaml`)
- **Resiliency** — retries, timeouts, circuit breakers (`resiliency.yaml`)
- **Observability** — Zipkin distributed tracing (`configuration.yaml`)
- **Access Control** — app-level RBAC restricting cross-service calls (`configuration.yaml`)

Container images: `Dockerfile.inventory` and `Dockerfile.products` (multi-stage Alpine + BuildKit cache mounts).

Local cluster: KinD + MetalLB (L2) for LoadBalancer support. `make e2e-setup` creates the full environment.

## CI/CD

GitHub Actions CI workflow (`.github/workflows/ci.yml`) runs on every push to `main`, tags `v*`, pull requests, and `workflow_call`:

| Job | Depends on | Steps |
|-----|-----------|-------|
| **static-check** | — | Checkout, Setup Go, Lint, Security, Tidy check |
| **docker-lint** | — | Checkout, Hadolint on Dockerfiles |
| **build** | static-check | Checkout, Setup Go, Build |
| **test** | static-check | Checkout, Setup Go, Test |
| **e2e** | build, test | KinD cluster, MetalLB, Dapr, build+load images, deploy, run e2e tests |

A separate cleanup workflow (`.github/workflows/cleanup-runs.yml`) removes old workflow runs weekly.

## Dapr Configuration

- `config.yaml` — Runtime config (tracing via Zipkin, mTLS, metrics)
- `components/` — Component definitions: `pubsub.yaml` (Redis), `statestore.yaml` (Redis, scoped to "inventory"), `secrets.yaml` (local file store), `subscription.yaml` (programmatic subscriptions)
- `secrets.json` — Local dev secrets for PostgreSQL connection

## Protobuf

Regenerate gRPC code after modifying `proto/products/products.proto`:
```bash
protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative proto/products/products.proto
```

## Static Analysis

The `make lint` composite gate runs:
- **golangci-lint** (includes gocritic via `.golangci.yml`)
- **mermaid-cli** (Mermaid diagram validation via `minlag/mermaid-cli` Docker image — catches broken diagrams in `README.md`, `CLAUDE.md`, `docs/*.md` before they render as red error boxes on github.com)

`make sec` runs **gosec** (excludes generated `proto/` dir).

## Code Quality Conventions

- **Build gate**: `make ci` runs lint → sec → test → build. All must pass with zero issues.
- **gosec**: Generated protobuf files (`proto/`) are excluded. `#nosec` annotations used sparingly with justification.
- **gocritic**: Integrated into golangci-lint via `.golangci.yml` with all tags enabled. No `defer` before `os.Exit` — use explicit cleanup instead.
- **gRPC APIs**: Use `grpc.NewClient` (not `grpc.Dial`/`DialContext`). Use `insecure.NewCredentials()` (not `grpc.WithInsecure()`).
- **Error returns**: All error returns must be checked (`errcheck`). Use `_ =` for intentionally ignored errors in cleanup paths.
- **Parameter style**: Combine consecutive same-type params (`store, key string` not `store string, key string`).
- **Network binding**: gRPC listeners bind to `127.0.0.1` to avoid gosec G102.

## Upgrade Backlog

Items surfaced by `/upgrade-analysis` that are not immediately actionable. Review on next upgrade cycle.

- [ ] Pin `ubuntu-latest` to `ubuntu-24.04` in CI workflows for fully reproducible builds — currently resolves correctly but will shift when GitHub promotes 26.04
- [ ] Add `DAPR_CLI_VERSION` to Makefile if `dapr-init` or `dapr-run` targets are ever added (latest stable: v1.17.1)
- [ ] Track `pgx/v5` CVEs (GO-2026-4772, GO-2026-4771) — no fix available yet, code not reachable per govulncheck. Upgrade when patched version is released
- [ ] Cross-project: align `golangci-lint` version in `go-todo-web` (v2.1.6) and `k8s-mcp-example` (v2.11.1) to current 2.11.4
- [ ] Update CI workflow to use `jdx/mise-action` instead of `actions/setup-go` (deferred — separate `/ci-workflow` task)

*Last reviewed: 2026-04-12*

## Skills

Use the following skills when working on related files:

| File(s) | Skill |
|---------|-------|
| `Makefile` | `/makefile` |
| `renovate.json` | `/renovate` |
| `README.md` | `/readme` |
| `.github/workflows/*.{yml,yaml}` | `/ci-workflow` |

When spawning subagents, always pass conventions from the respective skill into the agent's prompt.
