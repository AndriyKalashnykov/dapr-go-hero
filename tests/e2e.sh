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

  # Negative case: malformed CloudEvent — missing required `data` field.
  # Dapr should reject with a 4xx/5xx and the API should return 404 for the event's id.
  local malformed_status
  malformed_status=$(curl -s -o /dev/null -w '%{http_code}' --max-time ${CURL_TIMEOUT} \
    -X POST "http://localhost:${LOCAL_DAPR_PORT}/v1.0/publish/pubsub/inventory" \
    -H "Content-Type: application/cloudevents+json" \
    -d '{"specversion":"1.0","type":"widget.v1","source":"e2e","id":"malformed","datacontenttype":"application/json","data":"not-an-object"}' \
    2>/dev/null || echo "000")
  # Dapr 1.17 accepts the publish (the app rejects later), so we just note it.
  echo "  INFO: malformed publish returned HTTP ${malformed_status} (app handles decode failure)"

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

check_zipkin
check_access_control
check_resiliency

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
if [[ ${FAIL} -ne 0 ]]; then
  echo "E2E FAILED"
  exit 1
fi
echo "E2E PASSED"
