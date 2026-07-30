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
#   GRAFANA_IMAGE     Grafana docker image (default: grafana/grafana:latest)
#   SMOKE_TEST_PORT   Host port for the throwaway container (default: 3999).
#                     Deliberately not GRAFANA_PORT: docker-compose.yml already
#                     uses that name for the dev stack, and sharing it meant
#                     exporting it for compose made this script try to bind the
#                     port the dev stack was already holding.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GRAFANA_IMAGE="${GRAFANA_IMAGE:-grafana/grafana:latest}"
CONTAINER_NAME="honeycomb-plugin-smoke-test"
SMOKE_TEST_PORT="${SMOKE_TEST_PORT:-3999}"
GRAFANA_URL="http://localhost:${SMOKE_TEST_PORT}"
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

# A valid plugin zip unpacks to exactly one <plugin-id>/ directory. Assert that
# rather than assuming it, so a stray second entry (a __MACOSX/ sibling, say)
# fails here with a clear message instead of producing a multi-line PLUGIN_ID
# that corrupts every command downstream.
ROOT_ENTRIES="$(find "${PLUGINS_DIR}" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"
if [[ "${ROOT_ENTRIES}" != "1" ]]; then
  echo "ERROR: expected exactly one top-level directory in the zip, found ${ROOT_ENTRIES}:" >&2
  find "${PLUGINS_DIR}" -mindepth 1 -maxdepth 1 -exec basename {} \; >&2
  exit 1
fi
PLUGIN_DIR="$(find "${PLUGINS_DIR}" -mindepth 1 -maxdepth 1 -type d)"
if [[ ! -f "${PLUGIN_DIR}/plugin.json" ]]; then
  echo "ERROR: ${PLUGIN_DIR}/plugin.json missing — the zip is not a valid plugin." >&2
  exit 1
fi
# Read the id from plugin.json, the same place Grafana reads it, so a mismatch
# between the directory name and the declared id cannot slip through.
PLUGIN_ID="$(jq -r .id "${PLUGIN_DIR}/plugin.json")"
PACKAGED_VERSION="$(jq -r .info.version "${PLUGIN_DIR}/plugin.json")"
if [[ "$(basename "${PLUGIN_DIR}")" != "${PLUGIN_ID}" ]]; then
  echo "ERROR: zip root directory '$(basename "${PLUGIN_DIR}")' does not match plugin id '${PLUGIN_ID}'." >&2
  echo "       Grafana requires the directory to be named after the plugin id." >&2
  exit 1
fi
echo "Testing ${ZIP_PATH} (plugin id: ${PLUGIN_ID}, version ${PACKAGED_VERSION}) against ${GRAFANA_IMAGE}"

cleanup() {
  docker rm -f "${CONTAINER_NAME}" > /dev/null 2>&1 || true
  rm -rf "${PLUGINS_DIR}"
  # Set later in the script, so tolerate it being unset under `set -u`.
  rm -f "${HEALTH_BODY:-}"
}
trap cleanup EXIT

docker rm -f "${CONTAINER_NAME}" > /dev/null 2>&1 || true
docker run -d --name "${CONTAINER_NAME}" \
  -p "${SMOKE_TEST_PORT}:3000" \
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
LOADED_VERSION="$(echo "${PLUGIN_INFO}" | jq -r .info.version)"
# The version Grafana reports comes from the plugin.json inside the zip, so a
# mismatch means we packaged a stale dist/ — exactly the kind of thing that makes
# a release claim to be one version while shipping another.
if [[ "${LOADED_VERSION}" != "${PACKAGED_VERSION}" ]]; then
  echo "FAIL: Grafana loaded version ${LOADED_VERSION}, but the zip declares ${PACKAGED_VERSION}." >&2
  exit 1
fi
echo "PASS: plugin loaded (version ${LOADED_VERSION})"

# ---------------------------------------------------------------------------
# Check 2: the backend binary spawns and answers a health check.
#
# We create a datasource with a dummy API key. The backend should start, call
# Honeycomb, and report the auth failure — so the pass condition is a *specific*
# well-formed answer, not merely "not a 5xx".
#
# The previous `>= 500` check was far too loose: an empty DS_UID produces
# /api/datasources/uid//health -> 404, and a 401 or a 403 would also have slipped
# through, all of them printing "backend binary spawned and responded" without
# the binary having run at all.
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
DS_UID="$(echo "${DS_RESPONSE}" | jq -r '.datasource.uid // empty')"
if [[ -z "${DS_UID}" ]]; then
  echo "FAIL: could not create a datasource — no uid in the response." >&2
  echo "Response: ${DS_RESPONSE}" >&2
  exit 1
fi

HEALTH_BODY="$(mktemp)"
HEALTH_CODE="$(curl -s -o "${HEALTH_BODY}" -w '%{http_code}' -u "${ADMIN_AUTH}" \
  "${GRAFANA_URL}/api/datasources/uid/${DS_UID}/health")"
echo "Backend health check: HTTP ${HEALTH_CODE} — $(cat "${HEALTH_BODY}")"

# 400 is the expected answer: the backend ran and reported the bad API key.
# 200 is accepted too, in case the endpoint is ever pointed at something valid.
# Anything else — 404 (no such route/uid), 401/403 (auth), 5xx (Grafana could not
# start the backend) — means this check did not prove what it claims to.
if [[ "${HEALTH_CODE}" != "400" && "${HEALTH_CODE}" != "200" ]]; then
  echo "FAIL: expected HTTP 400 or 200 from the health endpoint, got ${HEALTH_CODE}." >&2
  docker logs "${CONTAINER_NAME}" 2>&1 | tail -50
  exit 1
fi

# A response from the plugin carries a message. Its absence means we got a
# generic Grafana error page rather than anything the backend produced.
HEALTH_MESSAGE="$(jq -r '.message // empty' < "${HEALTH_BODY}" 2>/dev/null || true)"
if [[ -z "${HEALTH_MESSAGE}" ]]; then
  echo "FAIL: health response carried no message, so the backend did not answer." >&2
  echo "Body: $(cat "${HEALTH_BODY}")" >&2
  docker logs "${CONTAINER_NAME}" 2>&1 | tail -50
  exit 1
fi
echo "PASS: backend binary spawned and answered — \"${HEALTH_MESSAGE}\""

echo ""
echo "Smoke test passed: ${ZIP_PATH} is deployable."
