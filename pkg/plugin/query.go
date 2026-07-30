package plugin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

// QueryType selects the top-level Honeycomb query kind. "metrics" is the
// default (events / aggregations via the Query Data API); "slo" hits the
// SLO endpoints; "logs" and "traces" build dedicated UX over the Query Data
// API; "raw" passes RawJSON through verbatim.
const (
	QueryTypeMetrics = "metrics"
	QueryTypeSLO     = "slo"
	QueryTypeLogs    = "logs"
	QueryTypeTraces  = "traces"
	QueryTypeRaw     = "raw"
)

// SLOResultType identifies whether the SLO query should return a list or a
// single SLO's detailed compliance/burn metrics.
const (
	SLOResultTypeList   = "list"
	SLOResultTypeSingle = "single"
)

// TracesResultType identifies whether the traces query fetches a single
// trace by ID or searches for matching traces.
const (
	TracesResultTypeSingle = "single"
	TracesResultTypeSearch = "search"
)

// HoneycombQuery is the deserialized form of a Grafana panel query for
// this datasource. It maps to the query editor's state in the frontend.
type HoneycombQuery struct {
	// QueryType selects metrics / slo / logs / traces / raw; defaults to "metrics".
	QueryType string `json:"queryType,omitempty"`

	// Dataset is the Honeycomb dataset slug (required).
	Dataset string `json:"dataset"`

	// SLO-specific fields. Used only when QueryType == "slo".
	SLOResultType string `json:"sloResultType,omitempty"` // "list" | "single"
	SLOID         string `json:"sloId,omitempty"`

	// Traces-specific fields. Used only when QueryType == "traces".
	TracesResultType string `json:"tracesResultType,omitempty"` // "single" | "search"
	TraceID          string `json:"traceId,omitempty"`

	// Logs-specific. Optional list of attribute columns to include in the
	// log line body. Empty means "all non-hidden columns".
	LogsAttributes []string `json:"logsAttributes,omitempty"`

	// QueryMode controls how results are mapped to Grafana frames.
	// Defaults to "timeseries".
	QueryMode string `json:"queryMode"` // "timeseries" | "table" | "stat" | "logs"

	// QueryResultType overrides which Honeycomb result fields are populated.
	// "" or "auto" picks based on QueryMode; explicit values are
	// "series", "result", "both".
	QueryResultType string `json:"queryResultType,omitempty"`

	// Calculations lists the aggregation operations to compute.
	Calculations []Calculation `json:"calculations"`

	// Filters restricts events included in the query.
	Filters []Filter `json:"filters"`

	// FilterCombination is "AND" or "OR" (default: "AND").
	FilterCombination string `json:"filterCombination"`

	// Breakdowns lists column names to group by.
	Breakdowns []string `json:"breakdowns"`

	// Orders controls result sorting.
	Orders []Order `json:"orders"`

	// Havings filter aggregated rows after calculations are applied.
	Havings []Having `json:"havings"`

	// Limit caps the number of result groups (default 100, max 10000).
	Limit int `json:"limit"`

	// Granularity is the time resolution in seconds (0 = auto-derive).
	Granularity int `json:"granularity"`

	// CompareTimeOffset adds a historical comparison in seconds.
	// Valid values: 1800, 3600, 7200, 28800, 86400, 604800, 2419200, 15724800.
	CompareTimeOffset int `json:"compareTimeOffset"`

	// RawMode bypasses the query editor and sends RawJSON directly.
	RawMode bool `json:"rawMode"`

	// RawJSON is used when RawMode is true. Must be a valid Honeycomb Query JSON.
	RawJSON string `json:"rawJson"`
}

// Calculation represents one aggregation operation in the query.
type Calculation struct {
	Op                string   `json:"op"`
	Column            string   `json:"column,omitempty"`
	Alias             string   `json:"alias,omitempty"`
	Filters           []Filter `json:"filters,omitempty"`
	FilterCombination string   `json:"filterCombination,omitempty"`
}

