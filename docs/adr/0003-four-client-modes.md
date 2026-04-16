# ADR-0003: Ship four Dapr client implementations side-by-side

- **Status**: Accepted
- **Date**: 2026-04-16
- **Deciders**: project author

## Context

Dapr's application API is available over two transports (HTTP and gRPC), and the project can call it two ways:

1. **Custom hand-rolled client** — the app calls `http://127.0.0.1:3500/v1.0/state/...` (HTTP) or the `dapr.proto.runtime.v1.Dapr` gRPC stub directly, parsing responses with stdlib.
2. **Dapr Go SDK** (`github.com/dapr/go-sdk`) — higher-level client wrapping the same APIs with typed helpers.

That yields 2 × 2 = four client modes. Most Dapr samples pick one and move on.

The educational goal of this repo is *specifically* to let readers compare the four modes against each other. The cost is more code; the question is whether the comparison is worth it.

## Decision

Implement all four modes and select between them at runtime via the inventory binary's first positional argument (`custom-http`, `custom-grpc`, `sdk-http`, or SDK-default). E2E tests exercise each mode end-to-end on every CI push via `make e2e-all`.

## Consequences

- **Positive**:
  - Readers can diff the four implementations side-by-side (`pkg/dapr/client_http.go`, `client_grpc.go`, `client_sdk.go`) to see the ergonomic and coverage differences between raw HTTP, raw gRPC, and the SDK.
  - CI matrix coverage (4 modes × 11 assertions = 44 assertions per push) catches regressions in any one client that a single-mode test suite would miss.
  - The `state.Store` and `secrets.Store` interfaces in `pkg/components/` stay thin — each client implements both interfaces, proving the abstraction is the real contract.
- **Negative**:
  - More code to maintain. Four clients multiply the surface area for Dapr runtime API changes.
  - E2E CI time grows roughly linearly with mode count (~6 min per mode). `make e2e-all` takes ~25 minutes on GitHub Actions runners.
  - The E2E test harness must redeploy the inventory pod between modes (kubectl patch + rollout wait), adding cluster churn.
- **Neutral**:
  - Local development picks any one mode via `make run-custom-http` / `run-custom-grpc` / `run-sdk-http` / `run-sdk-grpc`.

## Alternatives considered

| Option | Pros | Cons | Why not |
|--------|------|------|---------|
| Pick one (SDK HTTP) | Smallest surface area, fastest CI | Loses the comparative-demo value of the repo | Defeats the purpose of the project |
| Separate repos per mode | Isolates each example | 4× the upkeep; drift between samples; no apples-to-apples comparison possible | Harder to maintain than parallel files in one repo |
| Pick two (SDK HTTP + SDK gRPC) | Covers both transports with SDK ergonomics | Hides the "raw API" path which matters for contributors building non-SDK languages | Middle-ground loses on both axes |

## Related

- Code entry points: `cmd/inventory/main.go` (`clientType` dispatch)
- Client implementations: `pkg/dapr/client_http.go`, `client_grpc.go`, `client_sdk.go`
- E2E harness: `tests/e2e.sh` (`run_mode` function with `patch_inventory_mode`)
- CI gate: `make e2e-all` (invoked by `.github/workflows/ci.yml` e2e job)
