#!/usr/bin/env bash
#
# Package the built plugin into a Grafana-installable zip archive.
#
# Prerequisites: `npm run build` and `mage build:XXX` have populated dist/.
#
# Usage:
#   ./scripts/package.sh            # version taken from package.json
#   ./scripts/package.sh 1.2.3     # explicit version (must match dist/plugin.json)
#
# Output:
#   build/<plugin-id>-<version>.zip
#   build/<plugin-id>-<version>.zip.sha1
#
# The zip contains a single top-level directory named after the plugin id,
# which is the layout Grafana expects when unzipping into its plugins dir.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${REPO_ROOT}/dist"
BUILD_DIR="${REPO_ROOT}/build"

# ---------------------------------------------------------------------------
# Sanity checks: make sure dist/ contains a complete plugin build.
# ---------------------------------------------------------------------------
if [[ ! -f "${DIST_DIR}/plugin.json" ]]; then
  echo "ERROR: dist/plugin.json not found. Run 'npm run build' first." >&2
  exit 1
fi

if [[ ! -f "${DIST_DIR}/module.js" ]]; then
  echo "ERROR: dist/module.js not found. Run 'npm run build' first." >&2
  exit 1
fi

PLUGIN_ID="$(jq -r .id "${DIST_DIR}/plugin.json")"
DIST_VERSION="$(jq -r .info.version "${DIST_DIR}/plugin.json")"
EXECUTABLE="$(jq -r '.executable // empty' "${DIST_DIR}/plugin.json")"

if [[ "${DIST_VERSION}" == *"%"* ]]; then
  echo "ERROR: dist/plugin.json still contains an unreplaced version placeholder (${DIST_VERSION})." >&2
  echo "       Run a production build: npm run build" >&2
  exit 1
fi

VERSION="${1:-${DIST_VERSION}}"
if [[ "${VERSION}" != "${DIST_VERSION}" ]]; then
  echo "ERROR: requested version ${VERSION} does not match dist/plugin.json version ${DIST_VERSION}." >&2
  echo "       Update the 'version' field in package.json and rebuild." >&2
  exit 1
fi

# A backend plugin must ship at least one compiled binary.
if [[ -n "${EXECUTABLE}" ]]; then
  if ! compgen -G "${DIST_DIR}/${EXECUTABLE}_*" > /dev/null; then
    echo "ERROR: no backend binaries matching '${EXECUTABLE}_*' in dist/. Run 'mage build:all' first." >&2
    exit 1
  fi
fi

# ---------------------------------------------------------------------------
# Assemble the archive: dist/ contents under a <plugin-id>/ root directory.
# ---------------------------------------------------------------------------
ARCHIVE_NAME="${PLUGIN_ID}-${VERSION}.zip"
STAGING_DIR="${BUILD_DIR}/${PLUGIN_ID}"

rm -rf "${BUILD_DIR}"
mkdir -p "${STAGING_DIR}"
cp -r "${DIST_DIR}/." "${STAGING_DIR}/"

# ---------------------------------------------------------------------------
# Make the archive depend only on the commit, not on when it was built.
#
# Two things used to vary between builds of identical source: `info.updated`,
# which webpack stamps from the wall clock via the %TODAY% placeholder, and the
# mtimes zip copies into its headers. So packaging the same tag twice produced
# two different checksums.
#
# That matters because the release notes publish a SHA1 and tell users to verify
# against it, while RELEASING.md recommends re-running the Release workflow
# against an existing tag to recover from a failure. Doing so would quietly
# change the checksum out from under anyone who had already verified.
# ---------------------------------------------------------------------------
SOURCE_DATE="$(git -C "${REPO_ROOT}" log -1 --format=%cs 2>/dev/null || true)"
if [[ -z "${SOURCE_DATE}" ]]; then
  # Not a git checkout (an exported tarball, say). Keep whatever webpack stamped
  # rather than failing the build, but say so — the archive won't be reproducible.
  echo "WARNING: not a git checkout, so the archive will not be reproducible." >&2
else
  jq --arg d "${SOURCE_DATE}" '.info.updated = $d' "${STAGING_DIR}/plugin.json" \
    > "${STAGING_DIR}/plugin.json.tmp"
  mv "${STAGING_DIR}/plugin.json.tmp" "${STAGING_DIR}/plugin.json"
  find "${STAGING_DIR}" -exec touch -h -t "${SOURCE_DATE//-/}0000" {} +
fi

# Feed zip a sorted file list rather than letting it walk the tree: `zip -r`
# recurses in readdir order, which is not guaranteed stable across machines or
# filesystems. -X drops the extra attribute fields (uid/gid, native timestamps)
# that would otherwise re-introduce per-build variation.
(
  cd "${BUILD_DIR}"
  find "${PLUGIN_ID}" -print | LC_ALL=C sort | zip -qX "${ARCHIVE_NAME}" -@
)

# Checksum for release verification.
(cd "${BUILD_DIR}" && shasum -a 1 "${ARCHIVE_NAME}" | awk '{print $1}' > "${ARCHIVE_NAME}.sha1")

rm -rf "${STAGING_DIR}"

echo "Packaged: build/${ARCHIVE_NAME}"
echo "Version:  ${VERSION}"
echo "SHA1:     $(cat "${BUILD_DIR}/${ARCHIVE_NAME}.sha1")"
echo ""
echo "Archive contents:"
unzip -l "${BUILD_DIR}/${ARCHIVE_NAME}" | head -30
