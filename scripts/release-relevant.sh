#!/usr/bin/env bash
#
# Decide whether a range of commits changed anything that ships to users.
#
# Every merge to main auto-cuts a patch release (see .github/workflows/version-bump.yml),
# which means a CI tweak or a typo fix in a doc used to mint a version nobody needs.
# This script is the gate: the bump only runs when the diff touches code that ends up
# inside the plugin zip.
#
# Usage:
#   ./scripts/release-relevant.sh <from-ref> <to-ref>
#
# Exit codes:
#   0  release-relevant — cut a release
#   1  not release-relevant — skip the bump
#   2  usage error
#
# Deliberately runnable outside CI, so the classification can be checked against any
# commit range without pushing anything:
#
#   ./scripts/release-relevant.sh HEAD^1 HEAD
#   ./scripts/release-relevant.sh v0.1.0 main
#
# Judgement calls worth knowing about:
#
#   * README.md and CHANGELOG.md DO ship inside the zip (webpack copies them via
#     .config/bundler/copyFiles.ts, and Grafana renders the README on the plugin
#     page). They are still treated as skip-worthy: a docs-only release is noise,
#     and the change rides along with the next code release.
#
#   * A package.json edit counts as release-relevant even when only a "scripts"
#     entry moved, because the same file carries "version" and the dependency set.
#     Telling those apart is not worth the complexity; the cost is one extra patch
#     release.
#
#   * Test files are excluded even under src/ and pkg/, since they are never bundled
#     into the artifact.
#
#   * workflow_dispatch on the bump workflow bypasses this script entirely. That is
#     the escape hatch when you want a release regardless of what changed.

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $(basename "$0") <from-ref> <to-ref>" >&2
  exit 2
fi

FROM="$1"
TO="$2"

# --no-renames keeps a rename as separate delete+add paths, so moving a file INTO a
# release-relevant directory is still detected as relevant.
CHANGED="$(git diff --name-only --no-renames "${FROM}" "${TO}")"

if [[ -z "${CHANGED}" ]]; then
  echo "release-relevant: no files changed between ${FROM} and ${TO} — skipping release."
  exit 1
fi

# Paths whose contents end up in the built plugin. Anything not matched here — .github/,
# scripts/, tests/, docs/, *.md, playwright.config.js, docker-compose.yml, .gitignore —
# cannot change the artifact and so does not justify a release.
RELEVANT="$(printf '%s\n' "${CHANGED}" | grep -E \
  -e '^src/' \
  -e '^pkg/' \
  -e '^go\.(mod|sum)$' \
  -e '^Magefile\.go$' \
  -e '^package(-lock)?\.json$' \
  -e '^\.config/' \
  | grep -v -E \
  -e '\.test\.tsx?$' \
  -e '_test\.go$' \
  || true)"

if [[ -z "${RELEVANT}" ]]; then
  echo "release-relevant: no release-relevant changes between ${FROM} and ${TO} — skipping release."
  echo "Changed files:"
  sed 's/^/  /' <<< "${CHANGED}"
  exit 1
fi

echo "release-relevant: found release-relevant changes between ${FROM} and ${TO}."
sed 's/^/  /' <<< "${RELEVANT}"
exit 0
