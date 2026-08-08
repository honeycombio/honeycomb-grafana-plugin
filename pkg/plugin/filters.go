package plugin

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/honeycombio/grafana-honeycomb-datasource/pkg/honeycomb"
)

// collectionFilterOps are Honeycomb filter operators whose value must be a
// JSON array on the wire. The visual editor supplies these as CSV strings
// (e.g. "api,worker"); normalizeFilterValue turns them into arrays.
//
// Raw query mode does not use this path — callers paste Honeycomb Query JSON
// directly and must use a real JSON array for in/not-in values.
var collectionFilterOps = map[string]struct{}{
	"in":     {},
	"not-in": {},
}

// toHoneycombFilters converts plugin filters to Honeycomb API filters,
// normalizing in/not-in CSV strings into arrays. Returns an error for empty
// or malformed values so the invalid payload never reaches Honeycomb.
func toHoneycombFilters(in []Filter) ([]honeycomb.Filter, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]honeycomb.Filter, len(in))
	for i, f := range in {
		value, err := normalizeFilterValue(f.Op, f.Value)
		if err != nil {
			return nil, fmt.Errorf("filter[%d] column=%q op=%q: %w", i, f.Column, f.Op, err)
		}
		out[i] = honeycomb.Filter{
			Column: f.Column,
			Op:     f.Op,
			Value:  value,
		}
	}
	return out, nil
}

// validateFilters checks that every filter's operator/value pair is legal.
func validateFilters(filters []Filter) error {
	for i, f := range filters {
		if _, err := normalizeFilterValue(f.Op, f.Value); err != nil {
			return fmt.Errorf("filter[%d] column=%q op=%q: %w", i, f.Column, f.Op, err)
		}
	}
	return nil
}

// normalizeFilterValue prepares a filter value for the Honeycomb API.
//
// For in/not-in the visual-editor contract is a non-empty CSV string
// (e.g. "api,worker" or `api,"web, api"`). That string is parsed with
// encoding/csv and sent to Honeycomb as a JSON array of strings.
//
// For all other operators the value is returned unchanged.
func normalizeFilterValue(op string, value interface{}) (interface{}, error) {
	if _, ok := collectionFilterOps[op]; !ok {
		return value, nil
	}
	return normalizeCollectionCSV(value)
}

func normalizeCollectionCSV(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, fmt.Errorf("in/not-in requires a non-empty CSV list (e.g. a,b,c)")
	}

	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("in/not-in value must be a CSV string (e.g. a,b,c), got %T", value)
	}

	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, fmt.Errorf("in/not-in requires a non-empty CSV list (e.g. a,b,c)")
	}

	// Visual editor uses CSV only. Reject only when the value is an actual
	// JSON array (belongs in Raw query mode) — a CSV token like [legacy]
	// must still parse as CSV.
	if looksLikeJSONArray(trimmed) {
		return nil, fmt.Errorf("in/not-in expects CSV (e.g. a,b,c); use Raw query mode for a JSON array")
	}

	r := csv.NewReader(strings.NewReader(trimmed))
	r.TrimLeadingSpace = true
	fields, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("in/not-in value is not valid CSV: %w", err)
	}

	out := make([]interface{}, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("in/not-in requires a non-empty CSV list (e.g. a,b,c)")
	}
	return out, nil
}

// looksLikeJSONArray reports whether s is a JSON array literal. Used to
// steer visual-editor users toward CSV without rejecting CSV values that
// merely start with '[' (e.g. [legacy]).
func looksLikeJSONArray(s string) bool {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return false
	}
	var arr []interface{}
	return json.Unmarshal([]byte(s), &arr) == nil
}
