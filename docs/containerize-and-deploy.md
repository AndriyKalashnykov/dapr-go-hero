# Containerize & Deploy Go Dapr Services to Kubernetes

A reusable migration plan for moving any Go + Dapr application from local development to a fully containerized, Kubernetes-deployable project with all major Dapr features enabled.

## When to Use This Plan

- Go project with one or more services using Dapr building blocks
- Currently runs locally with `dapr run` and local component files
- Needs Docker images, K8s manifests, and CI/CD for Kubernetes deployment

---

## Phase 1: Externalize Configuration

**Goal:** Move all hardcoded hosts, ports, and addresses to environment variables. Load a `.env` file on startup (non-overriding) so `go run` works without `dapr run`.

### `pkg/config/config.go` template (generic for any Go + Dapr project)

The `defaults` slice is the single source of truth. Add entries for every listen address, remote endpoint, or tunable your services need. At minimum, include the Dapr sidecar ports so `go run` works standalone.

```go
// Package config provides environment-based configuration with sensible defaults.
package config

import (
    "os"

    "github.com/joho/godotenv"
)

type entry struct{ Key, Default, Comment string }

// defaults is the single source of truth for every config key.
// Used at runtime (env fallback) and by a generator to keep .env in sync.
// Add one entry per tunable: APP listen ports, gRPC addresses, remote services, etc.
var defaults = []entry{
    // --- App-level (customize per project) ---
    // {"<KEY>", "<default>", "<description>"},

    // --- Dapr sidecar ports (standard defaults — usually no need to change) ---
    {"DAPR_HTTP_PORT", "3500", "Dapr sidecar HTTP API port"},
    {"DAPR_GRPC_PORT", "50001", "Dapr sidecar gRPC API port"},

    // --- Dapr app IDs (for service invocation) ---
    // Local dev: use bare app IDs ("<callee>").
    // K8s deployment: override to fully-qualified IDs ("<callee>.<namespace>")
    // when caller and callee live in different namespaces.
    {"<CALLEE>_APP_ID", "<callee>", "Dapr app ID of the <callee> service"},
}

func init() {
    // Load .env if present; never overwrites existing env vars.
    _ = godotenv.Load()
}

// Env returns the value of the named environment variable, or fallback if unset/empty.
func Env(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

// Exported config vars — one per key in defaults. Initialized after init() loads .env.
var (
    DaprHTTPPort = Env("DAPR_HTTP_PORT", "3500")
    DaprGRPCPort = Env("DAPR_GRPC_PORT", "50001")

    // Dapr app IDs — use fully-qualified "<id>.<namespace>" in K8s cross-namespace setups.
    // CalleeAppID = Env("<CALLEE>_APP_ID", "<callee>")

    // Add project-specific vars here, each matching an entry in `defaults`.
)

// Defaults exposes all config entries for external tooling (.env generation).
func Defaults() []struct{ Key, Default, Comment string } {
    out := make([]struct{ Key, Default, Comment string }, len(defaults))
    for i, e := range defaults {
        out[i] = struct{ Key, Default, Comment string }{e.Key, e.Default, e.Comment}
    }
    return out
}
```

**Rules for adding new config:**
1. Add an entry to `defaults` with `{KEY, default_value, "description"}`
2. Add a matching exported variable: `var MyVar = Env("KEY", "default_value")`
3. Run `make generate-env` to sync `.env`
4. Add the env var to the K8s deployment's `env:` block for each service that needs it

### `.env` generator (`cmd/generate-env/main.go`)

Generic — works for any project using the template above. Replace the import path only.

```go
// Command generate-env writes a .env file from pkg/config defaults.
package main

import (
    "fmt"
    "os"
    "strings"

    "<module-path>/pkg/config"
)

func main() {
    var b strings.Builder
    b.WriteString("# Generated from pkg/config defaults. Regenerate with: make generate-env\n")
    b.WriteString("# Loaded by godotenv on startup. Existing env vars take precedence.\n\n")
    for _, e := range config.Defaults() {
        if e.Comment != "" {
            b.WriteString("# " + e.Comment + "\n")
        }
        b.WriteString(e.Key + "=" + e.Default + "\n\n")
    }
    if err := os.WriteFile(".env", []byte(b.String()), 0644); err != nil { // #nosec G306
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println(".env generated.")
}
```

