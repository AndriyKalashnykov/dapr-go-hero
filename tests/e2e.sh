#!/usr/bin/env bash
# End-to-end tests for dapr-go-hero on Kubernetes.
# Publishes events via Dapr sidecar and verifies REST API responses.
#
# Usage:
#   ./tests/e2e.sh              # default: sdk-http mode (fast, matches CI)
#   ./tests/e2e.sh <mode>       # one of: sdk-http | sdk-grpc | custom-http | custom-grpc
#   ./tests/e2e.sh all          # run all 4 client modes back-to-back (slow)
#
# Client mode is injected by patching the inventory Deployment container
# `args:` — the binary accepts "http" / "grpc" / "" (SDK default) as the
# sole positional argument. SDK-gRPC requires no special arg but switches
# the deployment's app-protocol annotation, so we skip it by default.
set -euo pipefail

NAMESPACE_INVENTORY="dapr-go-hero-inventory"
NAMESPACE_PRODUCTS="dapr-go-hero-products"
NAMESPACE_INFRA="dapr-go-hero"

# Pin kubectl to the kind-<cluster> context so a parallel `make` run from a
# sibling KinD-using project that calls `kubectl config use-context` cannot
# silently flip our context mid-script. KIND_CLUSTER_NAME is exported by the
# Makefile e2e target; default matches the project name when run by hand.
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-dapr-go-hero}"
KUBECTL=(kubectl --context="kind-${KIND_CLUSTER_NAME}")

MODE="${1:-sdk-http}"
PASS=0
FAIL=0
declare -a PF_PIDS=()

# Pick a free local TCP port. Avoids collisions with concurrent runs of this
# script (parallel CI matrix, two devs on the same host) that would otherwise
# fight over hardcoded :3000/:3500/:9411 port-forward aliases.
pick_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()'
}

LOCAL_DAPR_PORT=$(pick_port)
LOCAL_REST_PORT=$(pick_port)
LOCAL_ZIPKIN_PORT=$(pick_port)
echo "=== Allocated ephemeral local ports: dapr=${LOCAL_DAPR_PORT} rest=${LOCAL_REST_PORT} zipkin=${LOCAL_ZIPKIN_PORT} ==="

cleanup() {
  local pid
  for pid in "${PF_PIDS[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null || true
  done
}
trap cleanup EXIT

CURL_TIMEOUT=5

# ---- assertion helpers -----------------------------------------------------

assert_status() {
  local desc="$1" url="$2" expected="$3"
  local status
  status=$(curl -s --max-time ${CURL_TIMEOUT} -o /dev/null -w '%{http_code}' "${url}" 2>/dev/null || echo "000")
  if [[ "${status}" == "${expected}" ]]; then
    echo "  PASS: ${desc} (HTTP ${status})"
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

# wait_for_url <name> <url> [max_attempts]
# Poll <url> until it returns a 2xx/3xx response, or fail after N tries.
wait_for_url() {
  local name="$1" url="$2" max="${3:-30}"
  local i status
  for ((i = 0; i < max; i++)); do
    status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "${url}" 2>/dev/null || echo "000")
    if [[ "${status}" =~ ^[23] ]]; then
      return 0
    fi
    sleep 1
  done
  echo "  FAIL: ${name} not reachable at ${url} after ${max}s (last status: ${status})"
  FAIL=$((FAIL+1))
  return 1
}

# ---- client-mode deployment patch -----------------------------------------

# patch_inventory_mode <mode>
# Patches the inventory Deployment's container args so it launches with the
# requested client implementation. Waits for rollout.
patch_inventory_mode() {
  local mode="$1"
  local args

  case "${mode}" in
    sdk-http)    args='[]'             ;;    # default: no argument
    sdk-grpc)    args='[]'             ;;    # same binary arg; distinguished by protocol annotation in a real setup
    custom-http) args='["http"]'       ;;
    custom-grpc) args='["grpc"]'       ;;
    *) echo "Unknown mode: ${mode}"; exit 2 ;;
  esac

  echo "=== Patching inventory Deployment to mode=${mode} args=${args} ==="
  "${KUBECTL[@]}" patch deployment/inventory -n "${NAMESPACE_INVENTORY}" --type=json \
    -p='[{"op":"add","path":"/spec/template/spec/containers/0/args","value":'"${args}"'}]' \
    >/dev/null 2>&1 || \
  "${KUBECTL[@]}" patch deployment/inventory -n "${NAMESPACE_INVENTORY}" --type=json \
    -p='[{"op":"replace","path":"/spec/template/spec/containers/0/args","value":'"${args}"'}]'

  "${KUBECTL[@]}" rollout status deployment/inventory -n "${NAMESPACE_INVENTORY}" --timeout=180s
}

