package transform

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// TraceFrameOptions controls trace-frame generation.
type TraceFrameOptions struct {
	QueryURL    string
	APIURL      string
	Team        string
	Environment string
	Dataset     string
}

// ToTraceFrames converts a Honeycomb result whose rows are individual spans
// into a Grafana trace data frame.
//
// The plugin runs the underlying query with disable_series=false so that
// each span lands in its own (small) bucket. We prefer Series data, where
// the bucket time supplies each span's startTime; we fall back to Results
// when Series is empty (rare, but possible if granularity rounded to a
// large bucket and no events fell in it).
//
// Honeycomb column → Grafana field:
//
//	trace.trace_id    → traceID
//	trace.span_id     → spanID
//	trace.parent_id   → parentSpanID
//	name              → operationName
//	service.name      → serviceName
//	(bucket time)     → startTime
//	duration_ms       → duration
//	(everything else) → tags
//
// Sets frame.Meta.PreferredVisualisation = VisTypeTrace so Grafana's traces
// panel and Explore "Traces" view render it natively.
func ToTraceFrames(result *honeycomb.QueryResultResponse, opts TraceFrameOptions) (data.Frames, error) {
	if result == nil || result.Data == nil {
		return data.Frames{emptyTraceFrame(opts)}, nil
	}

	// Prefer Series (bucket time → startTime). Fall back to Results.
	rows := flattenSpans(result.Data)
	if len(rows) == 0 {
		return data.Frames{emptyTraceFrame(opts)}, nil
	}
	n := len(rows)

	traceIDs := make([]string, n)
	spanIDs := make([]string, n)
	parentIDs := make([]string, n)
	opNames := make([]string, n)
	serviceNames := make([]string, n)
	startTimes := make([]float64, n) // ms epoch
	durations := make([]float64, n)
	tags := make([]json.RawMessage, n)

	// Stable ordering by start time helps Grafana render the timeline left
	// to right; if startTime missing, fall back to spanID for determinism.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].startMs != rows[j].startMs {
			return rows[i].startMs < rows[j].startMs
		}
		return stringValue(rows[i].data["trace.span_id"]) < stringValue(rows[j].data["trace.span_id"])
	})

	for i, row := range rows {
		traceIDs[i] = stringValue(row.data["trace.trace_id"])
		spanIDs[i] = stringValue(row.data["trace.span_id"])
		parentIDs[i] = stringValue(row.data["trace.parent_id"])
		opNames[i] = stringValue(row.data["name"])
		serviceNames[i] = stringValue(row.data["service.name"])
		startTimes[i] = row.startMs
		durations[i] = toFloat64(row.data["duration_ms"])
		tags[i] = buildTagsJSON(row.data, traceCoreColumns)
	}

	frame := data.NewFrame(
		"Trace",
		data.NewField("traceID", nil, traceIDs),
		data.NewField("spanID", nil, spanIDs),
		data.NewField("parentSpanID", nil, parentIDs),
		data.NewField("operationName", nil, opNames),
		data.NewField("serviceName", nil, serviceNames),
		data.NewField("startTime", nil, startTimes),
		data.NewField("duration", nil, durations),
		data.NewField("tags", nil, tags),
	)
	frame.Meta = &data.FrameMeta{
		PreferredVisualization: data.VisTypeTrace,
	}

	SetFrameMeta(frame, opts.QueryURL, "")
	AttachTraceLinks(frame, opts.APIURL, opts.Team, opts.Environment, opts.Dataset)
	return data.Frames{frame}, nil
}

func emptyTraceFrame(opts TraceFrameOptions) *data.Frame {
	frame := data.NewFrame(
		"Trace",
		data.NewField("traceID", nil, []string{}),
		data.NewField("spanID", nil, []string{}),
		data.NewField("parentSpanID", nil, []string{}),
		data.NewField("operationName", nil, []string{}),
		data.NewField("serviceName", nil, []string{}),
		data.NewField("startTime", nil, []float64{}),
		data.NewField("duration", nil, []float64{}),
		data.NewField("tags", nil, []json.RawMessage{}),
	)
	frame.Meta = &data.FrameMeta{
		PreferredVisualization: data.VisTypeTrace,
	}
	SetFrameMeta(frame, opts.QueryURL, "")
	return frame
}

// traceCoreColumns are the columns that map to Grafana's named trace fields
// rather than the generic tags blob. Anything outside this set lands in tags.
var traceCoreColumns = map[string]struct{}{
	"trace.trace_id":  {},
	"trace.span_id":   {},
	"trace.parent_id": {},
	"name":            {},
	"service.name":    {},
	"timestamp":       {},
	"duration_ms":     {},
	"COUNT":           {}, // the synthetic COUNT calc we added to satisfy the API
}

// spanRow is a unified view of a span — produced from either a Series entry
// (preferred, bucket time → startMs) or a Results row (fallback, no time).
type spanRow struct {
	startMs float64
	data    map[string]interface{}
}

// flattenSpans pulls one spanRow per span out of the Honeycomb response,
// preferring Series data.
//
//   - Series entries carry their own bucket time, which we convert to ms
//     and use as startTime. Each entry's `data` already contains the
//     breakdown values + COUNT, which is exactly what we need.
//   - Results rows are the legacy fallback (used when disable_series=true,
//     e.g. existing tests). startMs is parsed from a `timestamp` column if
//     present; otherwise 0.
func flattenSpans(rd *honeycomb.ResultData) []spanRow {
	if len(rd.Series) > 0 {
		out := make([]spanRow, 0, len(rd.Series))
		for _, e := range rd.Series {
			out = append(out, spanRow{
				startMs: float64(e.Time.UTC().UnixMilli()),
				data:    e.Data,
			})
		}
		return out
	}
	if len(rd.Results) == 0 {
		return nil
	}
	out := make([]spanRow, 0, len(rd.Results))
	for _, r := range rd.Results {
		out = append(out, spanRow{
			startMs: parseEpochMs(r["timestamp"]),
			data:    map[string]interface{}(r),
		})
	}
	return out
}

// buildTagsJSON emits the per-span tags array that Grafana's trace view
// renders in the span detail pane. The shape matches Grafana's
// {"key":"...", "value":"..."} expectation.
func buildTagsJSON(row honeycomb.ResultEntry, exclude map[string]struct{}) json.RawMessage {
	type kv struct {
		Key   string      `json:"key"`
		Value interface{} `json:"value"`
	}
	tags := make([]kv, 0, len(row))
	for k, v := range row {
		if _, skip := exclude[k]; skip {
			continue
		}
		if v == nil {
			continue
		}
		tags = append(tags, kv{Key: k, Value: v})
	}
	// Sort for stable diffs across renders.
	sort.Slice(tags, func(i, j int) bool { return tags[i].Key < tags[j].Key })
	b, err := json.Marshal(tags)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// parseEpochMs accepts Honeycomb timestamp shapes (Unix seconds, ms, or
// ISO strings) and returns milliseconds since epoch — the unit Grafana's
// trace data frame expects for startTime.
func parseEpochMs(raw interface{}) float64 {
	switch v := raw.(type) {
	case float64:
		// Heuristic: > 1e12 → already ms.
		if v > 1e12 {
			return v
		}
		return v * 1000
	case int64:
		if v > 1e12 {
			return float64(v)
		}
		return float64(v) * 1000
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return float64(t.UnixMilli())
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return float64(t.UnixMilli())
		}
	}
	return 0
}

func stringValue(raw interface{}) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	// Trim wrapping quotes for primitives.
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