// Filter restricts events in the query.
type Filter struct {
	Column string      `json:"column"`
	Op     string      `json:"op"`
	Value  interface{} `json:"value,omitempty"`
}

// Order specifies a sort term.
type Order struct {
	Op     string `json:"op,omitempty"`
	Column string `json:"column,omitempty"`
	Order  string `json:"order"` // "ascending" | "descending"
}

// Having is a post-aggregation filter applied after calculations.
//
// CalculateOp identifies which calculation the having references (e.g. "P95",
// "COUNT"). Column is required for ops that take a column (matching the
// referenced Calculation). Op is the comparison: <, <=, =, !=, >=, >.
//
// See https://docs.honeycomb.io/api/queries/create-a-query.md for the full
// having spec.
type Having struct {
	CalculateOp string      `json:"calculateOp,omitempty"`
	Column      string      `json:"column,omitempty"`
	Op          string      `json:"op"`
	Value       interface{} `json:"value,omitempty"`
}

// Validate checks that the query has all required fields and returns an error
// with a descriptive message if any required field is missing or invalid.
func (q *HoneycombQuery) Validate() error {
	if strings.TrimSpace(q.Dataset) == "" {
		return fmt.Errorf("dataset is required")
	}

	// SLO queries have their own validation rules — no calculations needed.
	if q.QueryType == QueryTypeSLO {
		switch q.SLOResultType {
		case SLOResultTypeList, "":
			return nil
		case SLOResultTypeSingle:
			if strings.TrimSpace(q.SLOID) == "" {
				return fmt.Errorf("sloId is required when sloResultType is 'single'")
			}
			return nil
		default:
			return fmt.Errorf("unknown sloResultType %q (expected 'list' or 'single')", q.SLOResultType)
		}
	}

	// Logs queries don't need calculations — the backend supplies COUNT
	// and the user only chooses filters / attribute columns.
	if q.QueryType == QueryTypeLogs {
		return nil
	}

	// Traces queries: 'single' needs a trace ID; 'search' just needs the
	// dataset (filters optional).
	if q.QueryType == QueryTypeTraces {
		switch q.TracesResultType {
		case TracesResultTypeSearch:
			return nil
		case TracesResultTypeSingle, "":
			if strings.TrimSpace(q.TraceID) == "" {
				return fmt.Errorf("traceId is required when tracesResultType is 'single'")
			}
			return nil
		default:
			return fmt.Errorf("unknown tracesResultType %q (expected 'single' or 'search')", q.TracesResultType)
		}
	}

	if q.RawMode || q.QueryType == QueryTypeRaw {
		if strings.TrimSpace(q.RawJSON) == "" {
			return fmt.Errorf("rawJson is required when rawMode is true")
		}
		var raw honeycomb.Query
		if err := json.Unmarshal([]byte(q.RawJSON), &raw); err != nil {
			return fmt.Errorf("rawJson is not valid Honeycomb query JSON: %w", err)
		}
	} else if len(q.Calculations) == 0 {
		return fmt.Errorf("at least one calculation is required")
	}
	if q.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	if q.Limit > 10000 {
		return fmt.Errorf("limit cannot exceed 10000")
	}
	return nil
}

// IsEmpty returns true if the query has no meaningful content and should be
// skipped. This prevents sending empty queries to Honeycomb when a panel is
// freshly added to a dashboard.
func (q *HoneycombQuery) IsEmpty() bool {
	return strings.TrimSpace(q.Dataset) == "" &&
		len(q.Calculations) == 0 &&
		!q.RawMode
}

