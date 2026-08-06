package transform_test

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

func ft(epoch int64) honeycomb.FlexibleTime {
	return honeycomb.FlexibleTime{Time: time.Unix(epoch, 0).UTC()}
}

func result(series []honeycomb.SeriesEntry, results []honeycomb.ResultEntry, query honeycomb.Query) *honeycomb.QueryResultResponse {
	return &honeycomb.QueryResultResponse{
		ID:       "test-result-id",
		Complete: true,
		Query:    query,
		Data: &honeycomb.ResultData{
			Series:  series,
			Results: results,
		},
		Links: honeycomb.Links{QueryURL: "https://ui.honeycomb.io/test"},
	}
}

// ---------------------------------------------------------------------------
// Timeseries
// ---------------------------------------------------------------------------

func TestToFrames_Timeseries_NoBreakdown(t *testing.T) {
	t1 := ft(1700000000)
	t2 := ft(1700003600)

	r := result(
		[]honeycomb.SeriesEntry{
			{Time: t1, Data: map[string]interface{}{"COUNT": float64(42)}},
			{Time: t2, Data: map[string]interface{}{"COUNT": float64(55)}},
		},
		nil,
		honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}},
	)

	frames, err := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTimeseries})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Rows() != 2 {
		t.Errorf("expected 2 rows, got %d", frames[0].Rows())
	}
	// Verify time field
	timeField, _ := frames[0].FieldByName("time")
	if timeField == nil {
		t.Fatal("expected 'time' field")
	}
	// Verify metric field
	countField, _ := frames[0].FieldByName("COUNT")
	if countField == nil {
		t.Fatal("expected 'COUNT' field")
	}
}

func TestToFrames_Timeseries_WithBreakdown(t *testing.T) {
	t1 := ft(1700000000)

	r := result(
		[]honeycomb.SeriesEntry{
			{Time: t1, Data: map[string]interface{}{"COUNT": float64(10), "service.name": "api"}},
			{Time: t1, Data: map[string]interface{}{"COUNT": float64(5), "service.name": "frontend"}},
		},
		nil,
		honeycomb.Query{
			Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
			Breakdowns:   []string{"service.name"},
		},
	)

	frames, err := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTimeseries})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 2 {
		t.Errorf("expected 2 frames (one per group), got %d", len(frames))
	}

	// Each frame should have a 'service.name' label.
	for _, frame := range frames {
		countField, _ := frame.FieldByName("COUNT")
		if countField == nil {
			t.Error("expected COUNT field in each frame")
			continue
		}
		if countField.Labels == nil || countField.Labels["service.name"] == "" {
			t.Errorf("expected service.name label on COUNT field")
		}
	}
}

func TestToFrames_Timeseries_FramesAreDeterministicallyOrdered(t *testing.T) {
	t1 := ft(1700000000)

	r := result(
		[]honeycomb.SeriesEntry{
			{Time: t1, Data: map[string]interface{}{"COUNT": float64(1), "svc": "z-service"}},
			{Time: t1, Data: map[string]interface{}{"COUNT": float64(2), "svc": "a-service"}},
		},
		nil,
		honeycomb.Query{
			Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
			Breakdowns:   []string{"svc"},
		},
	)

	frames1, _ := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTimeseries})
	frames2, _ := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTimeseries})

	if len(frames1) != len(frames2) {
		t.Fatalf("got different frame counts: %d vs %d", len(frames1), len(frames2))
	}
	for i := range frames1 {
		f1, _ := frames1[i].FieldByName("COUNT")
		f2, _ := frames2[i].FieldByName("COUNT")
		if f1.Labels["svc"] != f2.Labels["svc"] {
			t.Errorf("frame %d: non-deterministic ordering: %s vs %s",
				i, f1.Labels["svc"], f2.Labels["svc"])
		}
	}
}

