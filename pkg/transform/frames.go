package transform

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// QueryMode controls how the Honeycomb result is mapped to Grafana frames.
type QueryMode string

const (
	// ModeTimeseries produces one frame per breakdown group, each with a time
	// column and one column per calculation. Use for time series panels.
	ModeTimeseries QueryMode = "timeseries"

	// ModeTable produces a single frame with all breakdown and calculation
	// columns as rows. Use for Table panels.
	ModeTable QueryMode = "table"

	// ModeStat produces a single frame with a single numeric value.
	// Picks the first calculation result from the first (or only) group.
	ModeStat QueryMode = "stat"

	// ModeLogs produces a single FrameTypeLogLines frame where each Honeycomb
	// result row becomes a log line. Required for the Logs panel and Explore
	// logs view. The first matching column from logBodyCandidates becomes the
	// log body; the first matching column from logTimeCandidates becomes the
	// timestamp; the first matching column from logSeverityCandidates becomes
	// the severity. All other columns are flattened into the body as
	// attributes.
	ModeLogs QueryMode = "logs"
)

// Column-name candidates for log frames, in priority order. First match wins.
var (
	logBodyCandidates     = []string{"body", "message", "msg", "log", "name"}
	logTimeCandidates     = []string{"timestamp", "time", "ts"}
	logSeverityCandidates = []string{"severity_text", "severity", "level", "log.level"}
)

// FrameOptions controls frame generation.
type FrameOptions struct {
	Mode     QueryMode
	QueryURL string // Honeycomb result deep link
	GraphURL string
	// MaxGroups limits the number of series in timeseries mode to prevent
	// UI overload. 0 means use DefaultMaxGroups.
	MaxGroups int

	// APIURL, Team, Environment, Dataset together let the transformer build
	// trace deep links to ui.honeycomb.io. If Team or Dataset are empty,
	// trace links are skipped.
	APIURL      string
	Team        string
	Environment string
	Dataset     string
}

const DefaultMaxGroups = 500

// ToFrames converts a Honeycomb QueryResultResponse to Grafana DataFrames.
//
// For timeseries mode: returns one frame per breakdown group. Each frame has
// a time field and one field per calculation. Labels are set to the breakdown
// column values for that group.
//
// For table mode: returns a single frame with breakdown columns + calculation
// columns as fields, one row per result entry.
//
// For stat mode: returns a single frame with a single value from the first
// calculation of the first result row.
func ToFrames(result *honeycomb.QueryResultResponse, opts FrameOptions) (data.Frames, error) {
	if result == nil || result.Data == nil {
		return data.Frames{emptyFrame(opts)}, nil
	}

	maxGroups := opts.MaxGroups
	if maxGroups <= 0 {
		maxGroups = DefaultMaxGroups
	}

	switch opts.Mode {
	case ModeTable:
		return toTableFrames(result, opts)
	case ModeStat:
		return toStatFrames(result, opts)
	case ModeLogs:
		return toLogsFrames(result, opts)
	default:
		return toTimeseriesFrames(result, opts, maxGroups)
	}
}

// ---------------------------------------------------------------------------
// Timeseries
// ---------------------------------------------------------------------------