### Usage in service code

Anywhere you previously had a hardcoded port or address:

```go
// BEFORE
app.Listen(":8080")
lis, _ := net.Listen("tcp", "127.0.0.1:50051")

// AFTER
import "<module-path>/pkg/config"

app.Listen(config.AppPort)
lis, _ := net.Listen("tcp", config.GRPCAddr) // #nosec G102 -- K8s requires 0.0.0.0
```

Add `github.com/joho/godotenv` to `go.mod`:
```bash
go get github.com/joho/godotenv
```

### Checklist

- [ ] Every `net.Listen`/`fiber.Listen`/`grpc.NewService` uses a config variable
- [ ] No hardcoded `127.0.0.1` or `localhost` in service startup code
- [ ] Services bind to `0.0.0.0` (K8s sidecar needs to reach the app)
- [ ] `#nosec G102` annotations on `0.0.0.0` bindings with justification
- [ ] `.env` is in `.gitignore`? No — commit it for defaults, exclude from Docker images

---

## Phase 2: Dockerfiles (Multi-stage Alpine + BuildKit)

### Per-service template

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /app ./cmd/<service>/main.go

FROM alpine:3.21

# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S app -G app

USER app
COPY --from=builder /app /app

EXPOSE <ports>
ENTRYPOINT ["/app"]
```

**Gotchas:**
- `# syntax=docker/dockerfile:1` enables BuildKit features (cache mounts)
- `# hadolint ignore=DL3018` for `ca-certificates` — pinning system package versions is fragile
- Cache mounts make repeat builds ~0.2s (vs 30-60s cold)

### `.dockerignore`

```
.git
.github
.env
.mise.toml
.golangci.yml
*.md
docs/
k8s/
tests/
kind-config.yaml
cmd/*/main
```

Critical: exclude `.env` — K8s manifests provide env vars via `env:` blocks, not bundled files.

---

## Phase 3: Kubernetes Manifests

### Namespace strategy (per-service RBAC)

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: <project>
  labels: { app.kubernetes.io/part-of: <project> }
---
apiVersion: v1
kind: Namespace
metadata:
  name: <project>-<service-a>
  labels:
    app.kubernetes.io/part-of: <project>
    app.kubernetes.io/component: <service-a>
```

### RBAC template

```yaml
apiVersion: v1
kind: ServiceAccount
metadata: { name: <service>, namespace: <project>-<service> }
---
# Only needed if service uses secretstores.kubernetes
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: { name: secret-reader, namespace: <project>-<service> }
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: { name: <service>-secret-reader, namespace: <project>-<service> }
subjects:
  - { kind: ServiceAccount, name: <service>, namespace: <project>-<service> }
roleRef: { kind: Role, name: secret-reader, apiGroup: rbac.authorization.k8s.io }
```

### Deployment with Dapr annotations + env from config

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: <service>, namespace: <project>-<service> }
spec:
  replicas: 1
  selector: { matchLabels: { app: <service> } }
  template:
    metadata:
      labels: { app: <service> }
      annotations:
        dapr.io/enabled: "true"
        dapr.io/app-id: "<service>"
        dapr.io/app-port: "<app-port>"
        dapr.io/app-protocol: "http"  # or "grpc"
        dapr.io/config: "dapr-config"
        dapr.io/log-level: "info"
        dapr.io/log-as-json: "true"
        dapr.io/sidecar-cpu-request: "50m"
        dapr.io/sidecar-memory-request: "64Mi"
        dapr.io/sidecar-cpu-limit: "200m"
        dapr.io/sidecar-memory-limit: "128Mi"
    spec:
      serviceAccountName: <service>
      containers:
        - name: <service>
          image: <registry>/<service>:<tag>
          imagePullPolicy: IfNotPresent  # critical for KinD local images
          ports:
            - { name: api, containerPort: <app-port> }
          env:
            # Mirror every pkg/config key here — this is the K8s source of truth
            - { name: APP_PORT, value: ":<app-port>" }
            - { name: GRPC_ADDR, value: "0.0.0.0:<grpc-port>" }
            # Fully-qualified app ID for cross-namespace service invocation
            # (local dev uses bare "<callee>"; K8s uses "<callee>.<callee-namespace>")
            - { name: "<CALLEE>_APP_ID", value: "<callee>.<project>-<callee>" }
          resources:
            requests: { cpu: 100m, memory: 64Mi }
            limits: { cpu: 500m, memory: 256Mi }
```