func TestToFrames_Timeseries_MaxGroupsRespected(t *testing.T) {
	var series []honeycomb.SeriesEntry
	for i := 0; i < 10; i++ {
		series = append(series, honeycomb.SeriesEntry{
			Time: ft(1700000000),
			Data: map[string]interface{}{
				"COUNT": float64(i),
				"svc":   string(rune('a' + i)),
			},
		})
	}

	r := result(series, nil, honeycomb.Query{
		Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
		Breakdowns:   []string{"svc"},
	})

	frames, err := transform.ToFrames(r, transform.FrameOptions{
		Mode:      transform.ModeTimeseries,
		MaxGroups: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) > 3 {
		t.Errorf("expected at most 3 frames, got %d", len(frames))
	}
}

// ---------------------------------------------------------------------------
// Table
// ---------------------------------------------------------------------------

func TestToFrames_Table(t *testing.T) {
	r := result(
		nil,
		[]honeycomb.ResultEntry{
			{"service.name": "api", "COUNT": float64(100)},
			{"service.name": "frontend", "COUNT": float64(50)},
		},
		honeycomb.Query{
			Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
			Breakdowns:   []string{"service.name"},
		},
	)

	frames, err := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTable})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Rows() != 2 {
		t.Errorf("expected 2 rows, got %d", frames[0].Rows())
	}
	if frames[0].Meta.Type != data.FrameTypeTable {
		t.Errorf("expected table frame type, got %v", frames[0].Meta.Type)
	}
}

// ---------------------------------------------------------------------------
// Stat
// ---------------------------------------------------------------------------

func TestToFrames_Stat(t *testing.T) {
	r := result(
		nil,
		[]honeycomb.ResultEntry{
			{"COUNT": float64(42)},
		},
		honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}},
	)

	frames, err := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeStat})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Rows() != 1 {
		t.Errorf("expected 1 row, got %d", frames[0].Rows())
	}
}

// ---------------------------------------------------------------------------
// Deep links
// ---------------------------------------------------------------------------

func TestToFrames_AttachesDeepLink(t *testing.T) {
	queryURL := "https://ui.honeycomb.io/team/env/result/123"
	r := result(
		[]honeycomb.SeriesEntry{
			{Time: ft(1700000000), Data: map[string]interface{}{"COUNT": float64(1)}},
		},
		nil,
		honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}},
	)

	frames, err := transform.ToFrames(r, transform.FrameOptions{
		Mode:     transform.ModeTimeseries,
		QueryURL: queryURL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, frame := range frames {
		for _, field := range frame.Fields {
			switch field.Type() {
			case data.FieldTypeNullableFloat64:
				if len(field.Config.Links) == 0 {
					t.Errorf("field %s should have a deep link", field.Name)
				}
				if field.Config.Links[0].URL != queryURL {
					t.Errorf("deep link URL: got %s, want %s", field.Config.Links[0].URL, queryURL)
				}
				if !field.Config.Links[0].TargetBlank {
					t.Error("deep link should open in new tab")
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Empty / nil result handling
// ---------------------------------------------------------------------------

func TestToFrames_NilResult_ReturnsEmptyFrame(t *testing.T) {
	frames, err := transform.ToFrames(nil, transform.FrameOptions{Mode: transform.ModeTimeseries})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 empty frame, got %d", len(frames))
	}
}

func TestToFrames_EmptySeries_ReturnsEmptyFrame(t *testing.T) {
	r := result(
		[]honeycomb.SeriesEntry{},
		nil,
		honeycomb.Query{Calculations: []honeycomb.Calculation{{Op: "COUNT"}}},
	)
	frames, err := transform.ToFrames(r, transform.FrameOptions{Mode: transform.ModeTimeseries})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 empty frame, got %d", len(frames))
	}
}

// ---------------------------------------------------------------------------
// Annotations
// ---------------------------------------------------------------------------

func TestToAnnotationFrames(t *testing.T) {
	r := result(
		[]honeycomb.SeriesEntry{
			{
				Time: ft(time.Now().Unix()),
				Data: map[string]interface{}{
					"COUNT": float64(10),
					"svc":   "api",
				},
			},
		},
		nil,
		honeycomb.Query{
			Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
			Breakdowns:   []string{"svc"},
		},
	)

	frames, err := transform.ToAnnotationFrames(r, transform.FrameOptions{
		Mode:     transform.ModeTimeseries,
		QueryURL: "https://ui.honeycomb.io/test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 annotation frame, got %d", len(frames))
	}
	if frames[0].Rows() != 1 {
		t.Errorf("expected 1 annotation row, got %d", frames[0].Rows())
	}
}
