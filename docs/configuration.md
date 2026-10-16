# Configuration Reference

## Data source settings

### Connection settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| API Region | Select | US | Choose `US (api.honeycomb.io)` or `EU (api.eu1.honeycomb.io)`, or enter a custom URL. |
| Custom API URL | String | — | Shown only if "Custom" is selected. Must be a valid HTTPS URL. |
| API Key | SecretString | — | Your Honeycomb Configuration API key. Stored encrypted; never returned to the browser. |

### Advanced settings

| Setting | jsonData key | Default | Description |
|---------|--------------|---------|-------------|
| Time Window (days) | `timeWindowDays` | 7 | Maximum query time window. Longer ranges are clamped. 0 = unbounded. |
| Rate Limit (queries/minute) | `rateLimitPerMinute` | 8 | Maximum Create Query Result calls per minute from this data source. Honeycomb's default limit is 10/min per team; if Honeycomb has raised yours, you can set this higher. Lower it if other clients share the budget. |

### API key requirements

The API key needs these Honeycomb permissions:
- **Manage Queries and Columns** — required to create query specifications (POST /1/queries)
- **Run Queries** — required to execute queries (POST /1/query_results)
- **Read** on each dataset you want to query

Create a dedicated key for the Grafana integration at **Honeycomb → Settings → API Keys**.

### Provisioning

To provision the datasource programmatically:

```yaml
# /etc/grafana/provisioning/datasources/honeycomb.yaml
apiVersion: 1

datasources:
  - name: Honeycomb Production
    type: honeycombio-honeycomb-datasource
    access: proxy
    jsonData:
      apiUrl: https://api.honeycomb.io   # or https://api.eu1.honeycomb.io
    secureJsonData:
      apiKey: "${HONEYCOMB_API_KEY}"     # from environment; never hardcode
    version: 1
    editable: true
```

Set `HONEYCOMB_API_KEY` in the Grafana server environment.

---

## Query settings

### Query fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| Dataset | String | — | The Honeycomb dataset slug to query. Required. Supports template variables (`$dataset`). |
| Query mode | Select | timeseries | Controls how results are mapped to Grafana frames. |
| Calculations | List | COUNT | Aggregation operations to compute. At least one required. |
| Filters | List | — | Restricts which events are included. Combined with AND or OR. |
| Filter combination | AND/OR | AND | How multiple top-level filters are combined. |
| Group by | List | — | Columns to break results down by (Honeycomb breakdowns). |
| Order by | List | — | Sort terms; reference calculation ops or breakdown columns. |
| Limit | Integer | 100 | Maximum result groups (1–10000). |
| Granularity | Integer | 0 (auto) | Time bucket size in seconds. 0 derives automatically from the time range. |
| Compare time offset | Integer | 0 | Historical comparison window in seconds. One of: 1800, 3600, 7200, 28800, 86400, 604800, 2419200, 15724800. |
| Raw JSON mode | Toggle | off | Bypass the visual editor and send raw Honeycomb Query API JSON. |

### Query modes

| Mode | Grafana panel types | Honeycomb data | Frame structure |
|------|---------------------|----------------|-----------------|
| timeseries | Time series, Graph | `series` data | One frame per breakdown group; time + metric fields |
| table | Table | `results` data | Single frame; breakdown + metric columns |
| stat | Stat, Gauge | `results` data | Single frame; single value |

**Note:** `table` and `stat` modes automatically set `disable_series=true` in the Honeycomb API request, which reduces response payload and enables higher result limits.

### Calculations

| Operation | Column required | Description |
|-----------|----------------|-------------|
| COUNT | No | Number of matching events |
| CONCURRENCY | No | Concurrent events |
| SUM | Yes | Sum of a numeric column |
| AVG | Yes | Average of a numeric column |
| COUNT_DISTINCT | Yes | Distinct values |
| MAX / MIN | Yes | Maximum or minimum value |
| P50, P75, P90, P95, P99, P99.9 | Yes | Percentiles |
| HEATMAP | Yes | Distribution heatmap |
| RATE_AVG, RATE_SUM, RATE_MAX | Yes | Rate calculations |

### Filter operators

`=`, `!=`, `>`, `>=`, `<`, `<=`, `starts-with`, `does-not-start-with`, `ends-with`, `does-not-end-with`, `exists`, `does-not-exist`, `contains`, `does-not-contain`, `in`, `not-in`

Operators `exists` and `does-not-exist` do not take a value.

---

## Environment variables

The plugin does not read any environment variables for per-datasource configuration. All per-datasource settings are stored in Grafana's database (with secrets encrypted). This is by design: environment variables provide no isolation between multiple configured datasource instances.

For provisioning, use Grafana's native `${ENV_VAR}` interpolation in provisioning YAML files.
