package transform_test

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

func TestSLOListToFrame_BasicShape(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	slos := []honeycomb.SLO{
		{
			ID: "abc", Name: "API Latency",
			Description:      "p95 < 500ms",
			SLI:              honeycomb.SLI{Alias: "good_p95"},
			TimePeriodDays:   30, TargetPerMillion: 990000,
			DatasetSlugs: []string{"prod-api"},
			CreatedAt:    now,
		},
		{
			ID: "def", Name: "Checkout Errors",
			SLI:              honeycomb.SLI{Alias: "checkout_ok"},
			TimePeriodDays:   7, TargetPerMillion: 999000,
			DatasetSlugs: []string{"prod-checkout", "prod-api"},
		},
	}

	frame := transform.SLOListToFrame(slos)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}
	if frame.Meta == nil || frame.Meta.Type != data.FrameTypeTable {
		t.Errorf("expected table frame; got meta=%+v", frame.Meta)
	}
	if frame.Rows() != 2 {
		t.Errorf("expected 2 rows, got %d", frame.Rows())
	}
	idF, _ := frame.FieldByName("id")
	if idF == nil || idF.At(0).(string) != "abc" {
		t.Errorf("id field mismatch: %v", idF)
	}
	dsF, _ := frame.FieldByName("dataset_slugs")
	if dsF.At(1).(string) != "prod-checkout,prod-api" {
		t.Errorf("expected joined dataset slugs; got %v", dsF.At(1))
	}
}

func TestSLODetailToFrame_HasPercentUnits(t *testing.T) {
	compliance := 99.85
	budget := 75.0
	burn := 1.2
	slo := &honeycomb.SLO{
		ID: "abc", Name: "API Latency", Status: "normal",
		TargetPerMillion: 990000, // 99.0%
		Compliance:       &compliance,
		BudgetRemaining:  &budget,
		BurnRate:         &burn,
	}

	frame := transform.SLODetailToFrame(slo)
	if frame.Rows() != 1 {
		t.Fatalf("expected 1 row, got %d", frame.Rows())
	}

	for _, name := range []string{"compliance", "budget_remaining", "target"} {
		f, idx := frame.FieldByName(name)
		if idx == -1 {
			t.Errorf("missing field %q", name)
			continue
		}
		if f.Config == nil || f.Config.Unit != "percent" {
			t.Errorf("field %q: expected unit=percent; got %+v", name, f.Config)
		}
	}

	// target_per_million=990000 → 99.0%
	tF, _ := frame.FieldByName("target")
	v, ok := tF.At(0).(*float64)
	if !ok || v == nil {
		t.Fatalf("target field should be *float64; got %T", tF.At(0))
	}
	if *v != 99.0 {
		t.Errorf("expected target 99.0%%, got %v", *v)
	}
}

func TestSLODetailToFrame_NilSLO_ReturnsEmptyFrame(t *testing.T) {
	frame := transform.SLODetailToFrame(nil)
	if frame == nil {
		t.Fatal("expected non-nil frame even for nil SLO")
	}
	if frame.Rows() != 0 {
		t.Errorf("expected 0 rows for nil SLO; got %d", frame.Rows())
	}
}

func TestSLODetailToFrame_NullableMetricsPreserved(t *testing.T) {
	// When a non-detailed call is upgraded to detailed, the metrics should
	// stay nil rather than rendering as 0.
	slo := &honeycomb.SLO{
		ID: "abc", Name: "API Latency", TargetPerMillion: 990000,
	}
	frame := transform.SLODetailToFrame(slo)
	for _, name := range []string{"compliance", "budget_remaining", "burn_rate"} {
		f, idx := frame.FieldByName(name)
		if idx == -1 {
			t.Errorf("missing field %q", name)
			continue
		}
		v := f.At(0)
		// Nullable fields hold *float64 (or nil)
		if pv, ok := v.(*float64); ok && pv != nil {
			t.Errorf("field %q: expected nil pointer; got %v", name, *pv)
		}
	}
}
