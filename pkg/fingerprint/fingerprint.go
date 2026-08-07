// Package fingerprint provides stable, normalized cache keys for Honeycomb
// query specifications and their execution contexts (time range, display mode).
//
// Design goals:
//   - Two semantically identical queries must produce the same fingerprint,
//     regardless of field ordering in the original JSON.
//   - The fingerprint must be stable across plugin restarts.
//   - The fingerprint must isolate different datasource instances.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// QuerySpec is the normalized, datasource-scoped representation of a query
// before time range is applied. It is used as the L1 cache key component.
type QuerySpec struct {
	DatasourceUID string
	Dataset       string
	Query         honeycomb.Query
}

// ExecutionSpec adds execution-time parameters to a QuerySpec. It is used
// as the L2/L3 cache key component.
type ExecutionSpec struct {
	QuerySpec
	// Snapped time range boundaries (see snapTime).
	From          int64
	To            int64
	DisableSeries bool
	Limit         int
}

// QueryShapeKey returns a stable cache key for the query shape (L1 cache).
// The key does NOT include time range, so the same query spec can share a
// query_id across different time windows.
func QueryShapeKey(spec QuerySpec) string {
	normalized := normalizeQuery(spec.Query)
	data := struct {
		UID     string          `json:"u"`
		Dataset string          `json:"d"`
		Query   honeycomb.Query `json:"q"`
	}{
		UID:     spec.DatasourceUID,
		Dataset: spec.Dataset,
		Query:   normalized,
	}
	return sha256Hex(mustMarshal(data))
}

// ExecutionKey returns a stable cache key for a specific query execution
// (L2 and L3 cache). It includes snapped time range and display options.
func ExecutionKey(spec ExecutionSpec) string {
	// Snap time range to reduce cache misses on small time skew.
	duration := spec.To - spec.From
	snapSecs := snapSeconds(duration)
	from := snapTime(spec.From, snapSecs)
	to := snapTime(spec.To, snapSecs)

	normalized := normalizeQuery(spec.Query)
	data := struct {
		UID     string          `json:"u"`
		Dataset string          `json:"d"`
		Query   honeycomb.Query `json:"q"`
		From    int64           `json:"f"`
		To      int64           `json:"t"`
		DS      bool            `json:"ds,omitempty"`
		Limit   int             `json:"l,omitempty"`
	}{
		UID:     spec.DatasourceUID,
		Dataset: spec.Dataset,
		Query:   normalized,
		From:    from,
		To:      to,
		DS:      spec.DisableSeries,
		Limit:   spec.Limit,
	}
	return sha256Hex(mustMarshal(data))
}

// CompletedResultKey returns the L3 cache key for a completed query result.
// It is keyed on the execKey (the execution fingerprint), NOT on Honeycomb's
// query_result_id. This allows two logically identical executions — which may
// receive different Honeycomb result IDs — to share the same cached result.
func CompletedResultKey(execKey string) string {
	return "result:" + execKey
}

// ApplyTimeRange sets the time range fields on a Query for the given
// Grafana from/to window. It uses start_time + end_time for absolute
// ranges (the common Grafana case) and derives a granularity if the
// caller passes granularity=0 (auto). When maxDataPoints > 0 the auto
// granularity is sized to roughly match Grafana's panel width so the
// chart isn't undersampled vs. the same query in Honeycomb's UI.
func ApplyTimeRange(q *honeycomb.Query, from, to time.Time, granularity int, maxDataPoints int64) {
	q.StartTime = from.Unix()
	q.EndTime = to.Unix()
	q.TimeRange = 0 // use absolute time, not relative

	if granularity == 0 {
		duration := to.Sub(from)
		q.Granularity = deriveGranularity(duration, maxDataPoints)
	} else {
		q.Granularity = granularity
	}
}

// ---------------------------------------------------------------------------
// Normalization helpers
// ---------------------------------------------------------------------------

