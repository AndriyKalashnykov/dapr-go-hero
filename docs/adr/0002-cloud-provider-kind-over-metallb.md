# ADR-0002: Use cloud-provider-kind (not MetalLB) for LoadBalancer support in KinD

- **Status**: Accepted (supersedes the MetalLB configuration used prior to 2026-04-16)
- **Date**: 2026-04-16
- **Deciders**: project author

## Context

`make kind-up` needs `Service` objects of type `LoadBalancer` to expose inventory and products externally for the e2e curl suite. A vanilla KinD cluster has no cloud provider, so LoadBalancer IPs stay in `<pending>` forever without a controller that watches those Services and allocates IPs on the host's `kind` Docker network.

The two established options are:

1. **MetalLB** — mature, runs in-cluster as a Deployment + DaemonSet, configured via `IPAddressPool` + `L2Advertisement` CRDs.
2. **cloud-provider-kind** — kind-team-maintained host-side controller that runs as a Docker container on the `kind` Docker network and watches Services via the Docker socket.

The project used MetalLB from initial setup until 2026-04-16.

## Decision

Switch to **cloud-provider-kind**.

## Consequences

- **Positive**:
  - Kind-team-maintained (`kubernetes-sigs/cloud-provider-kind`) — new `kindest/node` images are supported day-one. MetalLB releases on an independent cadence; MetalLB 0.15.3 refused to reach the K8s API on `kindest/node:v1.35.0` due to an nftables regression, stranding projects on v1.34.x for months.
  - Simpler Makefile: one `docker run --network kind ... cloud-provider-kind/cloud-controller-manager` line replaces a kubectl-apply of the MetalLB manifest + two `kubectl rollout status` waits + a `docker network inspect | awk` subnet carve + a `sed` templating pass on `metallb-config.yaml`.
  - Smaller in-cluster footprint — no `metallb-system` namespace, no controller Deployment, no speaker DaemonSet.
- **Negative**:
  - Requires access to `/var/run/docker.sock` on the host (MetalLB required only `kubectl` access).
  - Not suitable for non-kind clusters (k3s, bare-metal, cloud) — those still need a cloud controller or MetalLB.
- **Neutral**:
  - LoadBalancer IP discovery remains identical from the application's perspective (`kubectl get svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}'`).

## Alternatives considered

| Option | Pros | Cons | Why not |
|--------|------|------|---------|
| MetalLB (previous choice) | Mature; prod-parity with teams that run MetalLB upstream | Independent release track → K8s-version drift; in-cluster footprint; more Makefile surface | Lost the parity argument when MetalLB's K8s support lagged KinD's |
| NodePort exposure | Zero controller overhead | Loses the LoadBalancer abstraction; e2e test logic has to handle port mapping per runtime | Breaks the cloud-parity goal of the stack |
| Skip LoadBalancer altogether | Simplest | E2E loses the external-IP hop that production uses | Would produce a less faithful integration test |

## Related

- Deployment diagram: `docs/diagrams/c4-deployment.puml`
- Makefile: `CLOUD_PROVIDER_KIND_VERSION`, `kind-create` target
- Deleted: `k8s/metallb-config.yaml` (2026-04-16)
- Upstream: https://github.com/kubernetes-sigs/cloud-provider-kind