// toTimeseriesFrames converts the series data into per-group Grafana frames.
func toTimeseriesFrames(result *honeycomb.QueryResultResponse, opts FrameOptions, maxGroups int) (data.Frames, error) {
	series := result.Data.Series
	if len(series) == 0 {
		return data.Frames{emptyFrame(opts)}, nil
	}

	// Identify all calculation column names (those that appear as metric values)
	// and all breakdown column names from the query spec.
	calcCols := calcColumnNames(result.Query.Calculations)
	breakdownCols := result.Query.Breakdowns

	// Build a group key → per-time data map.
	// group key is a canonical string encoding the breakdown values.
	type timePoint struct {
		t    time.Time
		vals map[string]float64 // calc column → value
	}
	type group struct {
		labels     data.Labels
		timePoints []timePoint
	}
	groups := make(map[string]*group)
	var groupOrder []string // for deterministic frame ordering

	for _, entry := range series {
		t := entry.Time.UTC()
		labels := extractBreakdowns(entry.Data, breakdownCols)
		key := groupKey(labels)

		g, ok := groups[key]
		if !ok {
			if len(groups) >= maxGroups {
				continue // silently drop groups beyond the limit
			}
			g = &group{labels: labels}
			groups[key] = g
			groupOrder = append(groupOrder, key)
		}

		vals := make(map[string]float64, len(calcCols))
		for _, col := range calcCols {
			if v, ok := entry.Data[col]; ok {
				vals[col] = toFloat64(v)
			}
		}
		g.timePoints = append(g.timePoints, timePoint{t: t, vals: vals})
	}

	if len(groups) == 0 {
		return data.Frames{emptyFrame(opts)}, nil
	}

	// Sort groups for a stable frame ordering across refreshes.
	sort.Strings(groupOrder)

	frames := make(data.Frames, 0, len(groups))
	for _, key := range groupOrder {
		g := groups[key]

		// Sort time points within each group.
		sort.Slice(g.timePoints, func(i, j int) bool {
			return g.timePoints[i].t.Before(g.timePoints[j].t)
		})

		timeVals := make([]time.Time, len(g.timePoints))
		for i, tp := range g.timePoints {
			timeVals[i] = tp.t
		}

		frame := data.NewFrame("", data.NewField("time", nil, timeVals))
		frame.SetMeta(&data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti})

		for _, col := range calcCols {
			fieldVals := make([]*float64, len(g.timePoints))
			for i, tp := range g.timePoints {
				if v, ok := tp.vals[col]; ok {
					vv := v
					fieldVals[i] = &vv
				}
			}
			field := data.NewField(col, g.labels, fieldVals)
			if field.Config == nil {
				field.Config = &data.FieldConfig{}
			}
			field.Config.DisplayNameFromDS = labeledName(col, g.labels)
			if u := unitForCalcByColumnName(col, result.Query.Calculations); u != "" {
				field.Config.Unit = u
			}
			frame.Fields = append(frame.Fields, field)
		}

		SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
		AttachDeepLink(frame, opts.QueryURL)
		AttachTraceLinks(frame, opts.APIURL, opts.Team, opts.Environment, opts.Dataset)
		frames = append(frames, frame)
	}

	return frames, nil
}

// ---------------------------------------------------------------------------
// Table
// ---------------------------------------------------------------------------

func toTableFrames(result *honeycomb.QueryResultResponse, opts FrameOptions) (data.Frames, error) {
	rows := result.Data.Results
	if len(rows) == 0 {
		return data.Frames{emptyFrame(opts)}, nil
	}

	breakdownCols := result.Query.Breakdowns
	calcCols := calcColumnNames(result.Query.Calculations)

	// Build fields.
	stringFields := make(map[string][]string, len(breakdownCols))
	floatFields := make(map[string][]*float64, len(calcCols))

	for _, col := range breakdownCols {
		stringFields[col] = make([]string, 0, len(rows))
	}
	for _, col := range calcCols {
		floatFields[col] = make([]*float64, 0, len(rows))
	}

	for _, row := range rows {
		for _, col := range breakdownCols {
			v := ""
			if raw, ok := row[col]; ok {
				v = fmt.Sprintf("%v", raw)
			}
			stringFields[col] = append(stringFields[col], v)
		}
		for _, col := range calcCols {
			if raw, ok := row[col]; ok {
				f := toFloat64(raw)
				floatFields[col] = append(floatFields[col], &f)
			} else {
				floatFields[col] = append(floatFields[col], nil)
			}
		}
	}

	frame := data.NewFrame("honeycomb")
	frame.Meta = &data.FrameMeta{Type: data.FrameTypeTable}

	for _, col := range breakdownCols {
		frame.Fields = append(frame.Fields, data.NewField(col, nil, stringFields[col]))
	}
	for _, col := range calcCols {
		field := data.NewField(col, nil, floatFields[col])
		if u := unitForCalcByColumnName(col, result.Query.Calculations); u != "" {
			if field.Config == nil {
				field.Config = &data.FieldConfig{}
			}
			field.Config.Unit = u
		}
		frame.Fields = append(frame.Fields, field)
	}

	SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
	AttachDeepLink(frame, opts.QueryURL)
	AttachTraceLinks(frame, opts.APIURL, opts.Team, opts.Environment, opts.Dataset)
	return data.Frames{frame}, nil
}

