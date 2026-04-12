#!/usr/bin/env bash
# End-to-end tests for dapr-go-hero on Kubernetes.
# Publishes events via Dapr sidecar and verifies REST API responses.
set -euo pipefail

NAMESPACE_INVENTORY="dapr-go-hero-inventory"
NAMESPACE_INFRA="dapr-go-hero"
PASS=0
FAIL=0
PF_PID=""

cleanup() {
  if [[ -n "${PF_PID}" ]]; then
    kill "${PF_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

CURL_TIMEOUT=5

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

echo "=== Waiting for pods to be ready ==="

kubectl wait pods -n "${NAMESPACE_INVENTORY}" -l app=inventory \
  --for condition=Ready --timeout=180s

kubectl wait pods -n dapr-go-hero-products -l app=products \
  --for condition=Ready --timeout=180s

echo "=== Setting up port-forward to inventory Dapr sidecar ==="

INVENTORY_POD=$(kubectl get pod -n "${NAMESPACE_INVENTORY}" -l app=inventory \
  -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward "pod/${INVENTORY_POD}" 3500:3500 -n "${NAMESPACE_INVENTORY}" &
PF_PID=$!
sleep 3

echo "=== Publishing events ==="

echo "Publishing widget event..."
curl -sf -X POST http://localhost:3500/v1.0/publish/pubsub/inventory \
  -H "Content-Type: application/cloudevents+json" \
  -d '{
    "specversion": "1.0",
    "type": "widget.v1",
    "source": "e2e-test",
    "id": "e2e-widget-1",
    "datacontenttype": "application/json",
    "data": {"id": "widget", "description": "E2E Widget", "price": 9.99}
  }' && echo " OK" || echo " FAILED"

echo "Publishing gadget event..."
curl -sf -X POST http://localhost:3500/v1.0/publish/pubsub/inventory \
  -H "Content-Type: application/cloudevents+json" \
  -d '{
    "specversion": "1.0",
    "type": "gadget.v1",
    "source": "e2e-test",
    "id": "e2e-gadget-1",
    "datacontenttype": "application/json",
    "data": {"id": "gadget", "description": "E2E Gadget", "price": 19.99}
  }' && echo " OK" || echo " FAILED"

echo "Publishing thingamajig event..."
curl -sf -X POST http://localhost:3500/v1.0/publish/pubsub/inventory \
  -H "Content-Type: application/cloudevents+json" \
  -d '{
    "specversion": "1.0",
    "type": "thingamajig.v1",
    "source": "e2e-test",
    "id": "e2e-thingamajig-1",
    "datacontenttype": "application/json",
    "data": {"id": "thingamajig", "description": "E2E Thingamajig", "price": 29.99}
  }' && echo " OK" || echo " FAILED"

echo "=== Waiting for async processing (10s) ==="
sleep 10

echo "=== Setting up REST API access (port-forward) ==="

# Port-forward is more portable than LoadBalancer IP across environments
# (KinD + MetalLB works locally but Docker network detection is flaky in act/CI).
kubectl port-forward svc/inventory 3000:3000 -n "${NAMESPACE_INVENTORY}" &
sleep 3
BASE_URL="http://localhost:3000"

echo "=== Testing REST API ==="

assert_status "GET /v1/widgets/widget" "${BASE_URL}/v1/widgets/widget" "200"
assert_json_field "Widget description" "${BASE_URL}/v1/widgets/widget" "description" "E2E Widget"

assert_status "GET /v1/gadgets/gadget" "${BASE_URL}/v1/gadgets/gadget" "200"
assert_json_field "Gadget description" "${BASE_URL}/v1/gadgets/gadget" "description" "E2E Gadget"

assert_status "GET /v1/products/thingamajig" "${BASE_URL}/v1/products/thingamajig" "200"
assert_json_field "Product description" "${BASE_URL}/v1/products/thingamajig" "description" "E2E Thingamajig"

echo "=== Checking Zipkin traces ==="

ZIPKIN_IP=$(kubectl get svc zipkin -n "${NAMESPACE_INFRA}" \
  -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)
if [[ -n "${ZIPKIN_IP}" ]]; then
  kubectl port-forward svc/zipkin 9411:9411 -n "${NAMESPACE_INFRA}" &
  sleep 2
  TRACE_COUNT=$(curl -sf "http://localhost:9411/api/v2/traces?limit=5" 2>/dev/null | jq 'length' 2>/dev/null || echo "0")
  if [[ "${TRACE_COUNT}" -gt 0 ]]; then
    echo "  PASS: Zipkin has ${TRACE_COUNT} trace(s)"
    PASS=$((PASS+1))
  else
    echo "  WARN: No traces found in Zipkin (may need more time)"
  fi
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

if [[ ${FAIL} -ne 0 ]]; then
  echo "E2E FAILED"
  exit 1
fi

echo "E2E PASSED"
