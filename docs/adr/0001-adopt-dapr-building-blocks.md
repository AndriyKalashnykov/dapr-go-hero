# ADR-0001: Adopt Dapr building blocks for cross-service concerns

- **Status**: Accepted
- **Date**: 2026-04-16
- **Deciders**: project author

## Context

The project demonstrates a small microservice system that fans CloudEvents from a single pub/sub topic into three persistence paths (PostgreSQL, Redis state, downstream gRPC service). Each persistence path needs its own concern handled: credentials for PostgreSQL, state encoding for Redis, and mTLS + retry for the inter-service call.

Options for cross-service concerns in Go generally split into:

1. **Hand-rolled libraries** — one per concern (viper/envconfig for secrets, go-redis for state, grpc-go for invocation, hashicorp/go-retryablehttp for retries).
2. **Spring Cloud-style framework** — none exists natively in Go.
3. **Dapr** — a runtime that exposes building blocks (pub/sub, state, secrets, service invocation, resiliency) behind a language-agnostic sidecar.

## Decision

Use **Dapr** as the integration runtime. Each inventory/products service binary calls local sidecar APIs instead of vendor libraries; the sidecar handles broker, datastore, and cross-service concerns.

## Consequences

- **Positive**: the service code stays small and vendor-neutral (swap Redis for NATS, swap Postgres secret-store backend, add a circuit breaker — all via component YAML). Pub/sub content-based routing, mTLS, distributed tracing, and retry policies are declarative configuration rather than framework boilerplate. The project becomes a compact reference for four Dapr client styles (custom HTTP, custom gRPC, SDK HTTP, SDK gRPC — see ADR-0003).
- **Negative**: adds an operational dependency (the Dapr control plane + injected sidecar per pod). Adds a second deployment artifact (component YAMLs) that must be kept in sync across local dev (`components/*.yaml`) and K8s (`k8s/dapr/*.yaml`). Dapr runtime upgrades are coupled to K8s cluster upgrades.
- **Neutral**: the sidecar model trades in-process latency for a local loopback hop (~1ms). For non-hot-path services this is dominated by downstream I/O anyway.

## Alternatives considered

| Option | Pros | Cons | Why not |
|--------|------|------|---------|
| Hand-rolled Go libraries per concern | No extra runtime; maximum control | Every concern is bespoke; no consistent retry/tracing posture; secrets and state tightly coupled to specific vendors | Loses the educational demo value of comparing building blocks |
| OpenTelemetry + direct drivers | Industry-standard tracing, thinner stack | Still need to hand-code pub/sub, retries, secret rotation | Covers observability but not the full cross-cutting surface |
| Istio + direct drivers | mTLS + retries via service mesh | Heavy infrastructure; doesn't address pub/sub, state, or secrets | Wrong scope for a single-binary demo |

## Related

- Container diagram: `docs/diagrams/c4-container.puml`
- Component definitions: `components/` (local dev) and `k8s/dapr/` (cluster)
- Dapr runtime version pin: `Makefile` → `DAPR_RUNTIME_VERSION`