// ---------------------------------------------------------------------------
// Stat
// ---------------------------------------------------------------------------

func toStatFrames(result *honeycomb.QueryResultResponse, opts FrameOptions) (data.Frames, error) {
	rows := result.Data.Results
	calcCols := calcColumnNames(result.Query.Calculations)

	if len(rows) == 0 || len(calcCols) == 0 {
		return data.Frames{emptyFrame(opts)}, nil
	}

	col := calcCols[0]
	raw, ok := rows[0][col]
	if !ok {
		return data.Frames{emptyFrame(opts)}, nil
	}
	v := toFloat64(raw)

	field := data.NewField(col, nil, []*float64{&v})
	if field.Config == nil {
		field.Config = &data.FieldConfig{}
	}
	if u := unitForCalcByColumnName(col, result.Query.Calculations); u != "" {
		field.Config.Unit = u
	}
	frame := data.NewFrame("honeycomb", field)
	SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
	AttachDeepLink(frame, opts.QueryURL)
	AttachTraceLinks(frame, opts.APIURL, opts.Team, opts.Environment, opts.Dataset)
	return data.Frames{frame}, nil
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// toLogsFrames converts a Honeycomb result into a Grafana log frame.
//
// Honeycomb's Query Data API doesn't expose individual events; "logs" comes
// from running an events query with breakdowns of the user's selected
// attribute columns and reading the per-bucket Series response. Each
// (bucket × breakdown) entry becomes a log line and inherits the bucket time.
//
// We prefer Series data (when disable_series=false) because the bucket time
// gives each row a real timestamp from Honeycomb. Results-only data is the
// fallback path — the frame still renders in the Logs panel, but every row
// gets the wall clock at fetch as its time (lossy).
//
// Column auto-detection (first match wins, case-insensitive):
//   - body:      body, message, msg, log, name → frame's "Line" field
//   - severity:  severity_text, severity, level, log.level → optional column
//
// Remaining columns are joined into the body as `key=value` attribute pairs
// so users see them inline in the Logs panel without losing data.
func toLogsFrames(result *honeycomb.QueryResultResponse, opts FrameOptions) (data.Frames, error) {
	if result.Data == nil {
		return data.Frames{emptyLogsFrame(opts)}, nil
	}

	breakdownCols := result.Query.Breakdowns
	calcCols := calcColumnNames(result.Query.Calculations)
	allCols := append(append([]string{}, breakdownCols...), calcCols...)

	bodyCol := pickColumn(allCols, logBodyCandidates)
	severityCol := pickColumn(allCols, logSeverityCandidates)

	// Series path (preferred): bucket time supplies each log line's timestamp.
	if len(result.Data.Series) > 0 {
		entries := result.Data.Series
		times := make([]time.Time, 0, len(entries))
		bodies := make([]string, 0, len(entries))
		severities := make([]string, 0, len(entries))

		for _, entry := range entries {
			times = append(times, entry.Time.UTC())

			body := ""
			if bodyCol != "" {
				if raw, ok := entry.Data[bodyCol]; ok {
					body = fmt.Sprintf("%v", raw)
				}
			}
			var attrs []string
			for _, col := range allCols {
				if col == bodyCol || col == severityCol {
					continue
				}
				if raw, ok := entry.Data[col]; ok && raw != nil {
					attrs = append(attrs, fmt.Sprintf("%s=%v", col, raw))
				}
			}
			if len(attrs) > 0 {
				if body == "" {
					body = strings.Join(attrs, " ")
				} else {
					body = body + " " + strings.Join(attrs, " ")
				}
			}
			bodies = append(bodies, body)

			sev := "info"
			if severityCol != "" {
				if raw, ok := entry.Data[severityCol]; ok {
					sev = strings.ToLower(fmt.Sprintf("%v", raw))
				}
			}
			severities = append(severities, sev)
		}

		return data.Frames{buildLogsFrame(times, bodies, severities, opts)}, nil
	}

	// Results-only fallback. We still try to find a column-based timestamp
	// (in case the user explicitly broke down by a custom timestamp-like
	// column); otherwise everything gets the wall clock.
	rows := result.Data.Results
	if len(rows) == 0 {
		return data.Frames{emptyLogsFrame(opts)}, nil
	}

	timeCol := pickColumn(allCols, logTimeCandidates)
	now := time.Now().UTC()

	times := make([]time.Time, 0, len(rows))
	bodies := make([]string, 0, len(rows))
	severities := make([]string, 0, len(rows))

	for _, row := range rows {
		t := now
		if timeCol != "" {
			if raw, ok := row[timeCol]; ok {
				t = parseLogTime(raw, now)
			}
		}
		times = append(times, t)

		body := ""
		if bodyCol != "" {
			if raw, ok := row[bodyCol]; ok {
				body = fmt.Sprintf("%v", raw)
			}
		}
		var attrs []string
		for _, col := range allCols {
			if col == bodyCol || col == timeCol || col == severityCol {
				continue
			}
			if raw, ok := row[col]; ok && raw != nil {
				attrs = append(attrs, fmt.Sprintf("%s=%v", col, raw))
			}
		}
		if len(attrs) > 0 {
			if body == "" {
				body = strings.Join(attrs, " ")
			} else {
				body = body + " " + strings.Join(attrs, " ")
			}
		}
		bodies = append(bodies, body)

		sev := "info"
		if severityCol != "" {
			if raw, ok := row[severityCol]; ok {
				sev = strings.ToLower(fmt.Sprintf("%v", raw))
			}
		}
		severities = append(severities, sev)
	}

	return data.Frames{buildLogsFrame(times, bodies, severities, opts)}, nil
}

// buildLogsFrame is the shared frame-builder for both the Series and Results
// log paths.
func buildLogsFrame(times []time.Time, bodies, severities []string, opts FrameOptions) *data.Frame {
	frame := data.NewFrame(
		"honeycomb",
		data.NewField("Time", nil, times),
		data.NewField("Line", nil, bodies),
		data.NewField("severity", nil, severities),
	)
	frame.Meta = &data.FrameMeta{Type: data.FrameTypeLogLines}
	SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
	AttachDeepLink(frame, opts.QueryURL)
	AttachTraceLinks(frame, opts.APIURL, opts.Team, opts.Environment, opts.Dataset)
	return frame
}

// emptyLogsFrame returns an empty log-typed frame with the same field schema
// as a populated one. Some Grafana panels refuse to render unless the schema
// is present even when there are no rows.
func emptyLogsFrame(opts FrameOptions) *data.Frame {
	frame := data.NewFrame(
		"honeycomb",
		data.NewField("Time", nil, []time.Time{}),
		data.NewField("Line", nil, []string{}),
		data.NewField("severity", nil, []string{}),
	)
	frame.Meta = &data.FrameMeta{Type: data.FrameTypeLogLines}
	SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
	return frame
}

// pickColumn returns the first candidate that exists in cols (case-insensitive),
// or "" if none match.
func pickColumn(cols []string, candidates []string) string {
	lookup := make(map[string]string, len(cols))
	for _, c := range cols {
		lookup[strings.ToLower(c)] = c
	}
	for _, cand := range candidates {
		if real, ok := lookup[strings.ToLower(cand)]; ok {
			return real
		}
	}
	return ""
}

// parseLogTime accepts the common Honeycomb timestamp shapes and returns a
// UTC time.Time. Falls back to fallback on parse failure.
func parseLogTime(raw interface{}, fallback time.Time) time.Time {
	switch v := raw.(type) {
	case time.Time:
		return v.UTC()
	case float64:
		// Unix seconds (Honeycomb's most common shape) or milliseconds heuristic.
		if v > 1e12 {
			return time.UnixMilli(int64(v)).UTC()
		}
		return time.Unix(int64(v), 0).UTC()
	case int64:
		if v > 1e12 {
			return time.UnixMilli(v).UTC()
		}
		return time.Unix(v, 0).UTC()
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC()
		}
		// Try parsing as a Unix-seconds string.
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if n > 1e12 {
				return time.UnixMilli(n).UTC()
			}
			return time.Unix(n, 0).UTC()
		}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Annotations
// ---------------------------------------------------------------------------

// ToAnnotationFrames converts a Honeycomb result to annotation-compatible frames.
// Each result row becomes an annotation with the query URL as the URL.
func ToAnnotationFrames(result *honeycomb.QueryResultResponse, opts FrameOptions) (data.Frames, error) {
	if result == nil || result.Data == nil {
		return nil, nil
	}

	series := result.Data.Series
	if len(series) == 0 {
		return nil, nil
	}

	times := make([]time.Time, 0, len(series))
	texts := make([]string, 0, len(series))
	urls := make([]string, 0, len(series))
	// tags is a comma-separated string per annotation row (Grafana expects []string).
	tags := make([]string, 0, len(series))

	breakdownCols := result.Query.Breakdowns
	calcCols := calcColumnNames(result.Query.Calculations)

	for _, entry := range series {
		t := entry.Time.UTC()
		times = append(times, t)

		var parts []string
		for _, col := range calcCols {
			if v, ok := entry.Data[col]; ok {
				parts = append(parts, fmt.Sprintf("%s=%v", col, v))
			}
		}
		var tagParts []string
		for _, col := range breakdownCols {
			if v, ok := entry.Data[col]; ok {
				tagParts = append(tagParts, fmt.Sprintf("%v", v))
			}
		}

		texts = append(texts, strings.Join(parts, ", "))
		urls = append(urls, opts.QueryURL)
		tags = append(tags, strings.Join(tagParts, ","))
	}

	frame := data.NewFrame("annotations",
		data.NewField("time", nil, times),
		data.NewField("text", nil, texts),
		data.NewField("url", nil, urls),
		data.NewField("tags", nil, tags),
	)
	frame.Meta = &data.FrameMeta{Custom: map[string]interface{}{"isAnnotations": true}}
	return data.Frames{frame}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// calcColumnNames derives the display names for calculation columns from the
// query's calculation specs, matching how Honeycomb names them in results.
func calcColumnNames(calcs []honeycomb.Calculation) []string {
	seen := make(map[string]bool)
	var cols []string
	for _, c := range calcs {
		var name string
		if c.Alias != "" {
			name = c.Alias
		} else if c.Column != "" {
			name = fmt.Sprintf("%s(%s)", c.Op, c.Column)
		} else {
			name = c.Op
		}
		if !seen[name] {
			seen[name] = true
			cols = append(cols, name)
		}
	}
	return cols
}

// extractBreakdowns builds a Labels map from the breakdown columns in a series row.
func extractBreakdowns(row map[string]interface{}, breakdowns []string) data.Labels {
	if len(breakdowns) == 0 {
		return nil
	}
	labels := make(data.Labels, len(breakdowns))
	for _, col := range breakdowns {
		if v, ok := row[col]; ok && v != nil {
			labels[col] = fmt.Sprintf("%v", v)
		}
	}
	return labels
}

// groupKey produces a deterministic string key for a set of labels.
func groupKey(labels data.Labels) string {
	if len(labels) == 0 {
		return "__default__"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('|')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(labels[k])
	}
	return sb.String()
}

// labeledName produces a display name combining the column name and group labels.
func labeledName(col string, labels data.Labels) string {
	if len(labels) == 0 {
		return col
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(col)
	sb.WriteString(" {")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(labels[k])
	}
	sb.WriteByte('}')
	return sb.String()
}

// toFloat64 converts a JSON-decoded interface{} value to float64.
// JSON numbers are decoded as float64 by default; this handles the
// json.Number case too.
func toFloat64(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return 0
}

// emptyFrame returns a minimal valid frame to signal "no data" to Grafana.
func emptyFrame(opts FrameOptions) *data.Frame {
	frame := data.NewFrame("honeycomb")
	SetFrameMeta(frame, opts.QueryURL, opts.GraphURL)
	return frame
}