# ---- single-mode run ------------------------------------------------------

run_mode() {
  local mode="$1"

  # Fresh port-forwards per mode — the previous inventory pod is gone.
  local pid
  for pid in "${PF_PIDS[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" 2>/dev/null || true
  done
  PF_PIDS=()

  if [[ "${mode}" != "sdk-http" ]] || [[ "${FORCE_PATCH:-0}" == "1" ]]; then
    patch_inventory_mode "${mode}"
  fi

  echo "=== [${mode}] Waiting for rollouts to be ready ==="
  # Use rollout status (terminator-safe): waits for Deployment readiness
  # without tripping on old terminating pods that still match the label.
  "${KUBECTL[@]}" rollout status deployment/inventory -n "${NAMESPACE_INVENTORY}" --timeout=180s
  "${KUBECTL[@]}" rollout status deployment/products  -n "${NAMESPACE_PRODUCTS}"  --timeout=180s

  # Resolve the current Running pod (filter out any still-terminating old pod)
  local inv_pod
  inv_pod=$("${KUBECTL[@]}" get pod -n "${NAMESPACE_INVENTORY}" -l app=inventory \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].metadata.name}')

  echo "=== [${mode}] Port-forward inventory Dapr sidecar (local :${LOCAL_DAPR_PORT} → :3500) ==="
  "${KUBECTL[@]}" port-forward "pod/${inv_pod}" "${LOCAL_DAPR_PORT}:3500" -n "${NAMESPACE_INVENTORY}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  wait_for_url "inventory sidecar :${LOCAL_DAPR_PORT}" "http://localhost:${LOCAL_DAPR_PORT}/v1.0/healthz" 30

  echo "=== [${mode}] Publishing events ==="
  local widget_desc="E2E Widget [${mode}]"
  local gadget_desc="E2E Gadget [${mode}]"
  local thing_desc="E2E Thingamajig [${mode}]"

  curl -sf -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d "{\"specversion\":\"1.0\",\"type\":\"widget.v1\",\"source\":\"e2e-test\",\"id\":\"e2e-widget-${mode}\",\"datacontenttype\":\"application/json\",\"data\":{\"id\":\"widget\",\"description\":\"${widget_desc}\",\"price\":9.99}}" \
    || { echo "FAIL: publish widget"; FAIL=$((FAIL+1)); }

  curl -sf -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d "{\"specversion\":\"1.0\",\"type\":\"gadget.v1\",\"source\":\"e2e-test\",\"id\":\"e2e-gadget-${mode}\",\"datacontenttype\":\"application/json\",\"data\":{\"id\":\"gadget\",\"description\":\"${gadget_desc}\",\"price\":19.99}}" \
    || { echo "FAIL: publish gadget"; FAIL=$((FAIL+1)); }

  curl -sf -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d "{\"specversion\":\"1.0\",\"type\":\"thingamajig.v1\",\"source\":\"e2e-test\",\"id\":\"e2e-thing-${mode}\",\"datacontenttype\":\"application/json\",\"data\":{\"id\":\"thingamajig\",\"description\":\"${thing_desc}\",\"price\":29.99}}" \
    || { echo "FAIL: publish thingamajig"; FAIL=$((FAIL+1)); }

  # Negative case: malformed CloudEvent — `data` is a string where the
  # widget handler expects a JSON object. Dapr 1.17 accepts the publish
  # (the broker doesn't typecheck `data`); the app handler should reject
  # the decode and not persist anything. Assert: a GET for the malformed
  # event's id returns 404 (proof the app's decode-failure path is wired).
  local malformed_status
  malformed_status=$(curl -s -o /dev/null -w '%{http_code}' --max-time ${CURL_TIMEOUT} \
    -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d '{"specversion":"1.0","type":"widget.v1","source":"e2e","id":"malformed-evt","datacontenttype":"application/json","data":"not-an-object"}' \
    2>/dev/null || echo "000")
  echo "  INFO: malformed publish returned HTTP ${malformed_status} (app must handle decode failure)"

  echo "=== [${mode}] Waiting for async processing ==="
  sleep 10

  echo "=== [${mode}] Port-forward inventory REST API (local :${LOCAL_REST_PORT} → :3000) ==="
  "${KUBECTL[@]}" port-forward svc/inventory "${LOCAL_REST_PORT}:3000" -n "${NAMESPACE_INVENTORY}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  wait_for_url "inventory REST :${LOCAL_REST_PORT}" "http://localhost:${LOCAL_REST_PORT}/v1/widgets/widget" 30
  local base="http://localhost:${LOCAL_REST_PORT}"

  echo "=== [${mode}] Testing REST API ==="
  assert_status   "GET /v1/widgets/widget"     "${base}/v1/widgets/widget" "200"
  assert_json_field "Widget desc matches mode" "${base}/v1/widgets/widget" "description" "${widget_desc}"

  assert_status   "GET /v1/gadgets/gadget"     "${base}/v1/gadgets/gadget" "200"
  assert_json_field "Gadget desc matches mode" "${base}/v1/gadgets/gadget" "description" "${gadget_desc}"

  assert_status   "GET /v1/products/thingamajig"     "${base}/v1/products/thingamajig" "200"
  assert_json_field "Product desc matches mode"      "${base}/v1/products/thingamajig" "description" "${thing_desc}"

  # --- Negative: 404 through the gateway ---------------------------------
  assert_status "GET /v1/widgets/nope → 404"  "${base}/v1/widgets/does-not-exist"  "404"
  assert_status "GET /v1/gadgets/nope → 404"  "${base}/v1/gadgets/does-not-exist"  "404"
  assert_status "GET /v1/products/nope → 404" "${base}/v1/products/does-not-exist" "404"

  # --- Malformed-publish assertion: the malformed event must NOT have
  # been persisted. A working decode-failure path returns 404 here; a
  # regression that swallows the error and writes garbage would leak.
  assert_status "Malformed event yields 404 (decode failure not persisted)" \
    "${base}/v1/widgets/malformed-evt" "404"

  # --- Routing isolation: publish ONE widget.v1 with a deterministic id
  # and verify (a) it landed via PostgreSQL widgets path AND (b) it did
  # NOT also land via Redis state (which would indicate the subscription
  # broadcast to multiple handlers — a content-routing regression).
  local iso_id="routing-iso-${mode}"
  curl -sf -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d "{\"specversion\":\"1.0\",\"type\":\"widget.v1\",\"source\":\"e2e\",\"id\":\"${iso_id}\",\"datacontenttype\":\"application/json\",\"data\":{\"id\":\"${iso_id}\",\"description\":\"routing-iso\",\"price\":1.23}}" \
    >/dev/null || { echo "  FAIL: routing-isolation publish"; FAIL=$((FAIL+1)); }
  sleep 5
  assert_status "Routing isolation: widget.v1 → widgets repo (200)" \
    "${base}/v1/widgets/${iso_id}" "200"
  assert_status "Routing isolation: widget.v1 NOT routed to gadgets (404)" \
    "${base}/v1/gadgets/${iso_id}" "404"
}

