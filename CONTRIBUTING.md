# Contributing to honeycomb-grafana-plugin

Thank you for contributing! This guide covers everything you need to set up a local development environment, run tests, and submit a pull request.

## Prerequisites

| Tool | Minimum version | Notes |
|------|----------------|-------|
| Go   | 1.22           | `go version` |
| Node | 20             | `node --version` |
| npm  | 10             | bundled with Node 20 |
| Mage | 1.15           | `go install github.com/magefile/mage@latest` |
| Docker | any recent | for local Grafana |

## Local development

### 1. Clone and install

```bash
git clone https://github.com/honeycombio/honeycomb-grafana-plugin
cd honeycomb-grafana-plugin
npm install
go mod download
```

### 2. Build

```bash
# Build the Go backend binary (Linux; use build:darwin for macOS):
mage build:darwin

# Build the TypeScript frontend:
npm run build
```

For a watch-mode development cycle:

```bash
# Terminal 1 – rebuild frontend on changes:
npm run dev

# Terminal 2 – rebuild backend when Go files change (requires gow or similar):
# Or just re-run mage build:darwin after changes.
```

### 3. Run Grafana locally

```bash
docker-compose up
```

Open http://localhost:3000 (admin/admin). Add the Honeycomb datasource via Settings → Data sources.

> **Note:** The Docker Compose config sets `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS` so unsigned development builds work locally.

### 4. Run tests

```bash
# Go tests (scoped to pkg/ — ./... would traverse node_modules):
go test -v -race ./pkg/...

# TypeScript tests:
npm test

# Lint (Go):
golangci-lint run

# Lint (TypeScript):
npm run lint

# Type check:
npm run typecheck
```

### 5. End-to-end tests (Playwright)

E2E tests drive a real Grafana instance in a real browser using
[`@grafana/plugin-e2e`](https://grafana.com/developers/plugin-tools/e2e-test-a-plugin/get-started).

```bash
# One-time: install the browser
npx playwright install chromium

# Build the plugin and start Grafana (use build:linuxARM64 on Apple Silicon)
npm run build && mage build:linux
docker compose up -d

# Run the tests (GRAFANA_URL defaults to http://localhost:3000;
# use GRAFANA_PORT=3001 docker compose up -d if 3000 is taken)
npm run e2e
npm run e2e:report   # view the HTML report
```

> **Tip:** if e2e tests behave oddly, reset Grafana's state with
> `docker compose down -v` — the persistent volume accumulates datasources
> from previous runs, which can confuse the datasource picker.

CI runs the e2e suite against both the minimum supported Grafana version and
`latest`.

### 6. Package and smoke test a release artifact

To verify your change survives the full release path locally:

```bash
npm run build && mage build:all   # build:all so the zip covers your host arch
./scripts/package.sh              # produces build/<plugin-id>-<version>.zip
./scripts/smoke-test.sh           # installs the zip into a real Grafana container
```

CI runs both on every PR, so a green PR means a releasable commit. See
[RELEASING.md](RELEASING.md) for the release process itself.

## Code organization

```
pkg/
  honeycomb/    Honeycomb API client (HTTP, types, errors)
  fingerprint/  Query normalization and cache key generation
  cache/        TTL cache + singleflight deduplication
  ratelimit/    Token bucket limiter (8 tokens/60 s)
  transform/    Honeycomb response → Grafana DataFrames, deep links
  plugin/       Grafana backend datasource (QueryData, CheckHealth, CallResource)

src/
  components/   React query editor, config editor, variable editor
  datasource.ts Frontend datasource class (template vars, filter, metadata)
  types.ts      Shared TypeScript types
  module.ts     Plugin entry point
```

## Making changes

1. **Open an issue** first for non-trivial changes to discuss the approach.
2. **Fork** the repository and create a branch: `git checkout -b fix/my-bug`.
3. **Make your changes** and add/update tests.
4. **Run the full test suite** before submitting.
5. **Update CHANGELOG.md** under `[Unreleased]`.
6. **Open a PR** with a clear description of what changed and why.

## Commit conventions

We use conventional commits loosely:
- `fix: ...` for bug fixes
- `feat: ...` for new features
- `refactor: ...` for internal changes with no behaviour change
- `docs: ...` for documentation only
- `test: ...` for test additions

## Versioning

This project follows [Semantic Versioning](https://semver.org). Breaking changes to the query model (stored in Grafana's panel JSON) require a major version bump and a migration note in `CHANGELOG.md`.

## Security issues

Please see [SECURITY.md](SECURITY.md) for how to report security vulnerabilities. Do **not** open public GitHub issues for security bugs.
