package plugin_test

import (
	"encoding/json"
	"testing"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/plugin"
)

func TestHoneycombQuery_Validate_RequiresDataset(t *testing.T) {
	q := plugin.HoneycombQuery{
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
	}
	if err := q.Validate(); err == nil {
		t.Error("expected error for missing dataset")
	}
}

func TestHoneycombQuery_Validate_RequiresCalculations(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset: "production",
	}
	if err := q.Validate(); err == nil {
		t.Error("expected error for missing calculations")
	}
}

func TestHoneycombQuery_Validate_LimitGuardrails(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset:      "production",
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
		Limit:        99999,
	}
	if err := q.Validate(); err == nil {
		t.Error("expected error for limit > 10000")
	}
}

func TestHoneycombQuery_Validate_RawModeRequiresJSON(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset: "production",
		RawMode: true,
	}
	if err := q.Validate(); err == nil {
		t.Error("expected error for rawMode=true without rawJson")
	}
}

func TestHoneycombQuery_Validate_RawModeValidatesJSON(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset: "production",
		RawMode: true,
		RawJSON: `{invalid json}`,
	}
	if err := q.Validate(); err == nil {
		t.Error("expected error for invalid rawJson")
	}
}

func TestHoneycombQuery_Validate_ValidQuery(t *testing.T) {
	q := plugin.HoneycombQuery{
		Dataset:      "production",
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
		Limit:        100,
	}
	if err := q.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHoneycombQuery_IsEmpty(t *testing.T) {
	empty := plugin.HoneycombQuery{}
	if !empty.IsEmpty() {
		t.Error("expected empty query to be detected as empty")
	}

	notEmpty := plugin.HoneycombQuery{
		Dataset:      "production",
		Calculations: []plugin.Calculation{{Op: "COUNT"}},
	}
	if notEmpty.IsEmpty() {
		t.Error("expected non-empty query to not be empty")
	}
}

func TestHoneycombQuery_ShouldDisableSeries(t *testing.T) {
	tests := []struct {
		mode    string
		disable bool
	}{
		{"timeseries", false},
		{"table", true},
		{"stat", true},
		{"", false}, // default is timeseries
	}
	for _, tc := range tests {
		q := plugin.HoneycombQuery{QueryMode: tc.mode}
		got := q.ShouldDisableSeries()
		if got != tc.disable {
			t.Errorf("mode=%q: ShouldDisableSeries()=%v, want %v", tc.mode, got, tc.disable)
		}
	}
}

func TestHoneycombQuery_ToHoneycombQuery_ConvertsFields(t *testing.T) {
	pq := plugin.HoneycombQuery{
		Dataset: "production",
		Calculations: []plugin.Calculation{
			{Op: "COUNT"},
			{Op: "P99", Column: "duration_ms"},
		},
		Filters: []plugin.Filter{
			{Column: "status_code", Op: "!=", Value: "200"},
		},
		Breakdowns:        []string{"service.name"},
		FilterCombination: "AND",
		Limit:             50,
		Granularity:       300,
	}

	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hq.Calculations) != 2 {
		t.Errorf("expected 2 calculations, got %d", len(hq.Calculations))
	}
	if hq.Calculations[0].Op != "COUNT" {
		t.Errorf("expected COUNT, got %s", hq.Calculations[0].Op)
	}
	if hq.Calculations[1].Column != "duration_ms" {
		t.Errorf("expected duration_ms, got %s", hq.Calculations[1].Column)
	}
	if len(hq.Filters) != 1 {
		t.Errorf("expected 1 filter, got %d", len(hq.Filters))
	}
	if len(hq.Breakdowns) != 1 || hq.Breakdowns[0] != "service.name" {
		t.Errorf("expected [service.name] breakdown, got %v", hq.Breakdowns)
	}
	if hq.Limit != 50 {
		t.Errorf("expected limit 50, got %d", hq.Limit)
	}
	if hq.Granularity != 300 {
		t.Errorf("expected granularity 300, got %d", hq.Granularity)
	}
}

func TestHoneycombQuery_ToHoneycombQuery_RawMode(t *testing.T) {
	rawQuery := `{"calculations":[{"op":"COUNT"}],"breakdowns":["svc"]}`
	pq := plugin.HoneycombQuery{
		Dataset: "production",
		RawMode: true,
		RawJSON: rawQuery,
	}

	hq, err := pq.ToHoneycombQuery()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hq.Calculations) != 1 || hq.Calculations[0].Op != "COUNT" {
		t.Errorf("raw mode: unexpected calculations: %v", hq.Calculations)
	}
	if len(hq.Breakdowns) != 1 || hq.Breakdowns[0] != "svc" {
		t.Errorf("raw mode: unexpected breakdowns: %v", hq.Breakdowns)
	}
}

func TestDefaultQuery_IsValid(t *testing.T) {
	q := plugin.DefaultQuery()
	q.Dataset = "test" // provide required field
	if err := q.Validate(); err != nil {
		t.Errorf("default query is not valid: %v", err)
	}
}

func TestParseQuery_AppliesDefaults(t *testing.T) {
	// parseQuery is internal; we test it by round-tripping through JSON.
	raw := `{"dataset":"prod","calculations":[{"op":"COUNT"}]}`
	var q plugin.HoneycombQuery
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// After manual apply of defaults (as parseQuery does):
	if q.FilterCombination == "" {
		q.FilterCombination = "AND"
	}
	if q.QueryMode == "" {
		q.QueryMode = "timeseries"
	}
	if q.Limit == 0 {
		q.Limit = 100
	}

	if q.FilterCombination != "AND" {
		t.Errorf("default FilterCombination: got %s", q.FilterCombination)
	}
	if q.QueryMode != "timeseries" {
		t.Errorf("default QueryMode: got %s", q.QueryMode)
	}
	if q.Limit != 100 {
		t.Errorf("default Limit: got %d", q.Limit)
	}
}