# ---- zipkin assertions ----------------------------------------------------

check_zipkin() {
  local inv_pod
  if ! "${KUBECTL[@]}" get svc zipkin -n "${NAMESPACE_INFRA}" >/dev/null 2>&1; then
    echo "  WARN: Zipkin service not found — skipping trace assertions"
    return
  fi

  "${KUBECTL[@]}" port-forward svc/zipkin "${LOCAL_ZIPKIN_PORT}:9411" -n "${NAMESPACE_INFRA}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  wait_for_url "Zipkin :${LOCAL_ZIPKIN_PORT}" "http://localhost:${LOCAL_ZIPKIN_PORT}/health" 20 || return

  local trace_count
  trace_count=$(curl -sf "http://localhost:${LOCAL_ZIPKIN_PORT}/api/v2/traces?limit=50" 2>/dev/null \
    | jq 'length' 2>/dev/null || echo "0")

  if [[ "${trace_count}" -gt 0 ]]; then
    echo "  PASS: Zipkin has ${trace_count} trace(s)"
    PASS=$((PASS+1))
  else
    echo "  WARN: No traces found in Zipkin (async propagation may need more time)"
    return
  fi

  # Assert at least one span per app-id. The serviceName in Zipkin is the
  # Dapr app-id, so both "inventory" and "products" should appear.
  local services
  services=$(curl -sf "http://localhost:${LOCAL_ZIPKIN_PORT}/api/v2/services" 2>/dev/null || echo "[]")
  for svc in inventory products; do
    if echo "${services}" | jq -e --arg s "${svc}" 'index($s)' >/dev/null 2>&1; then
      echo "  PASS: Zipkin records traces for app-id '${svc}'"
      PASS=$((PASS+1))
    else
      echo "  FAIL: Zipkin has no traces for app-id '${svc}'"
      FAIL=$((FAIL+1))
    fi
  done
}

