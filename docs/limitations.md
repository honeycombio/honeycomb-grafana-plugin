# Known Limitations and Future Roadmap

## Current limitations

### API constraints

| Limitation | Detail |
|------------|--------|
| Create Query Result rate limit | Hard cap at 10 req/min per API key. The plugin mitigates this with caching and a token bucket, but cold-start dashboards with many unique queries will still queue. |
| Maximum time range | Honeycomb supports at most 7 days of historical data per query. |
| Query execution timeout | Honeycomb cancels queries that take longer than 10 seconds. |
| Compare time offset | The `compare_time_offset_seconds` field is sent in the query spec but Honeycomb's current Create Query Result API does not return comparison data in the result. When this API capability is added by Honeycomb, the plugin can be updated to surface comparison series. |
| Relational field queries | Queries using relational columns (dot notation like `root.response.status_code`) are subject to a stricter 1 req/min limit. The plugin's token bucket does not distinguish these. Until this is handled, relational field queries may see more 429 errors. |

### Cache limitations

| Limitation | Detail |
|------------|--------|
| In-process only | Cache is per-plugin-process. Multiple Grafana replicas each start cold. |
| Restart clears cache | In-memory only; Grafana restarts flush everything. |
| No manual invalidation | There is no UI to force-refresh a specific cache entry without restarting. |
| 24-hour L3 TTL | Completed results are cached for 24 hours. Queries that naturally produce changing results (e.g., live traffic) will show stale data after the TTL until the next cold execution. |

### Feature gaps

| Feature | Status |
|---------|--------|
| Exemplar frames | Not implemented. Grafana exemplars require trace/span IDs in result rows, which Honeycomb's query results API doesn't expose. DataLinks (deep links) are used instead. See [ADR-004](adr/ADR-004-deep-links.md). |
| Per-calculation filters | The visual editor supports top-level query filters only. Per-calculation filters (available in the Honeycomb API via `calculations[].filters`) require raw JSON mode. |
| Formula support | Mathematical formulas combining calculations are not exposed in the visual editor. Use raw JSON mode. |
| Havings | Post-aggregation filter (`havings`) is not in the visual editor. Use raw JSON mode. |
| Calculated fields | Derived column expressions (`calculated_fields`) are not in the visual editor. Use raw JSON mode. |
| HEATMAP visualization | While HEATMAP is a valid calculation op, mapping it to Grafana's heatmap panel type requires custom frame structure. Currently returns raw values. |
| Distributed cache | Cache is not shared across Grafana replicas. Redis-backed cache would require an operational dependency. |
| Streaming results | Honeycomb results are polled (max 30 s); streaming is not supported. |

### Platform limitations

| Limitation | Detail |
|------------|--------|
| Go binary required | The backend plugin is a compiled Go binary. It must be built for the target OS/architecture. Release zips include Linux (amd64, arm64), Darwin (amd64, arm64), and Windows (amd64) binaries. |
| Plugin signing | Unsigned plugin builds require `allow_loading_unsigned_plugins` in `grafana.ini`. Signed release versions will work without this setting. |

---

## Recommended best practices

### Designing queries for the cache

The cache is most effective when:
- Panels share the same query (or the same `execKey`)
- The time range is set to a "last N hours" window rather than a sliding absolute range
- The dashboard refresh interval is ≥ the Honeycomb granularity (to avoid meaningless re-queries)

Avoid:
- Using unique per-panel template variable combinations that prevent cache sharing
- Setting refresh intervals shorter than 60 seconds (cache TTL for short time ranges)
- Having >10 unique queries on a cold-loading dashboard (token bucket queuing)

### Group by cardinality

Honeycomb excels at high-cardinality data. The plugin supports up to `DefaultMaxGroups` (500) groups in timeseries mode. Beyond that, groups are silently dropped.

To handle high-cardinality breakdowns:
- Use **Limit** (e.g., 10) with **Order by** (e.g., COUNT descending) to get top-N series
- Use **table mode** (where `disable_series=true` unlocks limit up to 10000)
- Add filters to narrow the result set

### Dashboard variables

Use the `datasets` variable type to build a dataset picker:
```json
{ "queryType": "datasets" }
```

Then use `$dataset` in all panel queries. This lets users switch datasets without editing each panel.

---

## Roadmap

The following items are planned for future releases:

- [ ] **Per-calculation filter UI** — expose calculation-level filters in the visual editor
- [ ] **Formula editor** — visual UI for formula expressions
- [ ] **Having editor** — post-aggregation filter UI
- [ ] **HEATMAP visualization** — proper frame structure for Grafana heatmap panels
- [ ] **Relational field query support** — detect relational fields and apply a stricter per-token rate limit
- [ ] **Compare time offset visualization** — render comparison series when the Honeycomb API exposes comparison data in results
- [ ] **Configurable cache TTLs** — expose L1/L2/L3 TTLs as datasource settings
- [ ] **Redis cache backend** — optional distributed cache for multi-replica Grafana
- [ ] **Plugin signing** — submit to Grafana plugin catalogue with signature
- [ ] **E2E tests** — automated end-to-end tests against a live Honeycomb environment

Have a feature request? Open an issue at [github.com/honeycombio/honeycomb-grafana-plugin/issues](https://github.com/honeycombio/honeycomb-grafana-plugin/issues).
