# Changelog

All notable changes to the Honeycomb Grafana Data Source Plugin are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.4] — 2026-08-07

### Added
- Go test coverage floor raised from 60% to 65%, measured against 68.9% actual, so it acts as a regression alarm rather than a number pinned to current coverage.
- Backend tests for the resource handler (`CallResource`, dataset and column listing, the `__all__` short-circuit, per-dataset cache scoping, upstream error mapping), the logs query helpers, the retry/backoff schedule including the `Retry-After` and jitter paths, `FlexibleTime` and `ResultEntry` JSON decoding, and the SLO endpoints. `pkg/honeycomb` coverage rose from 53.3% to 83.9%, `pkg/plugin` from 45.4% to 58.6%.
- `npm run typecheck` now also typechecks `tests/` against `tests/tsconfig.json`. The e2e specs were previously never typechecked — the root config only includes `src/`, and `playwright test` transpiles via esbuild without checking types — so a broken fixture signature surfaced as a Grafana-boot-time failure instead of a type error.
- Frontend tests for the `slo`, `logs`, and `traces` branches of `filterQuery` (previously only the raw/builder tail was covered), for `havings` template substitution, and for `scopedVars` being forwarded to the template service.
- Reproducible packaging: the same commit now always produces a byte-identical zip and SHA1. `info.updated`, archive file mtimes, and the build timestamp the Grafana SDK embeds in each binary are all derived from the commit date instead of the wall clock, and `zip` is fed a sorted file list. `SOURCE_DATE_EPOCH` is honoured for builds outside a git checkout.

### Fixed
- The smoke test's backend check accepted any HTTP status below 500, so a 404 from an empty datasource UID, a 401, or a non-JSON error page all reported "backend binary spawned and responded". It now requires HTTP 400 or 200 *and* a plugin-supplied message, guards the datasource UID, cross-checks the loaded plugin version against the packaged one, and derives the plugin id from `plugin.json` rather than from `ls`.
- Re-running the release workflow against an existing tag produced a different SHA1 than the one already published in the release notes, because the zip was not reproducible. Users who verified the original checksum would have seen a mismatch.
- The Go module path was still `github.com/honeycombio/grafana-honeycomb-datasource`, a repo that does not exist. Renamed to `github.com/honeycombio/honeycomb-grafana-plugin` along with all 39 internal import paths. The `User-Agent` header sent to the Honeycomb API changed from `grafana-honeycomb-datasource/<version>` to `honeycomb-grafana-plugin/<version>` at the same time — anything filtering on the old value needs updating.

## [0.1.3] — 2026-08-02

### Added
- Release pipeline that packages a Grafana-installable zip (all platform binaries, SHA1 checksum) and publishes it to GitHub Releases (`.github/workflows/release.yml`, `scripts/package.sh`). Triggered by pushing a `v*` tag — from the version-bump workflow or by hand — and also runnable manually against an existing tag.
- Container smoke test that installs the packaged zip into a real Grafana and verifies the plugin loads and the backend binary answers a health check (`scripts/smoke-test.sh`), run on every PR and before every release.
- Playwright e2e test suite using `@grafana/plugin-e2e`, run in CI against Grafana 11.0.0 and latest (`tests/e2e/`).
- Frontend unit tests for query filtering, template variable substitution, and variable queries (`src/datasource.test.ts`).
- Go test coverage floor (60%) enforced in CI.
- `mage build:linuxARM64` target for local smoke testing on Apple Silicon.
- `RELEASING.md` documenting the release process and quality gates.
- `scripts/promote-changelog.js`, run during the version bump, renames the `[Unreleased]` heading to the released version and updates the link references — so release notes point at a real changelog section instead of a permanent "Unreleased".
- `scripts/release-relevant.sh`, which gates the auto-bump so that merges unable to change the artifact (CI config, `scripts/`, `tests/`, docs) no longer mint a patch release. Runnable locally against any commit range.
- `CI required` aggregate status check — one stable context to require in branch protection, instead of a hand-maintained list of job names that drifts and cannot cover matrix jobs.