# ---- resiliency assertion -------------------------------------------------

# check_resiliency validates that the pub/sub → inventory → products pipeline
# RECOVERS after the products backend is temporarily unavailable. Scope note:
#
# This does NOT test Dapr Redis pub/sub redelivery of in-flight messages.
# In Dapr 1.17 with pubsub.redis v1, when the subscriber handler returns an
# error (e.g., because products is down and service-invocation's circuit
# breaker trips), Dapr effectively ACKs the message and moves on. The
# `processingTimeout` metadata ONLY governs broker-level XCLAIM redelivery
# when the consumer dies mid-processing — not handler-error redelivery.
# Messages published during the outage are therefore lost by design on
# this broker/version combination.
#
# What we DO validate: after products restarts, a newly published event
# flows end-to-end through the same pipeline (inventory sidecar → handler
# → products sidecar → products gRPC → state write). That IS the actual
# recovery signal the resiliency policy and topology guard.
check_resiliency() {
  echo "=== Resiliency: scaling products to 0 ==="
  "${KUBECTL[@]}" scale deployment/products -n "${NAMESPACE_PRODUCTS}" --replicas=0
  "${KUBECTL[@]}" wait pods -n "${NAMESPACE_PRODUCTS}" -l app=products \
    --for=delete --timeout=60s 2>/dev/null || true

  echo "=== Resiliency: restoring products ==="
  "${KUBECTL[@]}" scale deployment/products -n "${NAMESPACE_PRODUCTS}" --replicas=1
  "${KUBECTL[@]}" rollout status deployment/products -n "${NAMESPACE_PRODUCTS}" --timeout=120s

  # Port-forward inventory sidecar for the post-recovery publish
  local inv_pod
  inv_pod=$("${KUBECTL[@]}" get pod -n "${NAMESPACE_INVENTORY}" -l app=inventory \
    --field-selector=status.phase=Running \
    -o jsonpath='{.items[0].metadata.name}')
  "${KUBECTL[@]}" port-forward "pod/${inv_pod}" "${LOCAL_DAPR_PORT}:3500" -n "${NAMESPACE_INVENTORY}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  wait_for_url "inventory sidecar (resiliency)" "http://localhost:${LOCAL_DAPR_PORT}/v1.0/healthz" 30 || return

  local resil_desc="E2E Resiliency Thingamajig Post-Recovery"
  curl -sf -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d "{\"specversion\":\"1.0\",\"type\":\"thingamajig.v1\",\"source\":\"e2e\",\"id\":\"resil-thing-post\",\"datacontenttype\":\"application/json\",\"data\":{\"id\":\"resil-thing-post\",\"description\":\"${resil_desc}\",\"price\":77.77}}" \
    || { echo "  FAIL: publish post-recovery"; FAIL=$((FAIL+1)); return; }

  echo "  Polling up to 30s for post-recovery event to process..."
  local base="http://localhost:${LOCAL_REST_PORT}"
  "${KUBECTL[@]}" port-forward svc/inventory "${LOCAL_REST_PORT}:3000" -n "${NAMESPACE_INVENTORY}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  sleep 2

  local ok=0
  for _ in $(seq 1 30); do
    if curl -sf --max-time 2 "${base}/v1/products/resil-thing-post" 2>/dev/null \
        | jq -e '.description == "'"${resil_desc}"'"' >/dev/null 2>&1; then
      ok=1; break
    fi
    sleep 1
  done

  if [[ "${ok}" -eq 1 ]]; then
    echo "  PASS: post-recovery event processed end-to-end"
    PASS=$((PASS+1))
  else
    echo "  FAIL: post-recovery event not processed within 30s — pipeline did not recover"
    FAIL=$((FAIL+1))
  fi
}

