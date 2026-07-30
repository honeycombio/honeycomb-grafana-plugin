package transform

import (
	"strings"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// unitForCalc returns a Grafana field-config Unit string for a calculation
// column when it can be inferred from the calc op and underlying column name.
// Returns "" when no unit can be confidently inferred — the field is then
// left unitless and Grafana renders raw numbers.
//
// Heuristics (most specific first):
//   - RATE_SUM / RATE_AVG / RATE_MAX → "reqps" (events per second)
//   - column ends with _ms / _milliseconds → "ms"
//   - column ends with _us / _microseconds  → "µs"
//   - column ends with _ns / _nanoseconds   → "ns"
//   - column ends with _seconds / _s        → "s"
//   - column ends with _bytes               → "bytes"
//   - column contains "percent"             → "percent"
//
// Honeycomb does not expose a unit on the column metadata, so this stays
// heuristic. False positives here are visual-only; users can override the
// unit in panel field-config if they disagree.
func unitForCalc(c honeycomb.Calculation) string {
	switch c.Op {
	case "RATE_SUM", "RATE_AVG", "RATE_MAX":
		return "reqps"
	}
	col := strings.ToLower(c.Column)
	switch {
	case col == "":
		return ""
	case hasSuffix(col, "_ms", "_milliseconds"):
		return "ms"
	case hasSuffix(col, "_us", "_microseconds"):
		return "µs"
	case hasSuffix(col, "_ns", "_nanoseconds"):
		return "ns"
	case hasSuffix(col, "_seconds", "_s"):
		return "s"
	case hasSuffix(col, "_bytes"):
		return "bytes"
	case strings.Contains(col, "percent"):
		return "percent"
	}
	return ""
}

// unitForCalcByColumnName returns the unit for a calculation by the column
// name as it appears in result rows (e.g. "P95(duration_ms)" or "duration_ms"
// or "COUNT"). Used by transformers that only have the result column name,
// not the underlying Calculation struct.
func unitForCalcByColumnName(name string, calcs []honeycomb.Calculation) string {
	for _, c := range calcs {
		// Match by alias, by canonical "OP(column)" form, or by op alone.
		if c.Alias != "" && c.Alias == name {
			return unitForCalc(c)
		}
		canonical := c.Op
		if c.Column != "" {
			canonical = c.Op + "(" + c.Column + ")"
		}
		if canonical == name {
			return unitForCalc(c)
		}
	}
	return ""
}

func hasSuffix(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}
