package honeycomb_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/honeycombio/grafana-honeycomb-datasource/pkg/honeycomb"
)

// FlexibleTime exists because Honeycomb returns event times as either an epoch
// number or one of two string layouts. Every branch is a silent wrong-timestamp
// bug if it breaks — the query succeeds and the points land in the wrong place
// on the graph — so each is pinned here.
func TestFlexibleTime_UnmarshalJSON(t *testing.T) {
	want := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{"unix epoch seconds", `1705320000`, want},
		{"rfc3339 with Z", `"2024-01-15T12:00:00Z"`, want},
		{"rfc3339 with offset", `"2024-01-15T14:00:00+02:00"`, want},
		{"no timezone falls back to UTC", `"2024-01-15T12:00:00"`, want},
		{"epoch zero", `0`, time.Unix(0, 0).UTC()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ft honeycomb.FlexibleTime
			if err := json.Unmarshal([]byte(tt.in), &ft); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.in, err)
			}
			if !ft.Time.Equal(tt.want) {
				t.Errorf("parsed %s as %s, want %s", tt.in, ft.Time.UTC(), tt.want)
			}
		})
	}
}

func TestFlexibleTime_UnmarshalJSON_Invalid(t *testing.T) {
	for _, in := range []string{
		`"not a time"`,
		`"2024-13-45T99:99:99Z"`,
		`{}`,
		`[]`,
	} {
		t.Run(in, func(t *testing.T) {
			var ft honeycomb.FlexibleTime
			if err := json.Unmarshal([]byte(in), &ft); err == nil {
				t.Errorf("expected an error for %s, got %s", in, ft.Time)
			}
		})
	}
}

// Honeycomb wraps each summary row in a {"data": {...}} envelope, but the
// in-memory shape is flat. Both forms must decode, since existing callers build
// entries directly.
func TestResultEntry_UnmarshalJSON(t *testing.T) {
	t.Run("unwraps the data envelope", func(t *testing.T) {
		var entry honeycomb.ResultEntry
		if err := json.Unmarshal([]byte(`{"data":{"COUNT":42,"service.name":"checkout"}}`), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := entry["COUNT"]; got != float64(42) {
			t.Errorf("COUNT = %#v, want 42", got)
		}
		if got := entry["service.name"]; got != "checkout" {
			t.Errorf("service.name = %#v, want \"checkout\"", got)
		}
		// The envelope key itself must not survive as a column, or it shows up
		// as a bogus field in the Grafana frame.
		if _, ok := entry["data"]; ok {
			t.Error(`"data" key leaked into the flattened entry`)
		}
	})

	t.Run("accepts an already-flat row", func(t *testing.T) {
		var entry honeycomb.ResultEntry
		if err := json.Unmarshal([]byte(`{"COUNT":7,"error":true}`), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := entry["COUNT"]; got != float64(7) {
			t.Errorf("COUNT = %#v, want 7", got)
		}
		if got := entry["error"]; got != true {
			t.Errorf("error = %#v, want true", got)
		}
	})

	t.Run("empty envelope yields an empty entry", func(t *testing.T) {
		var entry honeycomb.ResultEntry
		if err := json.Unmarshal([]byte(`{"data":{}}`), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(entry) != 0 {
			t.Errorf("expected an empty entry, got %#v", entry)
		}
	})

	t.Run("rejects malformed input", func(t *testing.T) {
		var entry honeycomb.ResultEntry
		if err := json.Unmarshal([]byte(`["not","an","object"]`), &entry); err == nil {
			t.Error("expected an error for a JSON array")
		}
		if err := json.Unmarshal([]byte(`{"data":"not an object"}`), &entry); err == nil {
			t.Error("expected an error when data is not an object")
		}
	})

	// A column literally named "data" is indistinguishable from the envelope, so
	// it is unwrapped. Documenting the limitation rather than pretending it away:
	// if this ever matters, the decoder needs a shape check instead of a key check.
	t.Run("a column named data is treated as the envelope", func(t *testing.T) {
		var entry honeycomb.ResultEntry
		if err := json.Unmarshal([]byte(`{"data":{"inner":1}}`), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := entry["inner"]; got != float64(1) {
			t.Errorf("expected the envelope to be unwrapped, got %#v", entry)
		}
	})
}
