# Usage Guide

## Time series panel

Best for: visualizing metric trends over time with optional high-cardinality breakdown.

### Example: P99 latency by service

1. Add a **Time series** panel.
2. Select the **Honeycomb** datasource.
3. **Dataset:** `production`
4. **Calculations:** `P99` on column `duration_ms`
5. **Group by:** `service.name`
6. **Order by:** `P99(duration_ms)` descending
7. **Limit:** `10` (top 10 services)
8. **Granularity:** `0` (auto)
9. Click **Run query**

Each `service.name` value produces a separate series. The legend shows `P99(duration_ms) {service.name=api-server}`.

Click any data point → **Open in Honeycomb** to jump to the corresponding Honeycomb query result.

---

## Table panel

Best for: ranked breakdowns, multi-metric summaries.

**Key difference from timeseries:** table mode sends `disable_series=true` to Honeycomb, which disables timeseries data but allows limits up to 10000 (vs 1000 for timeseries mode).

### Example: Top endpoints by error rate

1. Add a **Table** panel.
2. Query mode: **Table**
3. **Calculations:** `COUNT`, `P99` on `duration_ms`
4. **Filters:** `http.status_code >= 400`
5. **Group by:** `http.target`, `service.name`
6. **Order by:** `COUNT` descending
7. **Limit:** `50`

The table has columns: `http.target`, `service.name`, `COUNT`, `P99(duration_ms)`.

---

## Stat panel

Best for: single aggregate values, SLO-style metrics.

### Example: Total request count in the selected time range

1. Add a **Stat** panel.
2. Query mode: **Stat**
3. **Calculations:** `COUNT`
4. No breakdowns, no filters
5. **Limit:** `1`

Returns a single number representing total events in the panel's time window.

---

## Using dashboard variables

### Create a dataset variable

1. Go to **Dashboard settings → Variables → Add variable**.
2. Type: **Query**
3. Data source: **Honeycomb**
4. Query type: **Datasets**
5. Name: `dataset`
6. Save

Now `$dataset` is available in all panel queries. Users can switch datasets from the dashboard dropdown.

### Create a column variable

1. Add another variable with type **Query**.
2. Query type: **Columns**
3. Dataset: `$dataset` (uses the dataset variable)
4. Name: `breakdown_column`

Use `$breakdown_column` in the **Group by** field of your panel queries to let users choose the breakdown dimension interactively.

---

## Raw JSON mode

Power users can bypass the visual editor and write raw Honeycomb Query API JSON:

```json
{
  "calculations": [
    {"op": "COUNT"},
    {"op": "P99", "column": "duration_ms", "alias": "p99_latency"}
  ],
  "breakdowns": ["service.name", "http.status_code"],
  "filters": [
    {"column": "http.status_code", "op": ">=", "value": 400}
  ],
  "filter_combination": "AND",
  "orders": [
    {"op": "COUNT", "order": "descending"}
  ],
  "limit": 25
}
```

Time range is NOT specified in raw JSON — it is applied by the plugin from Grafana's panel time range. Dashboard template variables (`$var`) are substituted before the JSON is sent.

---

## Annotations

Use Honeycomb queries as annotation sources:

1. Open **Dashboard settings → Annotations → Add annotation rule**.
2. Data source: **Honeycomb**
3. Configure a query (same fields as a panel query).
4. The annotation title is built from calculation values; clicking the annotation opens the Honeycomb query result.

---

## Deep links

Every Honeycomb-backed panel includes a deep link to the Honeycomb query result. To use it:

1. Hover over any data point in a timeseries or table.
2. In the tooltip or context menu, click **Open in Honeycomb**.
3. The Honeycomb UI opens in a new tab, showing the exact query result that produced the data.

The link is persistent — you can bookmark it or share it with colleagues.

---

## Alerting

This plugin supports Grafana Alerting. To create an alert:

1. In a panel, switch to the **Alert** tab.
2. Define an alert condition based on the query result.
3. Grafana evaluates the alert on the configured schedule, using the same backend plugin code path as panel queries (including caching).

**Note on caching and alerting:** The cache serves both panel queries and alert evaluations. An alert query that misses the cache can wait in Honeycomb's query queue, and Grafana fails an evaluation that takes longer than `[unified_alerting] evaluation_timeout` (30 s by default). Space alert evaluation intervals to account for your team's query rate (10 per minute by default).

---

## Best queries for this plugin

The plugin works best for:

- **Aggregate timeseries**: `COUNT`, `AVG`, `P99` over time — the plugin's cache provides maximum benefit here
- **High-cardinality breakdown tables**: top-N by service, endpoint, user, region — use table mode for limits up to 10000
- **SLO stat panels**: single value with no breakdown, refreshed infrequently
- **Dashboard variable population**: `datasets` and `columns` queries are cheap and cached separately

The plugin works least well for:
- **Extremely high refresh rates** (< 60 s) — the L2/L3 cache still helps, but cold-start latency is 1–3 s per unique query
- **Large number of distinct queries** (more than 10 unique queries/minute on a cold dashboard) — later queries wait in Honeycomb's query queue
- **Real-time streaming** — not supported; polling max 10 minutes per query
