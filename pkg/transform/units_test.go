package transform

import (
	"testing"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

func TestUnitForCalc(t *testing.T) {
	tests := []struct {
		name string
		c    honeycomb.Calculation
		want string
	}{
		{"RATE_SUM is reqps", honeycomb.Calculation{Op: "RATE_SUM", Column: "request_count"}, "reqps"},
		{"RATE_AVG is reqps", honeycomb.Calculation{Op: "RATE_AVG"}, "reqps"},
		{"RATE_MAX is reqps", honeycomb.Calculation{Op: "RATE_MAX"}, "reqps"},
		{"P95 over duration_ms is ms", honeycomb.Calculation{Op: "P95", Column: "duration_ms"}, "ms"},
		{"AVG over response_milliseconds is ms", honeycomb.Calculation{Op: "AVG", Column: "response_milliseconds"}, "ms"},
		{"P50 over latency_us is microseconds", honeycomb.Calculation{Op: "P50", Column: "latency_us"}, "µs"},
		{"P50 over latency_seconds is s", honeycomb.Calculation{Op: "P50", Column: "latency_seconds"}, "s"},
		{"SUM over response_bytes is bytes", honeycomb.Calculation{Op: "SUM", Column: "response_bytes"}, "bytes"},
		{"COUNT no column → no unit", honeycomb.Calculation{Op: "COUNT"}, ""},
		{"AVG over plain column → no unit", honeycomb.Calculation{Op: "AVG", Column: "score"}, ""},
		{"AVG cpu_percent → percent", honeycomb.Calculation{Op: "AVG", Column: "cpu_percent"}, "percent"},
		{"case insensitive _MS suffix", honeycomb.Calculation{Op: "P99", Column: "Duration_MS"}, "ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unitForCalc(tt.c)
			if got != tt.want {
				t.Errorf("unitForCalc(%+v) = %q, want %q", tt.c, got, tt.want)
			}
		})
	}
}

func TestUnitForCalcByColumnName(t *testing.T) {
	calcs := []honeycomb.Calculation{
		{Op: "P95", Column: "duration_ms"},
		{Op: "RATE_SUM", Alias: "rps"},
		{Op: "COUNT"},
	}

	tests := []struct {
		name   string
		column string
		want   string
	}{
		{"alias match", "rps", "reqps"},
		{"canonical OP(col) match", "P95(duration_ms)", "ms"},
		{"plain op match", "COUNT", ""},
		{"no match", "unknown_field", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unitForCalcByColumnName(tt.column, calcs)
			if got != tt.want {
				t.Errorf("unitForCalcByColumnName(%q) = %q, want %q", tt.column, got, tt.want)
			}
		})
	}
}