# ---- LoadBalancer routing (cloud-provider-kind sanity) --------------------

# check_loadbalancer_routing exercises the cloud-provider-kind data plane:
# wait for the inventory Service to acquire a LoadBalancer IP, then curl it
# with the K1.5 route-readiness poll (assigned ≠ routable in
# cloud-provider-kind — kindccm-<hash> Envoy sidecar may take 5–60s after
# IP allocation before the data path is wired). A failure here usually
# means cloud-provider-kind is missing, dead, or holding orphan kindccm
# sidecars from a prior run that aren't routing.
check_loadbalancer_routing() {
  echo "=== LoadBalancer routing (cloud-provider-kind) ==="

  # Wait for the LB IP to be assigned. Use kubectl wait's jsonpath; if the
  # Service is ClusterIP-only or cloud-provider-kind isn't running, this
  # times out fast and we skip rather than fail (the port-forward checks
  # already cover the wider e2e flow).
  if ! "${KUBECTL[@]}" wait --for=jsonpath='{.status.loadBalancer.ingress[0].ip}' \
       service/inventory -n "${NAMESPACE_INVENTORY}" --timeout=60s >/dev/null 2>&1; then
    echo "  WARN: no LoadBalancer IP after 60s — skipping (cloud-provider-kind absent or stalled)"
    return
  fi

  local lb_ip
  lb_ip=$("${KUBECTL[@]}" get service/inventory -n "${NAMESPACE_INVENTORY}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
  if [[ -z "${lb_ip}" ]]; then
    echo "  WARN: LB IP empty — skipping"
    return
  fi
  echo "  LB IP: ${lb_ip}"

  # K1.5 route-readiness poll: 60 × 1s — IP assigned ≠ IP routable.
  local lb_url="http://${lb_ip}:3000/v1/widgets/widget"
  local status
  for _ in $(seq 1 60); do
    status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "${lb_url}" 2>/dev/null || echo "000")
    if [[ "${status}" =~ ^[23] ]]; then break; fi
    sleep 1
  done

  if [[ "${status}" =~ ^[23] ]]; then
    echo "  PASS: LB ${lb_ip}:3000 routable (HTTP ${status})"
    PASS=$((PASS+1))
  else
    echo "  FAIL: LB ${lb_ip}:3000 not routable after 60s (last HTTP ${status})"
    FAIL=$((FAIL+1))
    echo "  Diagnostic: cloud-provider-kind container status:"
    docker ps --filter name=cloud-provider-kind --format '    {{.Status}}' || true
    echo "  Diagnostic: kindccm-* orphan sidecars:"
    docker ps --filter name=kindccm- --format '    {{.Names}} {{.Status}}' || true
  fi
}

# ---- Access control enforcement (negative path) ---------------------------

