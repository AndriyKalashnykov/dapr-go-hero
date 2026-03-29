# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Educational Go application demonstrating Dapr building blocks (Pub/Sub with routing, Secret Store, State Store, Service Invocation) with four client approaches: custom HTTP, custom gRPC, Go SDK HTTP, and Go SDK gRPC.

## Build, Lint & Test Commands

```bash
make help               # List all available Make targets
make deps               # Install tool dependencies (gosec, golangci-lint, gocritic) if missing
make lint               # Run golangci-lint
make critic             # Run gocritic with all checks enabled
make sec                # Run gosec security scanner (excludes generated proto/ dir)
make test               # Run all tests (go test -v ./...)
make build              # Full pipeline: deps → lint → critic → sec → compile both binaries
make update             # Update all deps to latest (go get -u ./... && go mod tidy)
make release            # Tag and push a release (runs full build first)
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
| 50151 | Products gRPC service (127.0.0.1 only) |

## Dapr Configuration

- `config.yaml` — Runtime config (tracing via Zipkin, PubSub.Routing feature enabled)
- `components/` — Component definitions: `pubsub.yaml` (Redis), `statestore.yaml` (Redis, scoped to "inventory"), `secrets.yaml` (local file store)
- `secrets.json` — Local dev secrets for PostgreSQL connection

## Protobuf

Regenerate gRPC code after modifying `proto/products/products.proto`:
```bash
protoc --go_out=. --go-grpc_out=. proto/products/products.proto
```

## Code Quality Conventions

- **Build gate**: `make build` runs lint → critic → sec → compile. All must pass with zero issues.
- **gosec**: Generated protobuf files (`proto/`) are excluded. `#nosec` annotations used sparingly with justification.
- **gocritic**: All checks enabled (`-enableAll`). No `defer` before `os.Exit` — use explicit cleanup instead.
- **gRPC APIs**: Use `grpc.NewClient` (not `grpc.Dial`/`DialContext`). Use `insecure.NewCredentials()` (not `grpc.WithInsecure()`).
- **Error returns**: All error returns must be checked (`errcheck`). Use `_ =` for intentionally ignored errors in cleanup paths.
- **Parameter style**: Combine consecutive same-type params (`store, key string` not `store string, key string`).
- **Network binding**: gRPC listeners bind to `127.0.0.1` to avoid gosec G102.

## Skills

Use the following skills when working on related files:

| File(s) | Skill |
|---------|-------|
| `Makefile` | `/makefile` |
| `renovate.json` | `/renovate` |
| `README.md` | `/readme` |
| `.github/workflows/*.yml` | `/ci-workflow` |

When spawning subagents, always pass conventions from the respective skill into the agent's prompt.