### Dapr CRD gotchas (Dapr v1.17)

Validation errors we hit during implementation:

**Configuration mTLS — requires `controlPlaneTrustDomain` and `sentryAddress`:**
```yaml
spec:
  mtls:
    enabled: true
    workloadCertTTL: "24h"
    allowedClockSkew: "15m"
    controlPlaneTrustDomain: "cluster.local"
    sentryAddress: "dapr-sentry.dapr-system.svc:443"
```

**Resiliency — timeouts are strings directly, NOT nested `duration:`:**
```yaml
spec:
  policies:
    timeouts:
      service-invocation: 10s      # CORRECT
      # service-invocation: { duration: 10s }   # WRONG — validation error
```

**Component secretstores.kubernetes — `metadata: []` required even when empty:**
```yaml
spec:
  type: secretstores.kubernetes
  version: v1
  metadata: []  # Required by CRD validation
```

**Access control policies — each policy needs its own `trustDomain`:**
```yaml
accessControl:
  defaultAction: deny
  trustDomain: "cluster.local"
  policies:
    - appId: <caller>
      trustDomain: "cluster.local"   # REQUIRED per-policy
      namespace: "<caller-namespace>"
      defaultAction: allow
      operations:
        - name: /<target>/*
          action: allow
```

**Cross-namespace service invocation — CRITICAL gotcha:**

Dapr's default name resolver in K8s is **namespace-scoped**. When an app invokes another with just a bare app ID:

```go
ctx = dapr.InvokingContext(ctx, "callee")  // bare app ID
resp, err := client.SomeMethod(ctx, req)
```

Dapr resolves the callee's sidecar by appending the **caller's** namespace:
```
callee-dapr.<caller-namespace>.svc.cluster.local  // WRONG namespace!
```

If caller and callee are in different namespaces, DNS lookup fails with:
```
ERR_DIRECT_INVOKE: failed to resolve address for 'callee-dapr.<caller-ns>.svc.cluster.local':
no such host
```

The error is often hidden behind Dapr's resiliency retry logic and circuit breaker — you'll see `DROP status returned from app` in the caller sidecar logs, with the actual DNS error only visible at `log-level: debug`.

**Fix:** Use fully-qualified app IDs (`<app-id>.<namespace>`) for cross-namespace invocation:

```go
// In pkg/config/config.go — configurable per environment
var CalleeAppID = Env("CALLEE_APP_ID", "callee")  // local dev default

// In callee repository code
ctx = dapr.InvokingContext(ctx, config.CalleeAppID)
```

```yaml
# In caller's K8s deployment — override for cross-namespace
env:
  - name: CALLEE_APP_ID
    value: "callee.<callee-namespace>"
```

Local dev (single process + single Dapr) keeps using `callee`. K8s deployment with services in separate namespaces uses `callee.<callee-namespace>`. Same binary, different config.

**How to debug:** Enable `dapr.io/log-level: "debug"` annotation on the caller pod, publish a test event, grep sidecar logs for `ERR_DIRECT_INVOKE` or `failed to resolve`.

**K8s Secret structure for Dapr secretstore:** Dapr returns `secret.Data` as the Secret's `data` map directly. Each field must be a top-level key, not nested JSON:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: postgres  # This name is what the app passes as `secretName`
  namespace: <service-namespace>
type: Opaque
stringData:
  # Each field at top level — Dapr returns these as the struct fields
  host: "postgres.<project>.svc.cluster.local:5432"
  username: "postgres"
  password: "postgres"
  database: "postgres"
```

**Subscription v2alpha1 with CEL routing:**
```yaml
apiVersion: dapr.io/v2alpha1
kind: Subscription
metadata: { name: <name>, namespace: <namespace> }
spec:
  pubsubname: pubsub
  topic: <topic>
  routes:
    rules:
      - match: event.type == "widget.v1"
        path: /widgets.v1
    default: /default
