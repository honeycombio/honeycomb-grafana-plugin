# Changelog

All notable changes to the Honeycomb Grafana Data Source Plugin are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Release pipeline (triggered by the tags the version-bump workflow pushes) that packages a Grafana-installable zip (all platform binaries, SHA1 checksum) and publishes it to GitHub Releases (`.github/workflows/release.yml`, `scripts/package.sh`).
- Container smoke test that installs the packaged zip into a real Grafana and verifies the plugin loads and the backend binary answers a health check (`scripts/smoke-test.sh`), run on every PR and before every release.
- Playwright e2e test suite using `@grafana/plugin-e2e`, run in CI against Grafana 11.0.0 and latest (`tests/e2e/`).
- Frontend unit tests for query filtering, template variable substitution, and variable queries (`src/datasource.test.ts`).
- Go test coverage floor (60%) enforced in CI.
- `mage build:linuxARM64` target for local smoke testing on Apple Silicon.
- `RELEASING.md` documenting the release process and quality gates.

### Fixed
- `src/plugin.json` referenced a bundled dashboard that does not exist, which broke plugin validation; the version field is now stamped from `package.json` at build time via `%VERSION%`.
- The release workflow previously produced a zip without the compiled backend binaries or the required `<plugin-id>/` root directory, so it could not be installed into Grafana.
- README no longer suggests `grafana-cli plugins install`, which cannot work until the plugin is in the Grafana catalog.
- `docker-compose.yml` now mounts only `dist/` into Grafana (not the whole repo) and supports `GRAFANA_PORT`/`GRAFANA_VERSION` overrides.

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

[Unreleased]: https://github.com/honeycombio/grafana-honeycomb-datasource/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/honeycombio/grafana-honeycomb-datasource/releases/tag/v0.1.0