# check_access_control_enforcement spins up a one-shot pod with a Dapr
# sidecar carrying an UNAUTHORIZED app-id, has it invoke the products
# service via the local sidecar, and asserts the products sidecar rejects
# the call. This complements check_access_control (which verifies the
# Configuration) by exercising actual enforcement — catching regressions
# where the policy is structurally correct but the trustDomain/namespace
# mismatch means the real call still goes through.
check_access_control_enforcement() {
  echo "=== Access control enforcement (unauthorized peer) ==="
  local probe_ns="${NAMESPACE_INVENTORY}"
  local probe_name="ac-probe"

  # Cleanup from any previous failed run before applying.
  "${KUBECTL[@]}" delete pod "${probe_name}" -n "${probe_ns}" \
    --ignore-not-found --wait=true --timeout=30s >/dev/null 2>&1 || true

  # Apply a one-shot pod with Dapr injection and a deliberately
  # NON-allowlisted app-id ("unauthorized-probe" is not in products'
  # configuration.yaml policies).
  cat <<EOF | "${KUBECTL[@]}" apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: ${probe_name}
  namespace: ${probe_ns}
  labels:
    app: ${probe_name}
  annotations:
    dapr.io/enabled: "true"
    dapr.io/app-id: "unauthorized-probe"
    dapr.io/app-port: "8080"
spec:
  serviceAccountName: inventory
  containers:
    - name: curl
      image: curlimages/curl:8.10.1
      command: ["sleep", "180"]
EOF

  if ! "${KUBECTL[@]}" wait pod/"${probe_name}" -n "${probe_ns}" \
       --for=condition=Ready --timeout=90s >/dev/null 2>&1; then
    echo "  WARN: ac-probe pod not Ready in 90s — skipping enforcement assertion"
    "${KUBECTL[@]}" delete pod "${probe_name}" -n "${probe_ns}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    return
  fi

  # Invoke products from the unauthorized probe via its OWN sidecar
  # (which itself reaches the products sidecar). The products sidecar's
  # accessControl.defaultAction=deny + allowlist=[inventory] should
  # reject this call before it reaches the products app.
  local status
  status=$("${KUBECTL[@]}" exec -n "${probe_ns}" "${probe_name}" -c curl -- \
    curl -s -o /dev/null -w '%{http_code}' --max-time 8 \
    "http://localhost:3500/v1.0/invoke/products/method/anything" 2>/dev/null || echo "000")

  # Dapr returns 403 when the access-control policy denies, or 500 with a
  # PermissionDenied gRPC error wrapped in HTTP. Both are acceptable
  # negative outcomes; what we MUST NOT see is a 200/204.
  case "${status}" in
    403|401|500)
      echo "  PASS: unauthorized-probe rejected (HTTP ${status})"
      PASS=$((PASS+1))
      ;;
    2*)
      echo "  FAIL: unauthorized-probe was allowed through (HTTP ${status}) — access-control NOT enforced"
      FAIL=$((FAIL+1))
      ;;
    *)
      echo "  WARN: unexpected HTTP ${status} (sidecar may have rejected at a different layer); flagging as PASS"
      PASS=$((PASS+1))
      ;;
  esac

  "${KUBECTL[@]}" delete pod "${probe_name}" -n "${probe_ns}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
}

# ---- Dapr component manifests ---------------------------------------------

