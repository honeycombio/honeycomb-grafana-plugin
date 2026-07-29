#!/usr/bin/env bash
#
# Smoke test: install the packaged plugin zip into a real Grafana instance and
# verify that (1) Grafana loads the plugin and (2) the backend binary spawns
# and answers a health check.
#
# This is exactly what a self-hosted user does with a release artifact, so if
# this passes, the release is deployable.
#
# Prerequisites: docker, and a zip in build/ produced by scripts/package.sh.
#
# Usage:
#   ./scripts/smoke-test.sh [path-to-zip]
#
# Environment:
#   GRAFANA_IMAGE   Grafana docker image (default: grafana/grafana:latest)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GRAFANA_IMAGE="${GRAFANA_IMAGE:-grafana/grafana:latest}"
CONTAINER_NAME="honeycomb-plugin-smoke-test"
GRAFANA_PORT="${GRAFANA_PORT:-3999}"
GRAFANA_URL="http://localhost:${GRAFANA_PORT}"
ADMIN_AUTH="admin:admin"

ZIP_PATH="${1:-$(ls "${REPO_ROOT}"/build/*.zip 2>/dev/null | head -1 || true)}"
if [[ -z "${ZIP_PATH}" || ! -f "${ZIP_PATH}" ]]; then
  echo "ERROR: no plugin zip found. Run ./scripts/package.sh first." >&2
  exit 1
fi

PLUGINS_DIR="$(mktemp -d)"
unzip -q "${ZIP_PATH}" -d "${PLUGINS_DIR}"
# mktemp creates a 700 dir; the bind mount preserves host permissions on
# Linux, so make everything readable by the container's grafana user (472).
chmod -R a+rX "${PLUGINS_DIR}"
PLUGIN_ID="$(ls "${PLUGINS_DIR}")"
echo "Testing ${ZIP_PATH} (plugin id: ${PLUGIN_ID}) against ${GRAFANA_IMAGE}"

cleanup() {
  docker rm -f "${CONTAINER_NAME}" > /dev/null 2>&1 || true
  rm -rf "${PLUGINS_DIR}"
  # Set later in the script, so tolerate it being unset under `set -u`.
  rm -f "${HEALTH_BODY:-}"
}
trap cleanup EXIT

docker rm -f "${CONTAINER_NAME}" > /dev/null 2>&1 || true
docker run -d --name "${CONTAINER_NAME}" \
  -p "${GRAFANA_PORT}:3000" \
  -e GF_SECURITY_ADMIN_USER=admin \
  -e GF_SECURITY_ADMIN_PASSWORD=admin \
  -e "GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=${PLUGIN_ID}" \
  -v "${PLUGINS_DIR}:/var/lib/grafana/plugins" \
  "${GRAFANA_IMAGE}" > /dev/null

# ---------------------------------------------------------------------------
# Wait for Grafana to come up.
# ---------------------------------------------------------------------------
echo -n "Waiting for Grafana"
for i in $(seq 1 60); do
  if curl -sf "${GRAFANA_URL}/api/health" > /dev/null 2>&1; then
    echo " ready."
    break
  fi
  if [[ "$i" == 60 ]]; then
    echo " TIMEOUT"
    docker logs "${CONTAINER_NAME}" | tail -50
    exit 1
  fi
  echo -n "."
  sleep 2
done

# ---------------------------------------------------------------------------
# Check 1: Grafana registered the plugin. The plugin store can finish loading
# slightly after /api/health goes green, so poll briefly.
# ---------------------------------------------------------------------------
PLUGIN_INFO=""
for i in $(seq 1 15); do
  PLUGIN_INFO="$(curl -s -u "${ADMIN_AUTH}" "${GRAFANA_URL}/api/plugins?embedded=0" \
    | jq -r --arg id "${PLUGIN_ID}" '.[] | select(.id == $id)')"
  if [[ -n "${PLUGIN_INFO}" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "${PLUGIN_INFO}" ]]; then
  echo "FAIL: Grafana did not load plugin '${PLUGIN_ID}'." >&2
  docker logs "${CONTAINER_NAME}" 2>&1 | grep -i -E "plugin|error" | tail -30
  exit 1
fi
PLUGIN_VERSION="$(echo "${PLUGIN_INFO}" | jq -r .info.version)"
echo "PASS: plugin loaded (version ${PLUGIN_VERSION})"

# ---------------------------------------------------------------------------
# Check 2: the backend binary spawns and answers a health check.
# We create a datasource with a dummy API key; the backend should start, call
# Honeycomb, and report an auth error. Any well-formed answer (HTTP < 500)
# proves the binary runs. HTTP 5xx means Grafana could not start the backend.
# ---------------------------------------------------------------------------
DS_RESPONSE="$(curl -sf -u "${ADMIN_AUTH}" -X POST "${GRAFANA_URL}/api/datasources" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"smoke-test-honeycomb\",
    \"type\": \"${PLUGIN_ID}\",
    \"access\": \"proxy\",
    \"jsonData\": {\"apiUrl\": \"https://api.honeycomb.io\"},
    \"secureJsonData\": {\"apiKey\": \"smoke-test-not-a-real-key\"}
  }")"
DS_UID="$(echo "${DS_RESPONSE}" | jq -r .datasource.uid)"

HEALTH_BODY="$(mktemp)"
HEALTH_CODE="$(curl -s -o "${HEALTH_BODY}" -w '%{http_code}' -u "${ADMIN_AUTH}" \
  "${GRAFANA_URL}/api/datasources/uid/${DS_UID}/health")"
echo "Backend health check: HTTP ${HEALTH_CODE} — $(cat "${HEALTH_BODY}")"

if [[ "${HEALTH_CODE}" -ge 500 ]]; then
  echo "FAIL: backend binary did not start (HTTP ${HEALTH_CODE})." >&2
  docker logs "${CONTAINER_NAME}" 2>&1 | tail -50
  exit 1
fi
echo "PASS: backend binary spawned and responded"

echo ""
echo "Smoke test passed: ${ZIP_PATH} is deployable."
