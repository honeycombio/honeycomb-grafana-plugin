package plugin

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/fingerprint"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

// runLogsQuery executes a "logs" Query Type — a Honeycomb events query with
// breakdowns of the user-selected attribute columns. Each (bucket × breakdown)
// row in the timeseries response becomes a Grafana log line; the bucket time
// supplies the log line's Time field, so we don't need to break down by
// `timestamp` (which Honeycomb rejects with 422 "unknown column or derived
// column timestamp").
//
// This shares the same cache and rate-limit machinery as the standard
// metrics path because it ultimately hits the same /1/queries +
// /1/query_results endpoints.
func (d *Datasource) runLogsQuery(ctx context.Context, gq backend.DataQuery, pq HoneycombQuery) backend.DataResponse {
	// User picks attributes; if none, fall back to the default set.
	attrs := pq.LogsAttributes
	if len(attrs) == 0 {
		attrs = defaultLogAttributes()
	}

	filters, err := toHoneycombFilters(pq.Filters)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("logs filters: %v", err))
	}

	hq := honeycomb.Query{
		Calculations:      []honeycomb.Calculation{{Op: "COUNT"}},
		Breakdowns:        attrs,
		Filters:           filters,
		FilterCombination: defaultedFilterCombination(pq.FilterCombination),
		Limit:             defaultedLimit(pq.Limit, 1000),
	}
	from, to := gq.TimeRange.From, gq.TimeRange.To
	from = clampFrom(from, to, d.settings.TimeWindowDays)
	fingerprint.ApplyTimeRange(&hq, from, to, pq.Granularity, gq.MaxDataPoints)

	result, err := d.executeEventsQuery(ctx, pq.Dataset, hq, fingerprint.ExecutionSpec{
		QuerySpec: fingerprint.QuerySpec{
			DatasourceUID: d.uid,
			Dataset:       pq.Dataset,
			Query:         hq,
		},
		From:          from.Unix(),
		To:            to.Unix(),
		DisableSeries: false, // need Series for per-row time
		Limit:         hq.Limit,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("logs query: %v", err))
	}

	frames, err := transform.ToFrames(result, transform.FrameOptions{
		Mode:        transform.ModeLogs,
		QueryURL:    result.Links.QueryURL,
		GraphURL:    result.Links.GraphImageURL,
		APIURL:      d.settings.APIURL,
		Team:        d.settings.Team,
		Environment: d.settings.Environment,
		Dataset:     pq.Dataset,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform logs: %v", err))
	}
	return backend.DataResponse{Frames: frames}
}

// defaultLogAttributes is the breakdown set we use when the user hasn't
// picked specific attribute columns. These are common OpenTelemetry-shaped
// fields that appear in most Honeycomb datasets. Users can override via
// the LogsEditor.
//
// Do not include "timestamp": Honeycomb rejects it as a breakdown with
// 422 "unknown column or derived column timestamp". Each event's time is
// implicit and surfaces via the Series response when we run with
// disable_series=false (see runLogsQuery).
func defaultLogAttributes() []string {
	return []string{
		"name",
		"service.name",
		"trace.trace_id",
		"trace.span_id",
		"status_code",
		"error",
		"duration_ms",
	}
}

// executeEventsQuery is a small wrapper around the existing three-level
// cache + rate-limit flow used by runQuery for metrics queries. It exists
// so logs/traces can reuse the same infrastructure without duplicating it.
func (d *Datasource) executeEventsQuery(
	ctx context.Context,
	dataset string,
	hq honeycomb.Query,
	spec fingerprint.ExecutionSpec,
) (*honeycomb.QueryResultResponse, error) {
	execKey := fingerprint.ExecutionKey(spec)

	// L3 cache check.
	if v, ok := d.cache.Get(fingerprint.CompletedResultKey(execKey)); ok {
		return v.(*honeycomb.QueryResultResponse), nil
	}

	shapeKey := fingerprint.QueryShapeKey(spec.QuerySpec)
	queryID, err := d.getOrCreateQueryID(ctx, dataset, hq, shapeKey)
	if err != nil {
		return nil, fmt.Errorf("get query ID: %w", err)
	}

	// Build a thin pq view for getOrCreateQueryResultID — only Limit and
	// ShouldDisableSeries() are read.
	pqShim := HoneycombQuery{Limit: spec.Limit}
	if spec.DisableSeries {
		pqShim.QueryMode = "table" // forces ShouldDisableSeries() = true
	}

	queryResultID, err := d.getOrCreateQueryResultID(ctx, dataset, queryID, pqShim, execKey)
	if err != nil {
		return nil, fmt.Errorf("get query result ID: %w", err)
	}

	result, err := d.client.GetQueryResult(ctx, dataset, queryResultID)
	if err != nil {
		d.cache.Delete(execKey + ":resultid")
		return nil, fmt.Errorf("poll query result: %w", err)
	}

	d.cache.Set(fingerprint.CompletedResultKey(execKey), result, d.ttlL3)
	return result, nil
}

func defaultedFilterCombination(s string) string {
	if s == "" {
		return "AND"
	}
	return s
}

func defaultedLimit(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// dataLinkPlaceholder is a no-op import-prevention to keep go imports honest
// when this file is the only consumer of data in an early build.
var _ = data.Frames(nil)