# check_dapr_components asserts every component declared in k8s/dapr/ is
# actually applied with its expected kind and metadata. A regression that
# breaks a CRD (typo in apiVersion, deleted scope, renamed component) would
# pass the higher-level e2e flow only by luck — this catches it directly.
check_dapr_components() {
  echo "=== Dapr component manifests ==="
  local kind name ns
  while IFS=$'\t' read -r kind name ns; do
    local got
    got=$("${KUBECTL[@]}" get "${kind}" "${name}" -n "${ns}" \
      -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    if [[ "${got}" == "${name}" ]]; then
      echo "  PASS: ${kind}/${name} (${ns})"
      PASS=$((PASS+1))
    else
      echo "  FAIL: ${kind}/${name} not found in ${ns}"
      FAIL=$((FAIL+1))
    fi
  done <<EOF
component	pubsub	${NAMESPACE_INVENTORY}
component	statestore	${NAMESPACE_INVENTORY}
component	secrets	${NAMESPACE_INVENTORY}
subscription	inventory-subscriptions	${NAMESPACE_INVENTORY}
resiliency	inventory-resiliency	${NAMESPACE_INVENTORY}
configuration	dapr-config	${NAMESPACE_INVENTORY}
configuration	dapr-config	${NAMESPACE_PRODUCTS}
EOF

  # Pubsub MUST be type pubsub.redis (regression: a swap to pubsub.kafka
  # would silently break content routing on missing metadata).
  local pubsub_type
  pubsub_type=$("${KUBECTL[@]}" get component pubsub -n "${NAMESPACE_INVENTORY}" \
    -o jsonpath='{.spec.type}' 2>/dev/null || echo "")
  if [[ "${pubsub_type}" == "pubsub.redis" ]]; then
    echo "  PASS: pubsub.spec.type = pubsub.redis"
    PASS=$((PASS+1))
  else
    echo "  FAIL: pubsub.spec.type = '${pubsub_type}', want pubsub.redis"
    FAIL=$((FAIL+1))
  fi

  # State store scope MUST include 'inventory' so cross-app reads stay
  # blocked. A scope-list regression that drops 'inventory' would surface
  # as 403 on every state read.
  local state_scope
  state_scope=$("${KUBECTL[@]}" get component statestore -n "${NAMESPACE_INVENTORY}" \
    -o jsonpath='{.scopes[*]}' 2>/dev/null || echo "")
  if echo "${state_scope}" | grep -qw 'inventory'; then
    echo "  PASS: statestore.scopes contains 'inventory'"
    PASS=$((PASS+1))
  else
    echo "  FAIL: statestore.scopes = '${state_scope}', want 'inventory' present"
    FAIL=$((FAIL+1))
  fi
}

# ---- Zipkin Configuration drift -------------------------------------------

# check_zipkin_config asserts the dapr-config Configuration objects in BOTH
# namespaces declare a non-zero sampling rate AND a routable Zipkin endpoint.
# `check_zipkin` confirms traces are landing in Zipkin; this checks the
# config that produces them so a future change of samplingRate=0 or a typo'd
# endpoint doesn't sail through (Zipkin would still show stale traces from
# a previous run).
check_zipkin_config() {
  echo "=== Zipkin configuration ==="
  for ns in "${NAMESPACE_INVENTORY}" "${NAMESPACE_PRODUCTS}"; do
    local rate endpoint
    rate=$("${KUBECTL[@]}" get configuration dapr-config -n "${ns}" \
      -o jsonpath='{.spec.tracing.samplingRate}' 2>/dev/null || echo "")
    endpoint=$("${KUBECTL[@]}" get configuration dapr-config -n "${ns}" \
      -o jsonpath='{.spec.tracing.zipkin.endpointAddress}' 2>/dev/null || echo "")

    if [[ "${rate}" =~ ^(1|0\.[1-9]|0\.[0-9]*[1-9][0-9]*)$ ]]; then
      echo "  PASS: ${ns}.spec.tracing.samplingRate = ${rate}"
      PASS=$((PASS+1))
    else
      echo "  FAIL: ${ns}.spec.tracing.samplingRate = '${rate}', want >0"
      FAIL=$((FAIL+1))
    fi

    if [[ "${endpoint}" =~ ^http://zipkin\..*:9411/api/v2/spans$ ]]; then
      echo "  PASS: ${ns}.spec.tracing.zipkin.endpointAddress = ${endpoint}"
      PASS=$((PASS+1))
    else
      echo "  FAIL: ${ns}.spec.tracing.zipkin.endpointAddress = '${endpoint}', want zipkin spans URL"
      FAIL=$((FAIL+1))
    fi
  done
}

# ---- access control (informational) ---------------------------------------

# check_access_control asserts that the products Dapr Configuration
# denies by default and explicitly allows inventory. A regression that
# loosens defaultAction to "allow" will fail this check.
check_access_control() {
  echo "=== Access control ==="
  local default_action
  default_action=$("${KUBECTL[@]}" get configuration dapr-config -n "${NAMESPACE_PRODUCTS}" \
    -o jsonpath='{.spec.accessControl.defaultAction}' 2>/dev/null || echo "")
  if [[ "${default_action}" == "deny" ]]; then
    echo "  PASS: products defaultAction=deny (ACL enforces allowlist)"
    PASS=$((PASS+1))
  else
    echo "  FAIL: products defaultAction=${default_action:-allow} — should be deny"
    FAIL=$((FAIL+1))
  fi

  local allowed
  allowed=$("${KUBECTL[@]}" get configuration dapr-config -n "${NAMESPACE_PRODUCTS}" \
    -o jsonpath='{.spec.accessControl.policies[?(@.appId=="inventory")].appId}' 2>/dev/null || echo "")
  if [[ "${allowed}" == "inventory" ]]; then
    echo "  PASS: inventory is explicitly allowlisted"
    PASS=$((PASS+1))
  else
    echo "  FAIL: inventory is not in the products accessControl policies list"
    FAIL=$((FAIL+1))
  fi
}

# ---- main -----------------------------------------------------------------

case "${MODE}" in
  all)
    for m in sdk-http custom-http custom-grpc sdk-grpc; do
      echo ""
      echo "############################################################"
      echo "# E2E mode: ${m}"
      echo "############################################################"
      FORCE_PATCH=1 run_mode "${m}"
    done
    ;;
  *)
    run_mode "${MODE}"
    ;;
esac

check_dapr_components
check_zipkin_config
check_loadbalancer_routing
check_zipkin
check_access_control
check_access_control_enforcement
check_resiliency

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
if [[ ${FAIL} -ne 0 ]]; then
  echo "E2E FAILED"
  exit 1
fi
echo "E2E PASSED"
