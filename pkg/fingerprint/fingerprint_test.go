package fingerprint_test

import (
	"testing"
	"time"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/fingerprint"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

func TestQueryShapeKey_Stability(t *testing.T) {
	spec := fingerprint.QuerySpec{
		DatasourceUID: "ds-1",
		Dataset:       "production",
		Query: honeycomb.Query{
			Calculations: []honeycomb.Calculation{
				{Op: "COUNT"},
				{Op: "AVG", Column: "duration_ms"},
			},
			Breakdowns: []string{"service.name", "http.method"},
			Filters: []honeycomb.Filter{
				{Column: "status_code", Op: "!=", Value: 200},
			},
		},
	}

	key1 := fingerprint.QueryShapeKey(spec)
	key2 := fingerprint.QueryShapeKey(spec)
	if key1 != key2 {
		t.Errorf("QueryShapeKey is not stable: got %s and %s", key1, key2)
	}
}

func TestQueryShapeKey_NormalizesOrder(t *testing.T) {
	base := fingerprint.QuerySpec{
		DatasourceUID: "ds-1",
		Dataset:       "production",
		Query: honeycomb.Query{
			Calculations: []honeycomb.Calculation{
				{Op: "COUNT"},
				{Op: "AVG", Column: "duration_ms"},
			},
			Breakdowns: []string{"service.name", "http.method"},
		},
	}

	// Reorder fields in the same query.
	reordered := fingerprint.QuerySpec{
		DatasourceUID: "ds-1",
		Dataset:       "production",
		Query: honeycomb.Query{
			Calculations: []honeycomb.Calculation{
				{Op: "AVG", Column: "duration_ms"},
				{Op: "COUNT"},
			},
			Breakdowns: []string{"http.method", "service.name"}, // reversed
		},
	}

	key1 := fingerprint.QueryShapeKey(base)
	key2 := fingerprint.QueryShapeKey(reordered)
	if key1 != key2 {
		t.Errorf("QueryShapeKey does not normalize field ordering: %s vs %s", key1, key2)
	}
}

func TestQueryShapeKey_DifferentDatasourcesAreIsolated(t *testing.T) {
	q := honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}}

	key1 := fingerprint.QueryShapeKey(fingerprint.QuerySpec{DatasourceUID: "ds-1", Dataset: "production", Query: q})
	key2 := fingerprint.QueryShapeKey(fingerprint.QuerySpec{DatasourceUID: "ds-2", Dataset: "production", Query: q})

	if key1 == key2 {
		t.Error("QueryShapeKey should differ for different datasource UIDs")
	}
}

func TestQueryShapeKey_DifferentDatasetsAreIsolated(t *testing.T) {
	q := honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}}

	key1 := fingerprint.QueryShapeKey(fingerprint.QuerySpec{DatasourceUID: "ds-1", Dataset: "prod", Query: q})
	key2 := fingerprint.QueryShapeKey(fingerprint.QuerySpec{DatasourceUID: "ds-1", Dataset: "staging", Query: q})

	if key1 == key2 {
		t.Error("QueryShapeKey should differ for different datasets")
	}
}

func TestExecutionKey_TimeSnapping(t *testing.T) {
	q := honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}}
	base := fingerprint.ExecutionSpec{
		QuerySpec: fingerprint.QuerySpec{DatasourceUID: "ds-1", Dataset: "prod", Query: q},
		From:      1700000000,
		To:        1700000000 + 3600, // 1 hour range → snaps to 60s
	}

	// A slightly different "from" within the same 60-second snap bucket should
	// produce the same key.
	tweaked := base
	tweaked.From = base.From + 30 // 30 seconds later, still same bucket

	k1 := fingerprint.ExecutionKey(base)
	k2 := fingerprint.ExecutionKey(tweaked)
	if k1 != k2 {
		t.Errorf("ExecutionKey should snap short-range timestamps to 60s: got different keys")
	}
}

func TestExecutionKey_DifferentDisableSeriesProducesDifferentKey(t *testing.T) {
	q := honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}}
	base := fingerprint.ExecutionSpec{
		QuerySpec: fingerprint.QuerySpec{DatasourceUID: "ds-1", Dataset: "prod", Query: q},
		From:      1700000000,
		To:        1700003600,
	}

	withSeries := base
	withSeries.DisableSeries = false
	withoutSeries := base
	withoutSeries.DisableSeries = true

	if fingerprint.ExecutionKey(withSeries) == fingerprint.ExecutionKey(withoutSeries) {
		t.Error("ExecutionKey should differ when DisableSeries changes")
	}
}

func TestApplyTimeRange_AutoGranularity(t *testing.T) {
	q := honeycomb.Query{}
	from := time.Unix(1700000000, 0)
	to := from.Add(2 * time.Hour)

	fingerprint.ApplyTimeRange(&q, from, to, 0, 0)

	if q.StartTime != from.Unix() {
		t.Errorf("expected StartTime %d, got %d", from.Unix(), q.StartTime)
	}
	if q.EndTime != to.Unix() {
		t.Errorf("expected EndTime %d, got %d", to.Unix(), q.EndTime)
	}
	if q.Granularity <= 0 {
		t.Errorf("expected positive auto-granularity, got %d", q.Granularity)
	}
	if q.TimeRange != 0 {
		t.Errorf("expected TimeRange=0 when using absolute times, got %d", q.TimeRange)
	}
}

func TestApplyTimeRange_ExplicitGranularity(t *testing.T) {
	q := honeycomb.Query{}
	from := time.Unix(1700000000, 0)
	to := from.Add(1 * time.Hour)

	fingerprint.ApplyTimeRange(&q, from, to, 300, 0)

	if q.Granularity != 300 {
		t.Errorf("expected granularity 300, got %d", q.Granularity)
	}
}

// Auto-granularity should follow Grafana's MaxDataPoints (panel width) so
// charts are not undersampled relative to the same query in Honeycomb's UI.
// 1h / 2000 datapoints ≈ 1.8s, which Honeycomb floors at T/1000 = 3.6s and
// the nice-ladder snaps up to 5s.
func TestApplyTimeRange_AutoGranularity_UsesMaxDataPoints(t *testing.T) {
	q := honeycomb.Query{}
	from := time.Unix(1700000000, 0)
	to := from.Add(1 * time.Hour)

	fingerprint.ApplyTimeRange(&q, from, to, 0, 2000)

	if q.Granularity != 5 {
		t.Errorf("expected granularity 5 for 1h with MDP=2000, got %d", q.Granularity)
	}
}

// Without MaxDataPoints the default target is 1000 buckets. For a 1h range
// that lands at the Honeycomb minimum granularity (T/1000 = 3.6s), snapped
// up to 5s on the nice ladder.
func TestApplyTimeRange_AutoGranularity_DefaultTarget(t *testing.T) {
	q := honeycomb.Query{}
	from := time.Unix(1700000000, 0)
	to := from.Add(1 * time.Hour)

	fingerprint.ApplyTimeRange(&q, from, to, 0, 0)

	if q.Granularity != 5 {
		t.Errorf("expected granularity 5 for 1h with MDP=0, got %d", q.Granularity)
	}
}
