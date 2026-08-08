package plugin

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/fingerprint"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

// runTracesQuery dispatches to either single-trace fetch or trace search.
func (d *Datasource) runTracesQuery(ctx context.Context, gq backend.DataQuery, pq HoneycombQuery) backend.DataResponse {
	resultType := pq.TracesResultType
	if resultType == "" {
		resultType = TracesResultTypeSingle
	}
	switch resultType {
	case TracesResultTypeSingle:
		return d.runTraceByID(ctx, gq, pq)
	case TracesResultTypeSearch:
		return d.runTraceSearch(ctx, gq, pq)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("unknown tracesResultType %q", resultType))
	}
}

// runTraceByID fetches every span in one trace and builds a Grafana trace
// data frame. Honeycomb stores spans as events, so we issue an events query
// filtered to the trace ID, with breakdowns of every column the trace data
// frame needs. We run with disable_series=false so the per-bucket Series
// response gives each span a real timestamp from Honeycomb (we cannot break
// down by `timestamp` directly — Honeycomb rejects it with 422).
func (d *Datasource) runTraceByID(ctx context.Context, gq backend.DataQuery, pq HoneycombQuery) backend.DataResponse {
	hq := honeycomb.Query{
		Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
		Filters: []honeycomb.Filter{
			{Column: "trace.trace_id", Op: "=", Value: pq.TraceID},
		},
		FilterCombination: "AND",
		Breakdowns:        traceSpanBreakdowns(),
		// Force fine-grained granularity so each span lands in its own
		// bucket. 1 second is the minimum Honeycomb supports for short
		// time ranges; for longer ranges they'll round up.
		Granularity: 1,
		Limit:       defaultedLimit(pq.Limit, 1000),
	}
	from, to := gq.TimeRange.From, gq.TimeRange.To
	from = clampFrom(from, to, d.settings.TimeWindowDays)
	fingerprint.ApplyTimeRange(&hq, from, to, hq.Granularity, 0)

	result, err := d.executeEventsQuery(ctx, pq.Dataset, hq, fingerprint.ExecutionSpec{
		QuerySpec: fingerprint.QuerySpec{
			DatasourceUID: d.uid,
			Dataset:       pq.Dataset,
			Query:         hq,
		},
		From:          from.Unix(),
		To:            to.Unix(),
		DisableSeries: false,
		Limit:         hq.Limit,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("trace fetch: %v", err))
	}

	frames, err := transform.ToTraceFrames(result, transform.TraceFrameOptions{
		QueryURL:    result.Links.QueryURL,
		APIURL:      d.settings.APIURL,
		Team:        d.settings.Team,
		Environment: d.settings.Environment,
		Dataset:     pq.Dataset,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform trace: %v", err))
	}
	return backend.DataResponse{Frames: frames}
}

// runTraceSearch returns a table of trace IDs matching the supplied filters,
// summarised by min/max timestamp + total span count + duration. Each trace
// ID gets the standard "Open trace in Honeycomb" deep link via the existing
// AttachTraceLinks path.
func (d *Datasource) runTraceSearch(ctx context.Context, gq backend.DataQuery, pq HoneycombQuery) backend.DataResponse {
	filters, err := toHoneycombFilters(pq.Filters)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("trace search filters: %v", err))
	}

	hq := honeycomb.Query{
		Calculations: []honeycomb.Calculation{
			{Op: "COUNT"},
			{Op: "MIN", Column: "duration_ms"},
			{Op: "MAX", Column: "duration_ms"},
		},
		Filters:           filters,
		FilterCombination: defaultedFilterCombination(pq.FilterCombination),
		Breakdowns:        []string{"trace.trace_id"},
		Orders:            []honeycomb.Order{{Op: "COUNT", Order: "descending"}},
		Limit:             defaultedLimit(pq.Limit, 50),
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
		DisableSeries: true,
		Limit:         hq.Limit,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("trace search: %v", err))
	}

	// Reuse the existing table transformer; AttachTraceLinks will spot the
	// trace.trace_id column and add the deep-link automatically.
	frames, err := transform.ToFrames(result, transform.FrameOptions{
		Mode:        transform.ModeTable,
		QueryURL:    result.Links.QueryURL,
		GraphURL:    result.Links.GraphImageURL,
		APIURL:      d.settings.APIURL,
		Team:        d.settings.Team,
		Environment: d.settings.Environment,
		Dataset:     pq.Dataset,
	})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("transform trace search: %v", err))
	}
	return backend.DataResponse{Frames: frames}
}

// traceSpanBreakdowns is the list of columns we ask Honeycomb to break
// down a single-trace query by. These map to Grafana's trace data frame
// fields (traceID, spanID, parentSpanID, operationName, serviceName,
// duration). Extra columns become per-span tags.
//
// We do not include `timestamp` here — Honeycomb rejects it as a breakdown
// column with 422. The span's start time is reconstructed from the bucket
// time of the Series entry it appears in (see ToTraceFrames).
func traceSpanBreakdowns() []string {
	return []string{
		"trace.trace_id",
		"trace.span_id",
		"trace.parent_id",
		"name",
		"service.name",
		"duration_ms",
		"status_code",
		"error",
		"span.kind",
	}
}
