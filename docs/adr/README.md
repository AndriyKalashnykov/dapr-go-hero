# Architecture Decision Records

This directory holds [Architecture Decision Records](https://adr.github.io/) (ADRs) — short markdown documents that capture why a non-obvious architectural choice was made. Each ADR is immutable once Accepted; superseding an ADR creates a new file that references the old one in its Status line.

Format loosely based on [MADR](https://adr.github.io/madr/) — see `0000-template.md` for the template.

## Index

| # | Title | Status |
|---|-------|--------|
| [0001](0001-adopt-dapr-building-blocks.md) | Adopt Dapr building blocks for cross-service concerns | Accepted |
| [0002](0002-cloud-provider-kind-over-metallb.md) | Use cloud-provider-kind (not MetalLB) for LoadBalancer support in KinD | Accepted |
| [0003](0003-four-client-modes.md) | Ship four Dapr client implementations side-by-side | Accepted |

## Authoring a new ADR

1. Copy `0000-template.md` → `NNNN-kebab-title.md` (next sequential number)
2. Fill in Context, Decision, Consequences, Alternatives
3. Add an entry to the Index table above
4. Cross-link from any diagram caption or README section that reflects the decision
5. Commit under a `docs(adr):` conventional-commit prefix