scopes: [<app-id>]
```

---

## Phase 4: KinD + MetalLB

### `kind-config.yaml`

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000
        hostPort: 3000
        protocol: TCP
```

### MetalLB IP pool template (`k8s/metallb-config.yaml`)

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata: { name: default, namespace: metallb-system }
spec:
  addresses:
    - METALLB_IP_SUB.255.200-METALLB_IP_SUB.255.250
  autoAssign: true
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata: { name: default, namespace: metallb-system }
spec:
  ipAddressPools: [default]
```

Substitute `METALLB_IP_SUB` at deploy time:
```bash
ip_sub=$(docker network inspect kind -f '{{index .IPAM.Config 0 "Subnet"}}' \
  | awk -F. '{printf "%d.%d\n", $1, $2}')
sed "s/METALLB_IP_SUB/${ip_sub}/g" k8s/metallb-config.yaml | kubectl apply -f -
```

**CI gotcha:** In act/GitHub runners, Docker network inspection can produce an invalid subnet. E2E tests should prefer `kubectl port-forward` over the LoadBalancer IP (more portable).

---

## Phase 5: Makefile Targets

### Version constants

```makefile
# renovate: datasource=github-releases depName=kubernetes-sigs/kind
KIND_VERSION     := 0.31.0
KIND_NODE_IMAGE  := v1.35.0
# renovate: datasource=github-releases depName=metallb/metallb
METALLB_VERSION  := 0.15.3
# renovate: datasource=github-releases depName=hadolint/hadolint
HADOLINT_VERSION := 2.12.0

CLUSTER_NAME := <project>
REGISTRY     ?= <project>
TAG          ?= dev
NS_INFRA     := <project>
NS_A         := <project>-<service-a>
NS_B         := <project>-<service-b>
```

### Docker targets

```makefile
#docker-build: @ Build container images for all services
docker-build:
	@DOCKER_BUILDKIT=1 docker build -t $(REGISTRY)/<service-a>:$(TAG) -f Dockerfile.<service-a> .
	@DOCKER_BUILDKIT=1 docker build -t $(REGISTRY)/<service-b>:$(TAG) -f Dockerfile.<service-b> .

#docker-push: @ Push container images to registry
docker-push:
	@docker push $(REGISTRY)/<service-a>:$(TAG)
	@docker push $(REGISTRY)/<service-b>:$(TAG)

#docker-lint: @ Lint Dockerfiles with hadolint
docker-lint: deps-hadolint
	@hadolint Dockerfile.<service-a> Dockerfile.<service-b>

#deps-hadolint: @ Install hadolint
deps-hadolint:
	@command -v hadolint >/dev/null 2>&1 || { \
		curl -sSfL -o /tmp/hadolint \
			"https://github.com/hadolint/hadolint/releases/download/v$(HADOLINT_VERSION)/hadolint-Linux-x86_64"; \
		chmod +x /tmp/hadolint && sudo mv /tmp/hadolint /usr/local/bin/hadolint; }
