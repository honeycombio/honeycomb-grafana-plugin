# Honeycomb Grafana Data Source Plugin

A production-grade Grafana backend data source plugin for [Honeycomb](https://www.honeycomb.io) that lets you query Honeycomb datasets directly from Grafana dashboards.

[![CI](https://github.com/honeycombio/honeycomb-grafana-plugin/actions/workflows/ci.yml/badge.svg)](https://github.com/honeycombio/honeycomb-grafana-plugin/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

---

## Features

- **Full Honeycomb Query API support** — calculations, filters, group-by (breakdowns), order-by, limit, granularity, compare time offset, havings
- **Five query types** — Metrics, SLO, Logs, Traces, and Raw JSON
- **Four panel modes for Metrics** — timeseries, table, stat, and logs (each row → log line)
- **SLO mode** — list all SLOs in a dataset or render a single SLO's compliance, budget remaining, status, and burn rate
- **Logs mode** — Honeycomb events rendered as `FrameTypeLogLines` for the Grafana Logs panel and Explore view
- **Traces mode** — fetch a trace by ID (full span tree → Grafana trace view) or search traces by attribute filters
- **First-class high-cardinality group-by** — each breakdown combination becomes a separate Grafana series; series are labeled, ordered, and limited with guardrails
- **Aggressive caching** — three-level cache (query_id → result_id → completed result) protects against Honeycomb's 10 req/min Create Query Result limit
- **Token-bucket rate limiter** — 8 tokens/60 s with automatic exponential backoff on 429
- **Singleflight deduplication** — concurrent panels requesting identical data collapse into a single Honeycomb API call
- **Deep links** — every metric field carries a DataLink to the matching Honeycomb query result; `trace.trace_id` columns get a per-row link straight to the trace in Honeycomb
- **Dashboard variables** — list datasets or columns; use the result as template variables across all panels
- **Annotation support** — use Honeycomb queries as annotation sources
- **Configurable time window** — clamp dashboard queries to a maximum range (default 7 days) before sending to Honeycomb
- **Health check** — validates API key and connectivity from Grafana's data source settings page
- **Secure secret handling** — API key is stored in Grafana's `secureJsonData` (encrypted at rest) and never sent to the browser
- **Raw JSON mode** — power users can paste raw Honeycomb Query API JSON
- **US and EU region support** — configurable API base URL

---

## Requirements

- Grafana ≥ 11.0.0
- A Honeycomb Configuration API key with **Manage Queries and Columns** and **Run Queries** permissions

---

## Installation

### From GitHub Releases (recommended)

1. Download the latest release zip from [Releases](https://github.com/honeycombio/honeycomb-grafana-plugin/releases)
   (optionally verify it against the published `.sha1` checksum).
2. Extract to your Grafana plugins directory:
   ```bash
   unzip honeycombio-honeycomb-datasource-<version>.zip -d /var/lib/grafana/plugins/
   ```
3. Releases are currently unsigned, so allow the plugin in `grafana.ini`
   (or via the `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS` environment variable):
   ```ini
   [plugins]
   allow_loading_unsigned_plugins = honeycombio-honeycomb-datasource
   ```
4. Restart Grafana.

The zip bundles backend binaries for linux (amd64/arm/arm64), macOS
(amd64/arm64), and Windows (amd64); Grafana picks the right one automatically.
The plugin is not yet in the Grafana plugin catalog, so `grafana-cli plugins
install` does not work yet — see [RELEASING.md](RELEASING.md).

### Via provisioning

See [provisioning/datasources/honeycomb.yaml](provisioning/datasources/honeycomb.yaml).

---

## Configuration

1. In Grafana, go to **Settings → Data sources → Add data source**.
2. Search for **Honeycomb** and select it.
3. Configure:
   - **API Region**: US (default) or EU; or enter a custom URL
   - **API Key**: your Honeycomb Configuration API key (stored encrypted)
   - **Team** *(optional)*: your Honeycomb team slug. Required for trace deep links to `ui.honeycomb.io`.
   - **Environment** *(optional)*: Honeycomb environment name. Leave blank for Classic accounts.
   - **Time Window (days)** *(optional, default 7)*: maximum query time window. Longer dashboard ranges are clamped before being sent to Honeycomb. `0` = unbounded.
4. Click **Save & test** to verify connectivity.

---

## Quick start: first query

1. Create a new dashboard and add a **Time series** panel.
2. Select the **Honeycomb** datasource.
3. Select your **Dataset**.
4. Add a **Calculation**: `COUNT`.
5. Add a **Group by** column: `service.name`.
6. Set **Limit** to 10 and **Order by** `COUNT` descending.
7. Click **Run query**.

Each `service.name` value will appear as a separate time series. Click any data point and then **Open in Honeycomb** to jump directly to the corresponding Honeycomb query result.

---

## Query types

Pick a Query Type tab in the editor:

| Type | Use for | Honeycomb endpoint |
|---|---|---|
| **Metrics** | Aggregations over events: `COUNT`, percentiles, rates, histograms (anything driven by Honeycomb's events query API) | `POST /1/queries`, `POST /1/query_results`, `GET /1/query_results/{id}` |
| **SLO** | List all SLOs in a dataset, or render one SLO's compliance, budget remaining, status, and burn rate | `GET /1/slos/{dataset}`, `GET /1/slos/{dataset}/{id}?detailed=true` |
| **Logs** | Honeycomb events rendered as Grafana log lines — pick filters and which attribute columns to surface; the rest are inlined as `key=value` pairs in the body. Outputs `FrameTypeLogLines`. | events query with `disable_series=true`, breakdowns of attribute columns |
| **Traces** | Trace by ID (full span tree → Grafana trace view) or Search (filter → table of trace IDs with Honeycomb deep links). | events query filtered to `trace.trace_id = <id>` (single) or breakdown by `trace.trace_id` (search) |
| **Raw** | Power users — paste raw Honeycomb Query API JSON. Supports template variables in the JSON body. | same as Metrics, body unmodified |

The `Query mode` selector inside Metrics drives frame shape:

| Mode | Output | Best panel |
|---|---|---|
| Time series | One frame per breakdown group, time + numeric fields | Time series, Bar chart |
| Table | Wide frame with breakdown + calc columns (limit up to 10 000) | Table |
| Stat | Single value | Stat, Gauge |
| Logs | `FrameTypeLogLines` with auto-detected `Time`/`Line`/`severity` columns | Logs panel, Explore logs view |

`Returned data` lets you override what Honeycomb returns (`auto` follows the mode; `series` / `result` / `both` are explicit).

### Quick examples

**P99 latency over time, by service:** Metrics → dataset `production` → calculation `P99(duration_ms)` → group by `service.name` → order by `P99(duration_ms)` desc → limit 10. Each service becomes its own series.

**Top error endpoints:** Metrics → query mode `Table` → calculations `COUNT`, `P99(duration_ms)` → filter `status_code >= 500` → group by `name`, `status_code` → order by `COUNT` desc → limit 50.

**Tail Honeycomb events as logs:** Logs → dataset `api-gateway-public` → optional filter `status_code >= 400` → optional "Show attributes" `name`, `service.name`, `trace.trace_id`, `error` → limit 1000. Renders in any Grafana Logs panel; trace IDs deep-link into Honeycomb.

**Render a trace by ID:** Traces → result type `Trace by ID` → paste the trace ID (or use a `${trace_id}` variable). Grafana's trace view draws the span tree from Honeycomb spans.

**Search recent traces:** Traces → result type `Search` → filter (e.g. `service.name = checkout-service`, `duration_ms > 1000`). Get a table of trace IDs ranked by span count; click any one to open in Honeycomb.

**SLO compliance for a single SLI:** SLO → dataset `production` → result type `Single SLO` → paste the SLO ID (visible in Honeycomb's URL).

---

## How caching works

Honeycomb limits Create Query Result to **10 requests/minute per team**. The plugin uses a three-level cache to minimize these calls:

| Level | Caches | Default TTL | Benefit |
|-------|--------|-------------|---------|
| L1 | Query spec → `query_id` | 30 min | Reuse the same Honeycomb query across time range changes |
| L2 | Execution context → `query_result_id` | 10 min | Skip re-submission for recently-run queries |
| L3 | `query_result_id` → completed result | 2 hours | Serve identical queries from memory without any Honeycomb call |

All TTLs are configurable in the data source settings.

On a steady-state dashboard, panel refreshes almost always hit L3 (in-memory, microsecond latency). Only cold cache loads or new time ranges hit the rate-limited Create Query Result endpoint.

See [ADR-002](docs/adr/ADR-002-caching-strategy.md) for full details.

---

## Dashboard variables

Use the Honeycomb datasource for dashboard variable queries:

| Variable type | Query | Returns |
|--------------|-------|---------|
| Datasets | `{ "queryType": "datasets" }` | All dataset slug/name pairs |
| Columns | `{ "queryType": "columns", "dataset": "production" }` | Column names for a dataset |

Variables can be used in any string field: `dataset: $dataset`, `breakdowns: ["$column"]`.

---

## Deep links

This plugin attaches two flavors of `DataLink`:

1. **Open in Honeycomb (per-value)** — every numeric metric field gets a link to the Honeycomb query result page Honeycomb returned alongside the data. Click any cell value or hover a point's tooltip and follow the link to land on the same query in Honeycomb's UI.
2. **Open trace in Honeycomb (per-row)** — `trace.trace_id` and `trace_id` columns get a link templated to `https://ui.honeycomb.io/<team>/environments/<environment>/datasets/<dataset>/trace?trace_id=<value>`. Requires **Team** set on the datasource (and **Environment** for non-Classic accounts).

The UI host is derived from the API URL (`api.honeycomb.io` → `ui.honeycomb.io`, `api.eu1.honeycomb.io` → `ui.eu1.honeycomb.io`).

See [ADR-004](docs/adr/ADR-004-deep-links.md) for the design rationale.

---

## Known limitations

- Cache is in-process; it resets on Grafana restart and is not shared across Grafana replicas.
- Compare time offset (`compare_time_offset_seconds`) submits the comparison window, but Honeycomb's current API does not return comparison data in query result responses.
- Exemplar frames (native Grafana exemplar semantics) are not implemented; deep links via DataLinks are used instead. See [ADR-004](docs/adr/ADR-004-deep-links.md).
- Default query history is 7 days (configurable via the **Time Window** setting). Honeycomb's standard plan limit is 60 days.
- Granularity must be within Honeycomb's valid range: `time_range/1000` to `time_range/1` seconds.
- Per-calculation filters (`Calculation.filters`) require the Honeycomb Metrics Beta feature flag on your team; without it Honeycomb rejects the query with a 4xx.
- SLO mode requires a Configuration Key with `Manage SLOs` read permission in addition to the standard query permissions.

---

## Local development

See [CONTRIBUTING.md](CONTRIBUTING.md) for full setup instructions.

```bash
# Build backend + frontend, start Grafana:
mage build:darwin && npm run build
docker-compose up
# Open http://localhost:3000
```

---

## Architecture

See the [Architecture Decision Records](docs/adr/) for the major design decisions:

- [ADR-001](docs/adr/ADR-001-backend-plugin.md) — Why a backend plugin
- [ADR-002](docs/adr/ADR-002-caching-strategy.md) — Multi-level caching strategy
- [ADR-003](docs/adr/ADR-003-rate-limiting.md) — Rate limiting strategy
- [ADR-004](docs/adr/ADR-004-deep-links.md) — Deep link and exemplar strategy

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