// ToHoneycombQuery converts a plugin query into the Honeycomb API format.
// Time range is NOT included here; it is applied separately via
// fingerprint.ApplyTimeRange so the cache key remains time-independent at L1.
func (q *HoneycombQuery) ToHoneycombQuery() (honeycomb.Query, error) {
	if q.RawMode {
		var raw honeycomb.Query
		if err := json.Unmarshal([]byte(q.RawJSON), &raw); err != nil {
			return honeycomb.Query{}, fmt.Errorf("parse raw query: %w", err)
		}
		return raw, nil
	}

	hq := honeycomb.Query{
		Breakdowns:        q.Breakdowns,
		FilterCombination: q.FilterCombination,
		Granularity:       q.Granularity,
		Limit:             q.Limit,
	}

	// Calculations (with optional per-calc filters)
	hq.Calculations = make([]honeycomb.Calculation, len(q.Calculations))
	for i, c := range q.Calculations {
		hc := honeycomb.Calculation{
			Op:                c.Op,
			Column:            c.Column,
			Alias:             c.Alias,
			FilterCombination: c.FilterCombination,
		}
		if len(c.Filters) > 0 {
			hc.Filters = make([]honeycomb.Filter, len(c.Filters))
			for j, f := range c.Filters {
				hc.Filters[j] = honeycomb.Filter{
					Column: f.Column,
					Op:     f.Op,
					Value:  f.Value,
				}
			}
		}
		hq.Calculations[i] = hc
	}

	// Filters
	hq.Filters = make([]honeycomb.Filter, len(q.Filters))
	for i, f := range q.Filters {
		hq.Filters[i] = honeycomb.Filter{
			Column: f.Column,
			Op:     f.Op,
			Value:  f.Value,
		}
	}

	// Orders
	hq.Orders = make([]honeycomb.Order, len(q.Orders))
	for i, o := range q.Orders {
		hq.Orders[i] = honeycomb.Order{
			Op:     o.Op,
			Column: o.Column,
			Order:  o.Order,
		}
	}

	// Havings
	hq.Havings = make([]honeycomb.Having, len(q.Havings))
	for i, h := range q.Havings {
		hq.Havings[i] = honeycomb.Having{
			CalculateOp: h.CalculateOp,
			Column:      h.Column,
			Op:          h.Op,
			Value:       h.Value,
		}
	}

	// Compare time offset (optional).
	if q.CompareTimeOffset > 0 {
		hq.CompareTimeOffsetSeconds = q.CompareTimeOffset
	}

	return hq, nil
}

// QueryMode maps the query's string mode to the transform.QueryMode enum.
func (q *HoneycombQuery) FrameMode() transform.QueryMode {
	switch q.QueryMode {
	case "table":
		return transform.ModeTable
	case "stat":
		return transform.ModeStat
	case "logs":
		return transform.ModeLogs
	default:
		return transform.ModeTimeseries
	}
}

// ShouldDisableSeries returns true when the query mode does not need timeseries
// data. Sending disable_series=true to Honeycomb reduces response payload and
// unlocks higher result limits.
//
// QueryResultType overrides this:
//   - "series" / "both" → false (always include series)
//   - "result"          → true (summary only)
//   - "" / "auto"       → driven by QueryMode
func (q *HoneycombQuery) ShouldDisableSeries() bool {
	switch q.QueryResultType {
	case "series", "both":
		return false
	case "result":
		return true
	}
	switch q.QueryMode {
	case "table", "stat", "logs":
		return true
	default:
		return false
	}
}

// DefaultQuery returns a minimal valid query to show as a starting point
// when a user adds a new Honeycomb panel.
func DefaultQuery() HoneycombQuery {
	return HoneycombQuery{
		QueryMode:         "timeseries",
		Calculations:      []Calculation{{Op: "COUNT"}},
		FilterCombination: "AND",
		Limit:             100,
	}
}

// parseQuery deserializes the JSON from a Grafana backend.DataQuery.JSON field.
func parseQuery(rawJSON []byte) (HoneycombQuery, error) {
	var q HoneycombQuery
	if len(rawJSON) == 0 {
		return DefaultQuery(), nil
	}
	if err := json.Unmarshal(rawJSON, &q); err != nil {
		return HoneycombQuery{}, fmt.Errorf("parse query JSON: %w", err)
	}
	// Apply defaults.
	if q.QueryMode == "" {
		q.QueryMode = "timeseries"
	}
	if q.FilterCombination == "" {
		q.FilterCombination = "AND"
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	return q, nil
}
