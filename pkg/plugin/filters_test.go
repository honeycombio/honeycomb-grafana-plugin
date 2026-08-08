package plugin_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/honeycombio/grafana-honeycomb-datasource/pkg/plugin"
)

func TestToHoneycombQuery_CollectionFilters(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		op      string
		want    []interface{}
		wantErr string
	}{
		{
			name:  "CSV string",
			op:    "in",
			value: "api,worker",
			want:  []interface{}{"api", "worker"},
		},
		{
			name:  "CSV with quoted commas and surrounding whitespace",
			op:    "not-in",
			value: ` api, "web, api", worker `,
			want:  []interface{}{"api", "web, api", "worker"},
		},
		{
			name:  "single value CSV",
			op:    "in",
			value: "api",
			want:  []interface{}{"api"},
		},
		{
			name:  "CSV token starting with bracket",
			op:    "in",
			value: "[legacy]",
			want:  []interface{}{"[legacy]"},
		},
		{
			name:    "empty string",
			op:      "in",
			value:   "  ",
			wantErr: "non-empty CSV list",
		},
		{
			name:    "JSON array string rejected",
			op:      "in",
			value:   `["api","worker"]`,
			wantErr: "expects CSV",
		},
		{
			name:  "invalid JSON-looking brackets still parse as CSV",
			op:    "in",
			value: `[api,worker]`,
			want:  []interface{}{"[api", "worker]"},
		},
		{
			name:    "native array rejected",
			op:      "not-in",
			value:   []interface{}{"api", "worker"},
			wantErr: "must be a CSV string",
		},
		{
			name:    "nil value",
			op:      "in",
			value:   nil,
			wantErr: "non-empty CSV list",
		},
		{
			name:    "unsupported scalar type",
			op:      "in",
			value:   42,
			wantErr: "must be a CSV string",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pq := plugin.HoneycombQuery{
				Dataset:      "production",
				Calculations: []plugin.Calculation{{Op: "COUNT"}},
				Filters: []plugin.Filter{
					{Column: "service.name", Op: tc.op, Value: tc.value},
				},
			}
			hq, err := pq.ToHoneycombQuery()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(hq.Filters) != 1 {
				t.Fatalf("expected 1 filter, got %d", len(hq.Filters))
			}
			got, ok := hq.Filters[0].Value.([]interface{})
			if !ok {
				t.Fatalf("expected []interface{}, got %T (%v)", hq.Filters[0].Value, hq.Filters[0].Value)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("value = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestToHoneycombQuery_ScalarFilterUnchanged(t *testing.T) {
	pq := plugin.HoneycombQuery{
		Dataset:      "production",
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
		Filters: []plugin.Filter{
			{Column: "status_code", Op: "!=", Value: "200"},
		},
	}
	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hq.Filters[0].Value != "200" {
		t.Fatalf("scalar value mutated: got %#v", hq.Filters[0].Value)
	}
}

func TestToHoneycombQuery_CalculationCollectionFilters(t *testing.T) {
	pq := plugin.HoneycombQuery{
		Dataset: "production",
		Calculations: []plugin.Calculation{
			{
				Op: "COUNT",
				Filters: []plugin.Filter{
					{Column: "service.name", Op: "in", Value: "api,worker"},
				},
			},
		},
	}
	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := hq.Calculations[0].Filters[0].Value.([]interface{})
	want := []interface{}{"api", "worker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calc filter value = %#v, want %#v", got, want)
	}
}

func TestValidate_RejectsMalformedCollectionFilter(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset:      "production",
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
		Filters: []plugin.Filter{
			{Column: "service.name", Op: "in", Value: ""},
		},
	}
	err := q.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "non-empty CSV list") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LogsAndTracesCollectionFilters(t *testing.T) {
	logs := plugin.HoneycombQuery{
		QueryType: plugin.QueryTypeLogs,
		Dataset:   "production",
		Filters: []plugin.Filter{
			{Column: "service.name", Op: "in", Value: `["api"]`},
		},
	}
	if err := logs.Validate(); err == nil {
		t.Fatal("logs: expected validation error for JSON array string")
	}

	tracesSearch := plugin.HoneycombQuery{
		QueryType:        plugin.QueryTypeTraces,
		TracesResultType: plugin.TracesResultTypeSearch,
		Dataset:          "production",
		Filters: []plugin.Filter{
			{Column: "service.name", Op: "not-in", Value: ""},
		},
	}
	if err := tracesSearch.Validate(); err == nil {
		t.Fatal("traces search: expected validation error for empty CSV")
	}

	// Trace-by-ID ignores panel filters; leftover Search filter rows must not block it.
	tracesSingle := plugin.HoneycombQuery{
		QueryType:        plugin.QueryTypeTraces,
		TracesResultType: plugin.TracesResultTypeSingle,
		Dataset:          "production",
		TraceID:          "abc123",
		Filters: []plugin.Filter{
			{Column: "service.name", Op: "in", Value: ""},
		},
	}
	if err := tracesSingle.Validate(); err != nil {
		t.Fatalf("traces single: unexpected validation error: %v", err)
	}
}

func TestValidate_RawModeIgnoresEditorFilters(t *testing.T) {
	// Raw mode only sends RawJSON; leftover visual-editor filters must not block it.
	q := plugin.HoneycombQuery{
		Dataset: "production",
		RawMode: true,
		RawJSON: `{"calculations":[{"op":"COUNT"}],"filters":[{"column":"service.name","op":"in","value":["api","worker"]}]}`,
		Filters: []plugin.Filter{
			{Column: "service.name", Op: "in", Value: ""},
		},
	}
	if err := q.Validate(); err != nil {
		t.Fatalf("raw mode: unexpected validation error: %v", err)
	}
}

func TestValidate_RawModeStillChecksLimit(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset: "production",
		RawMode: true,
		RawJSON: `{"calculations":[{"op":"COUNT"}]}`,
		Limit:   99999,
	}
	err := q.Validate()
	if err == nil {
		t.Fatal("raw mode: expected limit validation error")
	}
	if !strings.Contains(err.Error(), "limit cannot exceed 10000") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToHoneycombQuery_CollectionFilterJSONRoundTrip(t *testing.T) {
	// Simulate Grafana sending a CSV string from the visual editor.
	raw := `{
		"dataset":"production",
		"calculations":[{"op":"COUNT"}],
		"filters":[{"column":"service.name","op":"in","value":"api,worker"}]
	}`
	var pq plugin.HoneycombQuery
	if err := json.Unmarshal([]byte(raw), &pq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		t.Fatalf("ToHoneycombQuery: %v", err)
	}
	payload, err := json.Marshal(hq.Filters[0])
	if err != nil {
		t.Fatalf("marshal filter: %v", err)
	}
	var decoded struct {
		Column string        `json:"column"`
		Op     string        `json:"op"`
		Value  []interface{} `json:"value"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal filter payload: %v (raw %s)", err, payload)
	}
	if !reflect.DeepEqual(decoded.Value, []interface{}{"api", "worker"}) {
		t.Fatalf("Honeycomb JSON value = %#v, want array", decoded.Value)
	}
}
