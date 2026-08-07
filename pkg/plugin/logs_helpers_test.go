package plugin

import (
	"testing"
)

// Internal test (package plugin, matching clamp_test.go) because these helpers
// are unexported. They are small, but every logs and traces query passes through
// them, and each one encodes a default that is invisible from the query JSON.

func TestToHoneycombFilters(t *testing.T) {
	t.Run("nil for empty input", func(t *testing.T) {
		// nil rather than an empty slice matters: the field is omitempty, so an
		// empty slice would serialise as "filters":[] and Honeycomb treats a
		// present-but-empty filter list differently from an absent one.
		if got := toHoneycombFilters(nil); got != nil {
			t.Errorf("toHoneycombFilters(nil) = %v, want nil", got)
		}
		if got := toHoneycombFilters([]Filter{}); got != nil {
			t.Errorf("toHoneycombFilters([]) = %v, want nil", got)
		}
	})

	t.Run("preserves order and value types", func(t *testing.T) {
		in := []Filter{
			{Column: "duration_ms", Op: ">", Value: 100},
			{Column: "service.name", Op: "=", Value: "checkout"},
			{Column: "error", Op: "exists"},
		}

		got := toHoneycombFilters(in)

		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		for i := range in {
			if got[i].Column != in[i].Column || got[i].Op != in[i].Op {
				t.Errorf("filter %d = {%q %q}, want {%q %q}",
					i, got[i].Column, got[i].Op, in[i].Column, in[i].Op)
			}
		}
		// Values pass through untouched — numbers must not become strings.
		if got[0].Value != 100 {
			t.Errorf("numeric value = %#v, want 100", got[0].Value)
		}
		if got[1].Value != "checkout" {
			t.Errorf("string value = %#v, want \"checkout\"", got[1].Value)
		}
		if got[2].Value != nil {
			t.Errorf("omitted value = %#v, want nil", got[2].Value)
		}
	})
}

func TestDefaultedFilterCombination(t *testing.T) {
	tests := map[string]string{
		"":    "AND", // Honeycomb requires one; AND is the safe default
		"AND": "AND",
		"OR":  "OR",
	}
	for in, want := range tests {
		if got := defaultedFilterCombination(in); got != want {
			t.Errorf("defaultedFilterCombination(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultedLimit(t *testing.T) {
	const def = 100
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back", 0, def},
		{"negative falls back", -5, def},
		{"positive is kept", 250, 250},
		{"one is kept", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultedLimit(tt.in, def); got != tt.want {
				t.Errorf("defaultedLimit(%d, %d) = %d, want %d", tt.in, def, got, tt.want)
			}
		})
	}
}

// The list must not contain "timestamp": asking Honeycomb for it returns
// 422 "unknown column or derived column timestamp", which fails the whole logs
// query. Event time arrives via the Series response instead.
func TestDefaultLogAttributes(t *testing.T) {
	attrs := defaultLogAttributes()

	if len(attrs) == 0 {
		t.Fatal("expected a non-empty default attribute list")
	}

	seen := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		if a == "timestamp" {
			t.Error("\"timestamp\" must not be requested — Honeycomb rejects it with a 422")
		}
		if seen[a] {
			t.Errorf("duplicate attribute %q", a)
		}
		seen[a] = true
	}

	// The attributes that make a log line useful to read.
	for _, required := range []string{"name", "service.name", "trace.trace_id"} {
		if !seen[required] {
			t.Errorf("expected %q in the default attributes, got %v", required, attrs)
		}
	}

	// Returned fresh each call, so a caller appending to it cannot poison the
	// defaults for every later query.
	attrs[0] = "mutated"
	if defaultLogAttributes()[0] == "mutated" {
		t.Error("defaultLogAttributes returns shared state; callers can corrupt the defaults")
	}
}