// normalizeQuery returns a copy of q with all slice fields sorted into a
// canonical order, ensuring identical queries with different field ordering
// produce the same fingerprint.
func normalizeQuery(q honeycomb.Query) honeycomb.Query {
	n := q

	// Sort breakdowns alphabetically.
	if len(n.Breakdowns) > 0 {
		sorted := make([]string, len(n.Breakdowns))
		copy(sorted, n.Breakdowns)
		sort.Strings(sorted)
		n.Breakdowns = sorted
	}

	// Sort calculations by (op, column) so order doesn't matter.
	if len(n.Calculations) > 0 {
		calcs := make([]honeycomb.Calculation, len(n.Calculations))
		copy(calcs, n.Calculations)
		sort.Slice(calcs, func(i, j int) bool {
			if calcs[i].Op != calcs[j].Op {
				return calcs[i].Op < calcs[j].Op
			}
			return calcs[i].Column < calcs[j].Column
		})
		n.Calculations = calcs
	}

	// Sort filters by (column, op) for stability.
	if len(n.Filters) > 0 {
		filters := make([]honeycomb.Filter, len(n.Filters))
		copy(filters, n.Filters)
		sort.Slice(filters, func(i, j int) bool {
			if filters[i].Column != filters[j].Column {
				return filters[i].Column < filters[j].Column
			}
			return filters[i].Op < filters[j].Op
		})
		n.Filters = filters
	}

	// Sort orders by (op, column).
	if len(n.Orders) > 0 {
		orders := make([]honeycomb.Order, len(n.Orders))
		copy(orders, n.Orders)
		sort.Slice(orders, func(i, j int) bool {
			if orders[i].Op != orders[j].Op {
				return orders[i].Op < orders[j].Op
			}
			return orders[i].Column < orders[j].Column
		})
		n.Orders = orders
	}

	return n
}

// snapSeconds returns the snap interval (in seconds) for a given query
// duration, matching Honeycomb's own truncation rules.
func snapSeconds(durationSecs int64) int64 {
	switch {
	case durationSecs <= 6*3600: // ≤6h → nearest 60s
		return 60
	case durationSecs <= 48*3600: // ≤48h → nearest 300s
		return 300
	default: // ≤7d → nearest 1800s
		return 1800
	}
}

// snapTime snaps a Unix timestamp down to the nearest snapSecs boundary.
func snapTime(unix, snapSecs int64) int64 {
	if snapSecs <= 0 {
		return unix
	}
	return (unix / snapSecs) * snapSecs
}

// deriveGranularity returns a reasonable granularity (in seconds) for a
// given query duration when the user has not specified one explicitly.
// The returned value is always within Honeycomb's valid range of [T/1000, T/1]
// and snaps up to a "nice" interval to improve cache hit rate across
// adjacent viewport widths.
func deriveGranularity(duration time.Duration, maxDataPoints int64) int {
	secs := int(duration.Seconds())
	if secs <= 0 {
		return 60
	}

	// Default target if Grafana didn't supply MaxDataPoints. 1000 matches
	// Honeycomb's own minimum-bucket-size limit (T/1000) and produces charts
	// at the same fidelity as queries run in Honeycomb's UI.
	target := int64(1000)
	if maxDataPoints > 0 {
		target = maxDataPoints
	}

	g := secs / int(target)
	if g < 1 {
		g = 1
	}

	// Honeycomb requires granularity in [T/1000, T]. Cap to T/1000 (rounded up)
	// so we don't produce values Honeycomb will reject.
	minG := (secs + 999) / 1000
	if minG < 1 {
		minG = 1
	}
	if g < minG {
		g = minG
	}
	if g > secs {
		g = secs
	}

	return roundUpNiceGranularity(g)
}

// roundUpNiceGranularity rounds a granularity (seconds) up to the nearest
// "nice" value. Snapping to a fixed ladder means small viewport-width
// differences don't fragment the cache.
func roundUpNiceGranularity(g int) int {
	nice := []int{1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 28800, 86400}
	for _, n := range nice {
		if g <= n {
			return n
		}
	}
	return g
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// json.Marshal only fails for unsupported types (channels, funcs).
		// Our structs are all serializable; this should never happen.
		panic(fmt.Sprintf("fingerprint: json.Marshal failed: %v", err))
	}
	return b
}
