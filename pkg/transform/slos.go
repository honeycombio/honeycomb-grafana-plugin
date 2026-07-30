package transform

import (
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// SLOListToFrame converts a list of SLOs into a single Grafana table frame.
// Suitable for Table panels and template-variable queries.
//
// Columns: id, name, description, target_per_million, time_period_days,
// sli_alias, dataset_slugs.
func SLOListToFrame(slos []honeycomb.SLO) *data.Frame {
	ids := make([]string, len(slos))
	names := make([]string, len(slos))
	descriptions := make([]string, len(slos))
	targets := make([]int64, len(slos))
	periods := make([]int64, len(slos))
	slis := make([]string, len(slos))
	datasets := make([]string, len(slos))

	for i, s := range slos {
		ids[i] = s.ID
		names[i] = s.Name
		descriptions[i] = s.Description
		targets[i] = int64(s.TargetPerMillion)
		periods[i] = int64(s.TimePeriodDays)
		slis[i] = s.SLI.Alias
		datasets[i] = joinStrings(s.DatasetSlugs, ",")
	}

	frame := data.NewFrame("slos",
		data.NewField("id", nil, ids),
		data.NewField("name", nil, names),
		data.NewField("description", nil, descriptions),
		data.NewField("target_per_million", nil, targets),
		data.NewField("time_period_days", nil, periods),
		data.NewField("sli_alias", nil, slis),
		data.NewField("dataset_slugs", nil, datasets),
	)
	frame.Meta = &data.FrameMeta{Type: data.FrameTypeTable}
	return frame
}

// SLODetailToFrame converts a detailed SLO (compliance + budget + burn_rate +
// status) into a single-row Grafana table frame. The Stat panel can pick any
// numeric field for display (compliance is most common).
//
// Detailed fields are emitted as nullable so missing values don't render as 0.
func SLODetailToFrame(slo *honeycomb.SLO) *data.Frame {
	if slo == nil {
		return data.NewFrame("slo")
	}

	target := percentFromPerMillion(slo.TargetPerMillion)

	frame := data.NewFrame("slo",
		data.NewField("id", nil, []string{slo.ID}),
		data.NewField("name", nil, []string{slo.Name}),
		data.NewField("status", nil, []string{slo.Status}),
		data.NewField("compliance", nil, []*float64{slo.Compliance}),
		data.NewField("budget_remaining", nil, []*float64{slo.BudgetRemaining}),
		data.NewField("burn_rate", nil, []*float64{slo.BurnRate}),
		data.NewField("target", nil, []*float64{&target}),
	)
	// Compliance, budget_remaining, target are percentages → unit hint.
	for _, name := range []string{"compliance", "budget_remaining", "target"} {
		if f, idx := frame.FieldByName(name); idx != -1 {
			if f.Config == nil {
				f.Config = &data.FieldConfig{}
			}
			f.Config.Unit = "percent"
		}
	}
	frame.Meta = &data.FrameMeta{Type: data.FrameTypeTable}
	return frame
}

// percentFromPerMillion converts a target_per_million (e.g. 999900 = 99.99%)
// to a percent value. Returned as 0–100 to match Grafana's "percent" unit.
func percentFromPerMillion(v int) float64 {
	return float64(v) / 10000.0
}

func joinStrings(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for _, x := range s[1:] {
		out += sep + x
	}
	return out
}
