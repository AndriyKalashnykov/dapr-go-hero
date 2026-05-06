[![CI](https://github.com/AndriyKalashnykov/dapr-go-hero/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/AndriyKalashnykov/dapr-go-hero/actions/workflows/ci.yml)
[![Hits](https://hits.sh/github.com/AndriyKalashnykov/dapr-go-hero.svg?view=today-total&style=plastic)](https://hits.sh/github.com/AndriyKalashnykov/dapr-go-hero/)
[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Renovate enabled](https://img.shields.io/badge/renovate-enabled-brightgreen.svg)](https://app.renovatebot.com/dashboard#github/AndriyKalashnykov/dapr-go-hero)

# dapr-go-hero

Reference implementation of a Go microservice platform on [Dapr](https://dapr.io), comparing three client approaches (custom HTTP, custom gRPC, Dapr Go SDK) across four Dapr building blocks: Pub/Sub with content-based routing, State Store, Secret Store, and Service Invocation. Production-ready K8s deployment with KinD + cloud-provider-kind for local parity, Testcontainers-backed integration tests, and a CI pipeline that provisions a full Dapr cluster on every PR.

<p align="center"><img src="docs/diagrams/out/c4-context.png" alt="C4 System Context: dapr-go-hero fans CloudEvents to Redis, PostgreSQL, Zipkin, and the Kubernetes Secret API" width="720"></p>

## Tech Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Language | Go 1.26.2 | Static binaries, minimal runtime, strong concurrency primitives |
| Dapr runtime | v1.17.6 | Decouples building-block APIs from broker/store implementations |
| HTTP framework | [Fiber v3](https://gofiber.io/) | Fasthttp-backed; lower allocation/request than `net/http` under load |
| Database driver | [pgx v5](https://github.com/jackc/pgx) | Native Postgres protocol + prepared-statement pooling (no `database/sql` overhead) |
| Event backbone | Redis (`pubsub.redis` v1, Streams under the hood) + Redis (state) | Dapr's default low-ceremony broker; swap-in any supported backend via component YAML |
| RPC | gRPC + Protobuf | Strongly-typed inter-service contracts, HTTP/2 multiplexing, native tracing headers |
| Container | Alpine + BuildKit cache mounts | ~12–21 MB images; `go mod download` cached across builds |
| Orchestration | Kubernetes + KinD + cloud-provider-kind | LoadBalancer Service support in KinD via the kind-team-maintained host-side controller — closes the parity gap with cloud K8s |
| Observability | Zipkin via Dapr sidecar | Zero-code distributed tracing; Dapr propagates W3C Trace Context across hops |
| Secrets | `secretstores.kubernetes` (K8s-native) | RBAC-scoped; no external Vault dependency |
| Resiliency | Dapr `Resiliency` CRD | Declarative retries, timeouts, circuit breakers per target app/component |
| Test layers | Unit (Go testing), Integration (Testcontainers), E2E (KinD + curl) | Each layer catches a different class of defect |
| Tool versioning | [mise](https://mise.jdx.dev) | Single source of truth for every CLI: Go, kind, golangci-lint, gosec, hadolint, trivy, gitleaks, actionlint, shellcheck, act, protoc, protoc-gen-* — pinned in `.mise.toml`, installed by `make deps` |
| Code quality | golangci-lint, gosec, hadolint, mermaid-cli | Enforced via composite `make static-check` gate |
| Dependency updates | [Renovate](https://docs.renovatebot.com/) | Built-in `gomod` / `github-actions` / `dockerfile` / `kubernetes` managers + custom-regex coverage of Makefile, `.mise.toml`, and workflow `with:` pins |

## Building Blocks Demonstrated

| Block | Component | Store | Access pattern |
|-------|-----------|-------|----------------|
| Pub/Sub (content-routed) | `pubsub.redis` | Redis Streams | CloudEvent type → declarative Subscription route |
| State Store | `state.redis` | Redis | Dapr Go SDK key/value API |
| Secret Store | `secretstores.kubernetes` | K8s Secrets | Credentials fetched at startup, no env vars |
| Service Invocation | Dapr sidecar-to-sidecar | gRPC | Fully-qualified app ID for cross-namespace routing, mTLS, Zipkin traces |

## Quick Start

Two delivery modes. Kubernetes (Option A) is closest to production; local dev (Option B) iterates faster on single-binary changes.

### Option A — Kubernetes (KinD + cloud-provider-kind + Dapr)

```bash
make kind-up       # create KinD cluster, install cloud-provider-kind + Dapr + Dashboard, deploy stack
make e2e           # run 11 end-to-end assertions against the deployed cluster (sdk-http mode)
make e2e-all       # run all 11 assertions across all 4 client modes (sdk-http, sdk-grpc, custom-http, custom-grpc)
make k8s-status    # show pods/services across all namespaces
make kind-down     # destroy everything
```

Access deployed services via `kubectl port-forward`:

| Service | Local URL | Port-forward command |
|---------|-----------|----------------------|
| Inventory REST API | `http://localhost:3000` | `kubectl port-forward svc/inventory 3000:3000 -n dapr-go-hero-inventory` |
| Dapr Dashboard | `http://localhost:8080` | `kubectl port-forward svc/dapr-dashboard 8080:8080 -n dapr-system` |
| Zipkin UI | `http://localhost:9411` | `kubectl port-forward svc/zipkin 9411:9411 -n dapr-go-hero` |
| Redis | `localhost:6379` | `kubectl port-forward svc/redis 6379:6379 -n dapr-go-hero` |
| PostgreSQL | `localhost:5432` | `kubectl port-forward svc/postgres 5432:5432 -n dapr-go-hero` |

Query examples:

```bash
curl http://localhost:3000/v1/widgets/widget | jq
curl http://localhost:3000/v1/gadgets/gadget | jq
curl http://localhost:3000/v1/products/thingamajig | jq
```

### Option B — Local dev (standalone binaries)

Single-binary processes with a Dapr sidecar each. Useful for tight feedback loops on code changes.

```bash
make deps           # install mise + pinned tool versions
make build          # compile both binaries
make test           # unit tests
make run-products   # terminal 1: products gRPC service
make run            # terminal 2: inventory service (default: SDK HTTP mode)
make send-all       # terminal 3: publish 3 CloudEvents
make get-all        # query REST endpoints
```

Local dev requires a running PostgreSQL container and `secrets.json`. See [Local dev setup](#local-dev-setup).

## Prerequisites

**Core (both modes):**

| Tool | Version | Purpose |
|------|---------|---------|
| [Git](https://git-scm.com/) | 2.0+ | Version control |
| [Go](https://go.dev/dl/) | 1.26.2+ | Auto-installed via [mise](https://mise.jdx.dev) on `make deps` |
| [GNU Make](https://www.gnu.org/software/make/) | 3.81+ | Build orchestration |
| [Docker](https://www.docker.com/) | BuildKit-enabled | Container builds; required by integration tests (Testcontainers) |
| [jq](https://jqlang.github.io/jq/) | any | JSON response formatting |

**Kubernetes mode (Option A):**

| Tool | Version | Purpose |
|------|---------|---------|
| [KinD](https://kind.sigs.k8s.io/) | 0.31+ | Local Kubernetes control plane in a container |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | bundled with KinD | K8s CLI |
| [Dapr CLI](https://docs.dapr.io/getting-started/install-dapr-cli/) | 1.17+ | `dapr init -k` installs runtime into KinD |

**Local dev mode (Option B):**

| Tool | Version | Purpose |
|------|---------|---------|
| [Dapr CLI](https://docs.dapr.io/getting-started/install-dapr-cli/) | 1.17+ | `dapr init` provides self-hosted Redis + Zipkin |
| PostgreSQL | 16+ | Run as Docker container, see [Local dev setup](#local-dev-setup) |

**Tooling auto-installed via mise** (`make deps` reads `.mise.toml`):

`go`, `kind`, `golangci-lint`, `gosec`, `govulncheck`, `gitleaks`, `actionlint`, `shellcheck`, `trivy`, `act`, `hadolint`, `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`, `node`. No `apt`/`brew`/`go install` steps required — just `make deps`.

Install everything:

```bash
make deps
```

## Design Rationale

### Package organization

Organized by feature (`pkg/features/{widgets,gadgets,products}`), not by type. Dapr pub/sub requires a single callback URL per app, so each feature's subscriptions are merged in `pkg/dapr/subscription.go` before being returned from `/dapr/subscribe`.

### Three client approaches

Both the custom HTTP/gRPC wrappers (`pkg/dapr/client_http.go`, `client_grpc.go`) and the Dapr Go SDK (`client_sdk.go`) are implemented against the same `state.Store` / `secrets.Store` interfaces. This isolates the integration surface, simplifies swapping implementations, and documents the trade-offs (SDK: less code, protocol-agnostic; custom: explicit control, smaller dependency set).

### HTTP router

[Fiber v3](https://gofiber.io/) is used for the public REST API (port 3000). Built on fasthttp, Fiber has lower per-request allocation than `net/http` under sustained load, which matters for gateway-fronted services. Dapr's internal callback server uses `net/http` via the SDK — no conflict.

### Private callback port

Dapr pub/sub callbacks (e.g., `/widgets.v1`) are delivery mechanics, not a public API. They listen on a separate port from the REST API to prevent accidental exposure and simplify Ingress/Service rules.

### Configuration surface

`pkg/config/config.go` centralizes every host/port/app-id behind env vars with sensible defaults. A small built-in `loadDotenv` helper reads `.env` on startup without overwriting existing process env vars (zero dependencies — the helper is ~20 lines in `pkg/config/config.go`). K8s deployments set the same keys via `env:` in the pod spec — one source of truth, two consumers.

### Test pyramid

- **Unit** (`_test.go`): pure logic, mocked interfaces, millisecond execution. Run in CI on every push.
- **Integration** (`//go:build integration`): widgets repo against real PostgreSQL via Testcontainers-go; gadgets repo against a stub Dapr state sidecar (httptest); products repo against a real gRPC server on an ephemeral port; `pkg/dapr/client_http` against a stub sidecar. Catches schema drift, wire-format drift, and SQL injection regressions. Run in CI on every push.
- **E2E** (`tests/e2e.sh`): KinD cluster + cloud-provider-kind + Dapr + full manifest deploy. Verifies sidecar injection, pub/sub routing, cross-namespace service invocation, and Zipkin trace propagation. Run in CI on every push (~3–5 min).

Gadget and Products repositories are covered by mock-based unit tests plus real E2E — adding a third integration layer for those would duplicate what E2E already validates.

## Event Flow

Each CloudEvent arrives on the `inventory` topic. Dapr's content-based routing selects one of three handlers based on `event.type`:

<p align="center"><img src="docs/diagrams/out/c4-container.png" alt="C4 Container view: dapr-go-hero topology across three K8s namespaces with Dapr sidecars, routing, and Zipkin tracing" width="800"></p>

Source: [`docs/diagrams/c4-container.puml`](docs/diagrams/c4-container.puml) · regenerate with `make diagrams`. Related ADRs: [ADR-0001](docs/adr/0001-adopt-dapr-building-blocks.md) (adopt Dapr), [ADR-0003](docs/adr/0003-four-client-modes.md) (four client modes).

**Cross-cutting concerns visible above:**

- **Resiliency** (`k8s/dapr/resiliency.yaml`): service invocation to products uses exponential retry (3×, 200ms→10s) guarded by a circuit breaker (trips after 5 consecutive failures, 10s cool-down). State writes retry 3× (exponential 100ms→5s). Pub/Sub has both an outbound publish-retry policy and an inbound subscriber-handler retry (unlimited exponential) so events redeliver after downstream recovery. Redis Streams' `processingTimeout=30s` + `redeliverInterval=5s` provide the broker-level at-least-once guarantee underneath.
- **Tracing** (`k8s/dapr/configuration.yaml` tracing block): both sidecars sample 100% and ship W3C Trace Context spans to Zipkin. Inventory → products invocation and every Redis/PostgreSQL hop appear as a single connected trace; `tests/e2e.sh` asserts spans exist for both `inventory` and `products` app-IDs after each run.
- **Access control** (products namespace `configuration.yaml`): `defaultAction: deny` with `inventory` in the allowlist. Any other app-ID calling the products sidecar is rejected at the sidecar boundary before reaching the app; mTLS between sidecars (`mtls.enabled: true`) authenticates the caller's identity.
- **Subscription registration** (dual-mode): K8s `Subscription` CRD (`k8s/dapr/subscription.yaml`) declares the topic and route rules declaratively; in parallel, each feature package also registers programmatically via the app's `/dapr/subscribe` endpoint, with `pkg/dapr/subscription.go` merging all per-feature subscriptions into one response. Either path alone is functional; both are present so the example exercises the full API surface.

| event.type | Route | Storage backend | Dapr building block |
|-----------|-------|-----------------|---------------------|
| `widget.v1` | `/widgets.v1` | PostgreSQL (creds from Secret Store) | Secret Store + direct DB |
| `gadget.v1` | `/gadgets.v1` | Redis via `state.redis` | State Store |
| default (e.g. `thingamajig.v1`) | `/products.v1` | Products gRPC service | Service Invocation |

Dapr envelopes events in [CloudEvents](https://cloudevents.io/) and propagates W3C Trace Context headers across every hop — all three flows appear as a single trace in Zipkin.

Service Invocation to the Products service uses a generated gRPC client pointed at the local Dapr sidecar, with `dapr-app-id` metadata attached. In K8s with per-service namespaces, the app ID is fully-qualified (`products.dapr-go-hero-products`) because Dapr's default name resolver is namespace-scoped — see [docs/containerize-and-deploy.md](docs/containerize-and-deploy.md) for the resolution semantics.

## Architecture & Deployment

<p align="center"><img src="docs/diagrams/out/c4-deployment.png" alt="C4 Deployment view: Docker host running a KinD cluster with three namespaces, injected Dapr sidecars, and the host-side cloud-provider-kind LoadBalancer controller" width="800"></p>

Source: [`docs/diagrams/c4-deployment.puml`](docs/diagrams/c4-deployment.puml) · regenerate with `make diagrams`. Decision: see [ADR-0002](docs/adr/0002-cloud-provider-kind-over-metallb.md) for the MetalLB → cloud-provider-kind rationale.

### Cluster Topology (flowchart)

```mermaid
%%{init: {'theme':'base','themeVariables':{'background':'#FFFFFF','fontFamily':'ui-sans-serif, system-ui, sans-serif','primaryColor':'#FFFFFF','primaryTextColor':'#000000','primaryBorderColor':'#000000','secondaryColor':'#FFFFFF','tertiaryColor':'#FFFFFF','lineColor':'#0070E5','edgeLabelBackground':'#FFFFFF','clusterBkg':'#FFFFFF','clusterBorder':'#0070E5','titleColor':'#0070E5'}}}%%
graph TB
  subgraph Client["External (host)"]
    C[curl / browser]
  end

  subgraph Cluster["KinD cluster"]
    subgraph LB["Host-side"]
      ML[cloud-provider-kind<br/>LoadBalancer controller]
    end

    subgraph DaprSystem["dapr-system"]
      DS[Dapr control plane<br/>Operator · Sentry · Placement<br/>Scheduler · Sidecar Injector]
      DD[Dapr Dashboard]
    end

    subgraph Infra["dapr-go-hero (infrastructure)"]
      R[(Redis<br/>6379)]
      P[(PostgreSQL<br/>5432)]
      Z[Zipkin<br/>9411]
    end

    subgraph InvNS["dapr-go-hero-inventory"]
      ISVC{{Service LB :3000}}
      IPOD[inventory pod<br/>app + daprd sidecar]
      ISEC[[Secret: postgres]]
      ISA((ServiceAccount<br/>+ secret-reader Role))
      ICRD[Dapr CRDs:<br/>pubsub, statestore,<br/>secretstore, subscription,<br/>resiliency, configuration]
    end

    subgraph ProdNS["dapr-go-hero-products"]
      PSVC{{Service :50151}}
      PPOD[products pod<br/>app + daprd sidecar]
      PSA((ServiceAccount))
      PCRD[Dapr CRD:<br/>configuration]
    end

    ML -.assigns LB IP.-> ISVC
    C -->|HTTP :3000| ISVC --> IPOD

    IPOD -->|pub/sub| R
    IPOD -->|state| R
    IPOD -->|SQL| P
    IPOD -->|GetSecret kubernetes| ISEC
    IPOD -.->|traces| Z
    PPOD -.->|traces| Z

    IPOD ==>|"gRPC via Dapr<br/>(fully-qualified app ID)"| PPOD

    DS -.manages.-> IPOD
    DS -.manages.-> PPOD
    ISA -.grants.-> IPOD
    PSA -.grants.-> PPOD
    ICRD -.configures.-> IPOD
    PCRD -.configures.-> PPOD
  end

  classDef plain fill:#FFFFFF,stroke:#0070E5,color:#000000,stroke-width:1px
  classDef dapr fill:#0070E5,stroke:#0070E5,color:#FFFFFF,stroke-width:1px
  classDef emphasis fill:#000000,stroke:#0070E5,color:#FFFFFF,stroke-width:1px

  class C,ML,R,P,Z,ISEC,ISA,ICRD,PSA,PCRD,ISVC,PSVC plain
  class DS,DD dapr
  class IPOD,PPOD emphasis
```

### Event Flow (CloudEvent → 3 routes)

```mermaid
%%{init: {'theme':'base','themeVariables':{'background':'#FFFFFF','fontFamily':'ui-sans-serif, system-ui, sans-serif','primaryColor':'#FFFFFF','primaryTextColor':'#000000','primaryBorderColor':'#0070E5','actorBkg':'#FFFFFF','actorBorder':'#0070E5','actorTextColor':'#000000','actorLineColor':'#0070E5','signalColor':'#0070E5','signalTextColor':'#0070E5','labelBoxBkgColor':'#0070E5','labelBoxBorderColor':'#0070E5','labelTextColor':'#FFFFFF','loopTextColor':'#0070E5','noteBkgColor':'#FFFFFF','noteBorderColor':'#0070E5','noteTextColor':'#000000','altBackground':'#FFFFFF','activationBkgColor':'#0070E5','activationBorderColor':'#0070E5','sequenceNumberColor':'#FFFFFF'}}}%%
sequenceDiagram
  autonumber
  actor Pub as Publisher<br/>(curl / make send-*)
  participant IDaprd as inventory daprd
  participant Redis as Redis pub/sub
  participant IApp as inventory app
  participant PG as PostgreSQL
  participant RState as Redis state
  participant PDaprd as products daprd
  participant PApp as products app

  Pub->>+IDaprd: POST /v1.0/publish/pubsub/inventory<br/>CloudEvent (type=widget.v1)
  IDaprd->>Redis: XADD inventory
  Redis-->>IDaprd: deliver to subscribers
  IDaprd->>+IApp: route by event.type

  alt widget.v1 → /widgets.v1
    IApp->>+PG: INSERT widget (via Secret Store creds)
    PG-->>-IApp: ok
  else gadget.v1 → /gadgets.v1
    IApp->>+RState: SET gadget:id
    RState-->>-IApp: ok
  else default → /products.v1 (thingamajig)
    IApp->>IDaprd: gRPC SaveProduct<br/>header dapr-app-id: products.dapr-go-hero-products
    IDaprd->>+PDaprd: invoke (cross-namespace name resolution)
    PDaprd->>+PApp: SaveProduct RPC
    PApp-->>-PDaprd: empty response
    PDaprd-->>-IDaprd: ok
  end

  IApp-->>-IDaprd: 204 No Content
  IDaprd-->>-Pub: 204 No Content

  Note over IDaprd,PDaprd: All hops traced → Zipkin
```

### Namespace Isolation

| Namespace | Contents | RBAC |
|-----------|----------|------|
| `dapr-go-hero` | Infrastructure: Redis, PostgreSQL, Zipkin | — |
| `dapr-go-hero-inventory` | Inventory deployment + six Dapr CRDs | ServiceAccount + `secret-reader` Role (required by `secretstores.kubernetes`) |
| `dapr-go-hero-products` | Products gRPC service + Configuration CRD | ServiceAccount with minimal permissions |

Per-service namespaces enforce least-privilege RBAC and scope Dapr access-control policies.

### Dapr CRDs (`k8s/dapr/`)

| File | Kind | Purpose |
|------|------|---------|
| `pubsub.yaml` | Component | Redis Streams pub/sub, FQDN-addressed |
| `statestore.yaml` | Component | Redis state store, scoped to `inventory` |
| `secretstore.yaml` | Component | `secretstores.kubernetes` — reads namespace Secrets |
| `subscription.yaml` | Subscription v2alpha1 | Content-based routing: `widget.v1` / `gadget.v1` / default |
| `resiliency.yaml` | Resiliency | Exponential retries + timeout + circuit breaker on `products` target |
| `configuration.yaml` | Configuration | Zipkin endpoint, mTLS, per-namespace access control |

### Container Images

Multi-stage Alpine with BuildKit cache mounts. Post-initial build, `make docker-build` completes in ~0.2 s.

### Local Cluster Composition

`make kind-up` (alias for `kind-deploy`) brings up the full stack; `make kind-create` provisions the cluster and installs the LoadBalancer controller + Dapr without deploying application manifests:

- **cloud-provider-kind** — kind-team-maintained host-side controller that watches `Service` objects of type `LoadBalancer` and allocates IPs on the `kind` Docker network. Replaces MetalLB: no in-cluster DaemonSet, no IPAddressPool/L2Advertisement CRDs, works across all supported `kindest/node` versions without release-track drift.
- **Dapr runtime** via `dapr init -k --runtime-version $(DAPR_RUNTIME_VERSION)` — Operator, Sentry, Placement, Scheduler, Sidecar Injector, pinned to the same version CI uses
- **Dapr Dashboard** — cluster inspection UI

## Local dev setup

See [Quick Start → Option A](#option-a--kubernetes-kind--cloud-provider-kind--dapr) for the Kubernetes flow. For standalone binaries:

```bash
docker run --name postgres -e POSTGRES_PASSWORD=postgres -p 5432:5432 -d postgres:16
cat tables.sql | docker exec -i postgres psql -U postgres -d postgres
```

Start services (three terminals):

```bash
# Terminal 1
make run-products

# Terminal 2 — pick a client mode
make run-custom-http   # custom HTTP client
make run-custom-grpc   # custom gRPC client
make run-sdk-http      # Dapr Go SDK over HTTP (default via `make run`)
make run-sdk-grpc      # Dapr Go SDK over gRPC

# Terminal 3 — publish events, then query
make send-all
make get-all
```

Each `run-*` target maps to a different code path in `cmd/inventory/main.go` and exercises the same interfaces (`state.Store`, `secrets.Store`) through a different transport.

## Available Make Targets

Run `make help` for the full list.

### Build & Test

| Target | Description |
|--------|-------------|
| `make build` | Compile both binaries (static, CGO disabled) |
| `make test` | Unit tests (`go test -race ./...`) |
| `make integration-test` | Integration tests (Postgres, Dapr-state stub, gRPC) via Testcontainers |
| `make format` | Auto-format Go source (`gofmt` + `golangci-lint --fix`) |
| `make clean` | Remove build artifacts |
| `make update` | `go get -u ./... && go mod tidy` |

### Quality Gates

| Target | Description |
|--------|-------------|
| `make static-check` | Composite gate: lint-ci + lint + sec + vulncheck + secrets + docker-lint + trivy-fs + trivy-config + mermaid-lint + diagrams-check + deps-prune-check |
| `make lint` | golangci-lint (gocritic + gosec via `.golangci.yml`) |
| `make lint-ci` | GitHub Actions workflow linting (actionlint + shellcheck) |
| `make sec` | gosec SAST scan |
| `make vulncheck` | `govulncheck ./...` — known CVEs in dependencies |
| `make secrets` | gitleaks — hardcoded credentials scan |
| `make trivy-fs` | Trivy filesystem scan (vulns + secrets + misconfigs), CRITICAL+HIGH |
| `make trivy-config` | Trivy K8s manifest misconfig scan (KSV-*), CRITICAL+HIGH |
| `make mermaid-lint` | Validate Mermaid diagrams via `minlag/mermaid-cli` Docker image |
| `make diagrams` | Render PlantUML diagrams via pinned `plantuml/plantuml` image |
| `make diagrams-check` | CI drift gate: fail if committed PNGs differ from `.puml` sources |
| `make diagrams-clean` | Remove rendered diagram PNGs |
| `make deps-prune-check` | Fail if `go.mod`/`go.sum` would be modified by `go mod tidy` |

### CI

| Target | Description |
|--------|-------------|
| `make ci` | Full local pipeline: deps → format → static-check → test → integration-test → build |
| `make ci-run` | Execute GitHub Actions workflow locally via [act](https://github.com/nektos/act) |

### Dapr Services

| Target | Description |
|--------|-------------|
| `make run` | Inventory service with Dapr (default: SDK HTTP mode) |
| `make run-products` | Products gRPC service |
| `make run-custom-http` | Inventory with custom HTTP client |
| `make run-custom-grpc` | Inventory with custom gRPC client |
| `make run-sdk-http` | Inventory with Dapr Go SDK over HTTP |
| `make run-sdk-grpc` | Inventory with Dapr Go SDK over gRPC |
| `make dapr-test` | Dapr sidecar only (no app) — useful for API debugging |

### Events & Queries

| Target | Description |
|--------|-------------|
| `make send-widget` / `send-gadget` / `send-thingamajig` | Publish individual CloudEvents |
| `make send-all` | Publish all three event types |
| `make get-widget` / `get-gadget` / `get-thingamajig` | GET individual items |
| `make get-all` | GET all three |

### Docker & Kubernetes

| Target | Description |
|--------|-------------|
| `make docker-build` | Build both images (BuildKit + cache mounts; multi-arch-ready Dockerfiles) |
| `make docker-push` | Push to `$(REGISTRY)` |
| `make docker-lint` | Hadolint on Dockerfiles |
| `make docker-smoke-test` | Boot each image briefly + assert it stays up — local equivalent of CI Gate 3. Override `SERVICES=inventory` to scope. |
| `make kind-up` | Full stack: create cluster + cloud-provider-kind + Dapr + deploy manifests (alias for `kind-deploy`) |
| `make kind-down` | Full teardown (alias for `kind-destroy`) |
| `make kind-create` | Create cluster + install LoadBalancer controller + Dapr (no app deploy) |
| `make kind-bootstrap` | Install cloud-provider-kind + Dapr into an existing KinD cluster (used by CI after `helm/kind-action`) |
| `make kind-deploy` | `kind-create` + `k8s-deploy` (full stack up) |
| `make kind-destroy` | Delete cluster, stop cloud-provider-kind, prune `kindccm-*` orphan sidecars |
| `make k8s-deploy` | Build images, load into KinD, apply all manifests |
| `make k8s-undeploy` | Remove all manifests |
| `make k8s-status` | Pod/service status across namespaces |
| `make e2e` | Run `tests/e2e.sh` against deployed cluster (default: sdk-http mode) |
| `make e2e-all` | Run e2e across all 4 client modes (sdk-http, sdk-grpc, custom-http, custom-grpc) |
| `make e2e-setup` / `e2e-teardown` | Composite setup/teardown |

### Utilities

| Target | Description |
|--------|-------------|
| `make deps` | Install all tools pinned in `.mise.toml` via mise (Go, kind, golangci-lint, gosec, govulncheck, gitleaks, actionlint, shellcheck, trivy, act, hadolint, protoc, protoc-gen-go, protoc-gen-go-grpc, node) |
| `make deps-check` | Show mise + Go status |
| `make deps-prune` | Run `go mod tidy` (shows any changes needed) |
| `make generate-env` | Regenerate `.env` from `pkg/config` defaults |
| `make proto-gen` | Regenerate gRPC code from `.proto` files (uses mise-pinned `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`) |
| `make release` | Tag and push a new semver release |
| `make renovate-bootstrap` | Install Node via mise — auto-invoked by `make renovate-validate` |
| `make renovate-validate` | Dry-run Renovate config |

## CI/CD

GitHub Actions runs on every push to `main`, tags `v*`, pull requests, and `workflow_call`.

| Job | Depends on | Purpose |
|-----|-----------|---------|
| `changes` | — | Path-filter detector (`dorny/paths-filter`) — gates downstream on whether code changed; doc-only changes short-circuit |
| `static-check` | changes | `make static-check` composite gate (lint-ci + lint + sec + vulncheck + secrets + docker-lint + trivy-fs + trivy-config + mermaid-lint + diagrams-check + deps-prune-check) |
| `build` | changes, static-check | Compile both binaries |
| `test` | changes, static-check | Unit tests (`go test -race ./...`) |
| `integration-test` | changes, static-check | Integration tests with Testcontainers PostgreSQL (`make integration-test`) |
| `docker` | changes, static-check, build, test | **Tag-gated** (`refs/tags/v*`) hardened image publish pipeline — see [Pre-push image hardening](#pre-push-image-hardening) below. Skipped on non-tag pushes |
| `e2e` | changes, build, test | Provision KinD + cloud-provider-kind + Dapr, deploy, run 11 end-to-end assertions across all 4 client modes (`make e2e-all`). Gated on code-affecting paths or the `run-e2e` PR label |
| `ci-pass` | all above | Aggregate gate — single required status check for branch protection |

### Pre-push image hardening

The `docker` job runs the following gates **before** any image is pushed to GHCR. Any failure blocks the release. The job is tag-gated (`refs/tags/v*`) and runs once per service via `strategy.matrix`.

| # | Gate | Catches | Tool |
|---|------|---------|------|
| 1 | Build local single-arch image (`load: true`) | Build regressions on the runner architecture | `docker/build-push-action` |
| 2 | **Trivy image scan** (CRITICAL/HIGH, fixed-only — blocking) | CVEs in the alpine base layer, OS packages, and our own files | `aquasecurity/trivy-action` with `image-ref:` |
| 3 | **Smoke test** (`make docker-smoke-test`) | Image boots correctly: USER 10001 has read/exec, libc deps resolve, ENTRYPOINT wired, Go runtime starts | `docker run` + boot-marker probe |
| 4 | Multi-arch build + push (`linux/amd64,linux/arm64`) | Publishes for both Intel and ARM consumers (Apple Silicon, Graviton, Pi, etc.) | `docker/build-push-action` with `platforms:` |
| 5 | **Cosign keyless OIDC signing** | Sigstore signature on the manifest digest | `sigstore/cosign-installer` + `cosign sign` |

Buildkit in-manifest attestations (`provenance` + `sbom`) are deliberately disabled so the image index stays free of `unknown/unknown` platform entries — this lets the GHCR Packages UI render the "OS / Arch" tab for the multi-arch manifest. Cosign keyless signing still provides the Sigstore signature for supply-chain verification.

**Pull a published image:**

```bash
docker pull ghcr.io/andriykalashnykov/dapr-go-hero/inventory:latest
docker pull ghcr.io/andriykalashnykov/dapr-go-hero/products:latest
```

**Verify the cosign signature:**

```bash
cosign verify ghcr.io/andriykalashnykov/dapr-go-hero/inventory:<tag> \
  --certificate-identity-regexp 'https://github\.com/AndriyKalashnykov/dapr-go-hero/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

**Local parity:** the same gates run via `make docker-build && make docker-smoke-test` before tagging a release. Pre-push is the second line of defense; the local commands catch most regressions earlier.

The `changes` job uses the canonical path-filter expression so doc-only changes (`**.md`, `docs/**`, `LICENSE`, etc.) are detected at the workflow-run level rather than the trigger level. Trigger-level `paths-ignore` would create a Repository-Rulesets deadlock: no run, no `ci-pass` status, blocked merge.

### Required Secrets and Variables

No custom secrets required — CI uses only the implicit `GITHUB_TOKEN`.

[Renovate](https://docs.renovatebot.com/) auto-merges minor/patch updates after CI passes (3-day minimum release age on majors). Coverage spans the built-in `gomod` / `github-actions` / `dockerfile` / `kubernetes` managers plus three custom-regex managers for Makefile constants, `.mise.toml` tool pins, and workflow `with:` value pins.

A scheduled workflow (`.github/workflows/cleanup-runs.yml`) removes workflow runs older than 7 days weekly (retains last 5 regardless of age).

## Architecture Decision Records

See [`docs/adr/`](docs/adr/) for the key non-obvious decisions behind this project:

- [ADR-0001](docs/adr/0001-adopt-dapr-building-blocks.md) — adopting Dapr as the integration runtime
- [ADR-0002](docs/adr/0002-cloud-provider-kind-over-metallb.md) — cloud-provider-kind over MetalLB for LoadBalancer support
- [ADR-0003](docs/adr/0003-four-client-modes.md) — shipping four Dapr client implementations side-by-side

## Further reading

- Companion slides: [`slides.pdf`](slides.pdf)
- Original pattern: [pkedy/golang-dapr](https://github.com/pkedy/golang-dapr)
- [vladimirvivien/dapr-examples](https://github.com/vladimirvivien/dapr-examples)
- Article: [Building Cloud-Native Services with Dapr, Go, and Kubernetes](https://medium.com/@vladimirvivien/building-cloud-native-services-with-dapr-go-and-kubernetes-part-2-f773d484ecb0)
- Implementation notes: [docs/containerize-and-deploy.md](docs/containerize-and-deploy.md)

Contributions welcome — open a PR.