### Fixed
- **No release was ever published.** Two independent causes. First, the version-bump workflow created a lightweight tag and pushed with `git push --follow-tags`, which only pushes annotated tags, so the `v0.1.1` and `v0.1.2` tags never reached origin and the release workflow never ran. Tags are now annotated and the branch and tag are pushed together with `--atomic`. Second, the `main` ruleset requires pull requests and cannot exempt `GITHUB_TOKEN` — ruleset bypass actors may only be org-owned GitHub Apps, and the first-party GitHub Actions app is not one — so the push was rejected outright with `GH013`. The bump workflow now authenticates with a `RELEASE_TOKEN` PAT belonging to a bypass-listed user, which `RELEASING.md` documents as a required setup step. Because PAT-created events do trigger workflows (unlike `GITHUB_TOKEN` ones), pushing the tag now starts the release directly and the `workflow_call` hop was removed, so bot and human tags follow one identical path.
- CI built only the linux/amd64 backend binary while releases build all six platforms, so a cross-compile break could pass every PR check and only fail after the release tag had been pushed. CI now runs `mage build:all`.
- `npm test` no longer passes with `--passWithNoTests`, which made the frontend test gate unfalsifiable: a test-discovery regression was indistinguishable from success in both CI and the release workflow.
- `src/plugin.json` referenced a bundled dashboard that does not exist, which broke plugin validation; the version field is now stamped from `package.json` at build time via `%VERSION%`.
- The release workflow previously produced a zip without the compiled backend binaries or the required `<plugin-id>/` root directory, so it could not be installed into Grafana.
- README no longer suggests `grafana-cli plugins install`, which cannot work until the plugin is in the Grafana catalog.
- `docker-compose.yml` now mounts only `dist/` into Grafana (not the whole repo) and supports `GRAFANA_PORT`/`GRAFANA_VERSION` overrides.
- Every GitHub link pointed at `honeycombio/grafana-honeycomb-datasource`, which does not exist — including the README's release-download link and the two `plugin.json` links Grafana renders on the plugin page. All now point at `honeycombio/honeycomb-grafana-plugin`.

## [0.1.0] — 2025-01-15

### Added
- Initial release of the Honeycomb Grafana data source plugin.
- Backend plugin (Go) with full Honeycomb Query API and Query Data API support.
- Three-level in-process cache (query_id → result_id → completed result) to protect against the 10 req/min rate limit on Create Query Result.
- Token-bucket rate limiter at 8 tokens/60 s with exponential backoff on 429.
- Singleflight deduplication to prevent concurrent dashboard panels from fanning out identical Honeycomb API calls.
- Support for timeseries, table, and stat query modes.
- High-cardinality group-by / breakdown support: one Grafana frame per breakdown group in timeseries mode.
- Deep links to Honeycomb query result pages, attached as DataLinks on every metric field.
- Dashboard variable support: list datasets and list columns for a dataset.
- Annotation query support.
- Health check endpoint.
- Visual query editor with dataset selector, calculations editor, filters editor, group-by editor, order-by editor.
- Raw JSON mode for power users who want direct access to the Honeycomb Query API.
- Template variable substitution in all string query fields.
- Configurable API base URL for US and EU Honeycomb accounts.
- Secure API key storage in Grafana `secureJsonData` (encrypted at rest; never sent to browser).
- Architecture Decision Records (ADR-001 through ADR-004).
- Docker Compose local development environment.
- Example provisioning configuration and example dashboard.
- Apache 2.0 license.

[Unreleased]: https://github.com/honeycombio/honeycomb-grafana-plugin/compare/v0.1.4...HEAD
[0.1.4]: https://github.com/honeycombio/honeycomb-grafana-plugin/releases/tag/v0.1.4
[0.1.3]: https://github.com/honeycombio/honeycomb-grafana-plugin/releases/tag/v0.1.3
[0.1.0]: https://github.com/honeycombio/honeycomb-grafana-plugin/releases/tag/v0.1.0