```

### KinD + K8s targets

```makefile
#kind-up: @ Create KinD cluster with MetalLB and Dapr
kind-up:
	@command -v kind >/dev/null 2>&1 || { echo "Error: kind not installed"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl not installed"; exit 1; }
	@kind create cluster --config=kind-config.yaml --name $(CLUSTER_NAME) \
		--image=kindest/node:$(KIND_NODE_IMAGE) --wait 60s 2>/dev/null || true
	@echo "=== Installing MetalLB ==="
	@kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v$(METALLB_VERSION)/config/manifests/metallb-native.yaml
	@kubectl rollout status deployment/controller -n metallb-system --timeout=180s
	@kubectl rollout status daemonset/speaker -n metallb-system --timeout=180s
	@ip_sub=$$(docker network inspect kind -f '{{index .IPAM.Config 0 "Subnet"}}' | awk -F. '{printf "%d.%d\n", $$1, $$2}'); \
		sed "s/METALLB_IP_SUB/$$ip_sub/g" k8s/metallb-config.yaml | kubectl apply -f -
	@echo "=== Installing Dapr ==="
	@command -v dapr >/dev/null 2>&1 || { echo "Error: dapr CLI not installed"; exit 1; }
	@dapr init -k --wait --timeout 300 2>/dev/null || true
	@dapr dashboard -k -p 0 2>/dev/null &

#kind-down: @ Destroy KinD cluster
kind-down:
	@kind delete cluster --name $(CLUSTER_NAME) 2>/dev/null || true

#k8s-deploy: @ Build images, load into KinD, apply manifests
k8s-deploy: docker-build
	@kind load docker-image $(REGISTRY)/<service-a>:$(TAG) --name $(CLUSTER_NAME)
	@kind load docker-image $(REGISTRY)/<service-b>:$(TAG) --name $(CLUSTER_NAME)
	@kubectl apply -f k8s/namespace.yaml
	@kubectl apply -f k8s/rbac.yaml
	@kubectl apply -f k8s/redis.yaml
	@kubectl apply -f k8s/postgres.yaml
	@kubectl apply -f k8s/zipkin.yaml
	@kubectl apply -f k8s/dapr/
	@kubectl apply -f k8s/<service-a>-deployment.yaml
	@kubectl apply -f k8s/<service-a>-service.yaml
	@kubectl apply -f k8s/<service-b>-deployment.yaml
	@kubectl apply -f k8s/<service-b>-service.yaml
	@kubectl rollout status deployment/<service-a> -n $(NS_A) --timeout=180s
	@kubectl rollout status deployment/<service-b> -n $(NS_B) --timeout=180s

#k8s-undeploy: @ Remove all K8s manifests
k8s-undeploy:
	@kubectl delete -f k8s/<service-b>-service.yaml --ignore-not-found
	@kubectl delete -f k8s/<service-b>-deployment.yaml --ignore-not-found
	@kubectl delete -f k8s/<service-a>-service.yaml --ignore-not-found
	@kubectl delete -f k8s/<service-a>-deployment.yaml --ignore-not-found
	@kubectl delete -f k8s/dapr/ --ignore-not-found
	@kubectl delete -f k8s/zipkin.yaml --ignore-not-found
	@kubectl delete -f k8s/postgres.yaml --ignore-not-found
	@kubectl delete -f k8s/redis.yaml --ignore-not-found
	@kubectl delete -f k8s/rbac.yaml --ignore-not-found
	@kubectl delete -f k8s/namespace.yaml --ignore-not-found

#k8s-status: @ Show pod/service status across namespaces
k8s-status:
	@echo "=== $(NS_INFRA) ==="; kubectl get pods,svc -n $(NS_INFRA) 2>/dev/null || true
	@echo "=== $(NS_A) ==="; kubectl get pods,svc -n $(NS_A) 2>/dev/null || true
	@echo "=== $(NS_B) ==="; kubectl get pods,svc -n $(NS_B) 2>/dev/null || true
```

### E2E targets

```makefile
#e2e: @ Run end-to-end tests against running cluster
e2e:
	@./tests/e2e.sh

#e2e-setup: @ Full setup: kind-up + k8s-deploy
e2e-setup: kind-up k8s-deploy

#e2e-teardown: @ Full teardown
e2e-teardown: k8s-undeploy kind-down

#generate-env: @ Regenerate .env from pkg/config defaults
generate-env:
	@go run ./cmd/generate-env
```

Add all new targets to `.PHONY`.

---

## Phase 6: E2E Test Script (`tests/e2e.sh`)

### Critical bash patterns

```bash
#!/usr/bin/env bash
set -euo pipefail

PASS=0
FAIL=0
PF_PID=""

cleanup() {
  if [[ -n "${PF_PID}" ]]; then
    kill "${PF_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

CURL_TIMEOUT=5  # CRITICAL: curl defaults to no timeout — will hang forever

assert_status() {
  local desc="$1" url="$2" expected="$3"
  local status
  status=$(curl -s --max-time ${CURL_TIMEOUT} -o /dev/null -w '%{http_code}' "${url}" 2>/dev/null || echo "000")
  if [[ "${status}" == "${expected}" ]]; then
    echo "  PASS: ${desc} (HTTP ${status})"
    # NOTE: ((PASS++)) returns 0 when PASS was 0 — triggers set -e!
    # Use arithmetic expansion instead:
    PASS=$((PASS+1))
  else
    echo "  FAIL: ${desc} — expected HTTP ${expected}, got ${status}"
    FAIL=$((FAIL+1))
  fi
}

assert_json_field() {
  local desc="$1" url="$2" field="$3" expected="$4"
  local value
  value=$(curl -sf --max-time ${CURL_TIMEOUT} "${url}" 2>/dev/null | jq -r ".${field}" 2>/dev/null || echo "")
  if [[ "${value}" == "${expected}" ]]; then
    echo "  PASS: ${desc} (.${field} = ${value})"
    PASS=$((PASS+1))
  else
    echo "  FAIL: ${desc} — expected .${field}=${expected}, got ${value}"
    FAIL=$((FAIL+1))
  fi
}
```

### Two critical bash gotchas we hit

1. **`((PASS++))` + `set -e` = silent exit** — The post-increment operator returns the pre-value. When PASS=0, the expression evaluates to 0, which bash treats as "false" and `set -e` exits. Use `PASS=$((PASS+1))` instead.

2. **`curl` without `--max-time` hangs indefinitely** — On unreachable endpoints (e.g., MetalLB assigning an unreachable IP inside act), curl waits for TCP connect timeout which is minutes. Always set `--max-time N`.

### Test flow

```bash
# 1. Wait for pods to be Ready (Dapr sidecars included)
kubectl wait pods -n "${NAMESPACE}" -l app=<service> \
  --for condition=Ready --timeout=180s

# 2. Port-forward to Dapr sidecar (more portable than LoadBalancer IP)
POD=$(kubectl get pod -n "${NAMESPACE}" -l app=<service> \
  -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward "pod/${POD}" 3500:3500 -n "${NAMESPACE}" &
PF_PID=$!
sleep 3

# 3. Publish CloudEvents
curl -sf -X POST http://localhost:3500/v1.0/publish/pubsub/<topic> \
  -H "Content-Type: application/cloudevents+json" \
  -d '{"specversion":"1.0","type":"event.v1","source":"e2e",
       "id":"e2e-1","data":{"id":"test","value":42}}'

# 4. Wait for async processing
sleep 10

# 5. Port-forward to REST API (avoid LoadBalancer IP issues in CI)
kubectl port-forward svc/<service> 3000:3000 -n "${NAMESPACE}" &
sleep 3

# 6. Assert REST responses
assert_status "GET /v1/items/test" "http://localhost:3000/v1/items/test" "200"
assert_json_field "value" "http://localhost:3000/v1/items/test" "value" "42"

# 7. Report
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[[ ${FAIL} -ne 0 ]] && { echo "E2E FAILED"; exit 1; }
echo "E2E PASSED"
```

---

## Phase 7: GitHub Actions CI/CD

### Parallel `docker-lint` job

```yaml
docker-lint:
  runs-on: ubuntu-latest
  timeout-minutes: 5
  steps:
    - name: Checkout
      uses: actions/checkout@<pinned-sha>
    - name: Lint Dockerfiles
      run: make docker-lint
```

### Full `e2e` job (KinD + MetalLB + Dapr)

```yaml
e2e:
  needs: [build, test]
  runs-on: ubuntu-latest
  timeout-minutes: 20
  steps:
    - name: Checkout
      uses: actions/checkout@<pinned-sha>

    - name: Install Go
      uses: actions/setup-go@<pinned-sha>
      with:
        go-version-file: go.mod
        cache: true

    - name: Create KinD cluster
      uses: helm/kind-action@v1.14.0
      with:
        version: v0.31.0
        cluster_name: <project>
        config: ./kind-config.yaml
        node_image: kindest/node:v1.35.0

    - name: Install MetalLB
      run: |
        kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.15.3/config/manifests/metallb-native.yaml
        kubectl rollout status deployment/controller -n metallb-system --timeout=180s
        kubectl rollout status daemonset/speaker -n metallb-system --timeout=180s
        ip_sub=$(docker network inspect kind -f '{{index .IPAM.Config 0 "Subnet"}}' | awk -F. '{printf "%d.%d\n", $1, $2}')
        sed "s/METALLB_IP_SUB/${ip_sub}/g" k8s/metallb-config.yaml | kubectl apply -f -

    - name: Install Dapr
      uses: dapr/setup-dapr@v2
      with:
        version: '1.17.1'

    - name: Initialize Dapr on cluster
      run: dapr init -k --wait --timeout 300

    - name: Build and load images
      run: |
        make docker-build
        kind load docker-image <project>/<service-a>:dev --name <project>
        kind load docker-image <project>/<service-b>:dev --name <project>

    - name: Deploy to KinD
      run: |
        kubectl apply -f k8s/namespace.yaml
        kubectl apply -f k8s/rbac.yaml
        kubectl apply -f k8s/redis.yaml
        kubectl apply -f k8s/postgres.yaml
        kubectl apply -f k8s/zipkin.yaml
        kubectl apply -f k8s/dapr/
        kubectl apply -f k8s/<service-a>-deployment.yaml
        kubectl apply -f k8s/<service-a>-service.yaml
        kubectl apply -f k8s/<service-b>-deployment.yaml
        kubectl apply -f k8s/<service-b>-service.yaml
        kubectl rollout status deployment/<service-a> -n <project>-<service-a> --timeout=180s
        kubectl rollout status deployment/<service-b> -n <project>-<service-b> --timeout=180s

    - name: Run E2E tests
      run: make e2e

    - name: Debug on failure
      if: failure()
      run: |
        echo "=== Pod status ==="
        kubectl get pods -A
        echo "=== <service-a> app logs ==="
        kubectl logs -n <project>-<service-a> -l app=<service-a> -c <service-a> --tail=100 || true
        echo "=== <service-a> sidecar logs (filtered for errors) ==="
        kubectl logs -n <project>-<service-a> -l app=<service-a> -c daprd --tail=200 \
          | grep -iE 'level.*(error|warn)|ERR_DIRECT|failed to resolve|DROP status' \
          | grep -v Scheduler | tail -30 || true
        echo "=== <service-b> app logs ==="
        kubectl logs -n <project>-<service-b> -l app=<service-b> -c <service-b> --tail=100 || true
        echo "=== <service-b> sidecar logs (filtered for errors) ==="
        kubectl logs -n <project>-<service-b> -l app=<service-b> -c daprd --tail=200 \
          | grep -iE 'level.*(error|warn)|ERR_DIRECT|failed to resolve|DROP status' \
          | grep -v Scheduler | tail -30 || true
```

### CI hierarchy

```
static-check (lint, sec, tidy)    ┐
docker-lint (hadolint)            ┤  parallel
                                  ┘
build (depends on static-check)   ┐
test  (depends on static-check)   ┤  parallel after static-check
                                  ┘
e2e (depends on build + test)     — KinD, MetalLB, Dapr, deploy, test
```

---

## Debugging Workflow

**Debug E2E failures standalone first, then verify in CI.** `act` adds a slow feedback loop (~7min per iteration) and obscures logs behind its own output framing. Prefer:

1. `make kind-up` → create cluster once locally
2. `make k8s-deploy` → deploy (fast iteration on image + manifest changes)
3. `bash ./tests/e2e.sh` → run tests directly
4. `kubectl logs -n <ns> -l app=<svc> -c <container>` → trace specific hops
5. Enable `dapr.io/log-level: "debug"` annotation temporarily for sidecar errors
6. Once green standalone, `make kind-down && make ci-run` to verify in act

**Isolate the failing hop.** Dapr's resiliency (retries + circuit breakers) masks root causes. To bypass the resiliency layer during debugging, invoke the target directly from inside the caller pod:

```bash
# Shell into caller pod
kubectl exec -n <caller-ns> deploy/<caller> -c <caller> -- sh
# Publish a single event via local sidecar
wget -qO- --header "dapr-app-id: <callee>.<callee-ns>" --post-data="{...}" \
  http://localhost:3500/v1.0/invoke/<callee>.<callee-ns>/method/<Method>
```

**Trace pattern for service invocation failures:**

```bash
# 1. Did the caller app initiate the call?
kubectl logs -n <caller-ns> -l app=<caller> -c <caller> | grep Invoking

# 2. Did the caller sidecar attempt resolution?
kubectl logs -n <caller-ns> -l app=<caller> -c daprd | grep -E 'Endpoint Policy|invoke'

# 3. Did the callee sidecar receive the request?
kubectl logs -n <callee-ns> -l app=<callee> -c daprd | grep -E 'method|invoke'

# 4. Did the callee app receive the method call?
kubectl logs -n <callee-ns> -l app=<callee> -c <callee>
```

If step 2 shows `ERR_DIRECT_INVOKE: failed to resolve address` → cross-namespace app ID issue (see gotcha above). If step 3 has no entries → request didn't reach callee → resolver or access control. If step 4 has no entries but step 3 does → callee app not listening or wrong port.

---

## Lessons Learned (gotchas that cost debugging time)

| Symptom | Cause | Fix |
|---------|-------|-----|
| `bash: line 1: /scripts/gvm: No such file or directory` in act | `HAS_GVM` evaluates host state, not container | Guard with `[ -z "$CI" ] && [ -n "$GVM_ROOT" ]` |
| `Configuration.mtls: controlPlaneTrustDomain required` | Dapr v1.17 validation stricter | Set `controlPlaneTrustDomain: cluster.local` + `sentryAddress` |
| `Resiliency: unknown field "spec.policies.timeouts.X.duration"` | Schema changed — timeouts are strings now | Use `service-invocation: 10s` directly |
| `Component: spec.metadata: Required value` | `secretstores.kubernetes` requires metadata | Set `metadata: []` explicitly |
| `missing trustdomain for apps` | Access control policy needs per-policy trustDomain | Add `trustDomain: cluster.local` to each policy |
| `secrets "postgres" not found` | K8s Secret name must match `secretName` arg | Rename Secret to match app's `GetSecret(..., "postgres", ...)` |
| Wrong values unmarshaled from Dapr secret | Dapr returns Secret `data` map directly, not nested JSON | Flatten stringData fields to match struct fields |
| `DROP status returned from app` during service invocation | Cross-namespace call with bare app ID; DNS fails for `callee-dapr.<caller-ns>` | Use fully-qualified app ID `callee.<callee-ns>` (see Cross-namespace section) |
| `ERR_DIRECT_INVOKE: failed to resolve address` hidden by resiliency retries | Circuit breaker opens before root cause surfaces | Set `dapr.io/log-level: "debug"` temporarily and grep for `failed to resolve` |
| E2E exits after first PASS with set -e | `((PASS++))` returns 0 when PASS=0 → triggers set -e | Use `PASS=$((PASS+1))` |
| curl hangs for minutes on unreachable LoadBalancer IP | No default timeout | Always use `curl --max-time N` |
| LoadBalancer IP `0.0.255.200` in act/CI | Docker network subnet detection unreliable in act | Use `kubectl port-forward` in E2E — more portable |

---

## Decision Matrix

| Decision | Recommendation | Rationale |
|----------|----------------|-----------|
| Base image | Alpine 3.x | Debugging shell, small (~5MB), CA certs via apk |
| K8s format | Plain YAML | Transparent, works with `kubectl apply -f` |
| Namespace | Per-service | Demonstrates RBAC, access control, isolation |
| Secret store | `secretstores.kubernetes` | K8s-native, works with RBAC |
| E2E in CI | Every PR + main | Catches regressions; ~3-5min overhead |
| Local cluster | KinD + MetalLB | Lightest, best CI support |
| E2E REST access | `kubectl port-forward` | More portable than LoadBalancer IP (esp. in CI) |
| Dapr in CI | `dapr/setup-dapr@v2` + `dapr init -k` | Official action, simplest path |

## Prerequisites

- Docker with BuildKit
- KinD (`go install sigs.k8s.io/kind@latest`)
- kubectl
- Dapr CLI (https://docs.dapr.io/getting-started/install-dapr-cli/)

## References

- [Dapr on Kubernetes](https://docs.dapr.io/operations/hosting/kubernetes/)
- [Dapr Component Specs](https://docs.dapr.io/reference/components-reference/)
- [Dapr Resiliency](https://docs.dapr.io/operations/resiliency/)
- [Dapr Access Control](https://docs.dapr.io/operations/configuration/invoke-allowlist/)
- [Dapr Subscription Routing](https://docs.dapr.io/developing-applications/building-blocks/pubsub/howto-route-messages/)
- [Dapr Annotations Reference](https://docs.dapr.io/reference/arguments-annotations-overview/)
- [KinD Quick Start](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [MetalLB in KinD](https://kind.sigs.k8s.io/docs/user/loadbalancer/)
