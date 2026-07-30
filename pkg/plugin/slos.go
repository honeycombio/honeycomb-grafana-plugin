package plugin

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/cache"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/transform"
)

// runSLOQuery dispatches an SLO query (list or single) to Honeycomb's /1/slos
// endpoints and produces a Grafana frame.
//
// SLO list responses are cached for cache.TTLMetadata (5 min). Detailed
// single-SLO responses are cached briefly to avoid hammering the endpoint
// when a dashboard renders the same SLO across multiple panels.
func (d *Datasource) runSLOQuery(ctx context.Context, pq HoneycombQuery) backend.DataResponse {
	switch pq.SLOResultType {
	case SLOResultTypeSingle:
		return d.runSLOSingle(ctx, pq)
	case SLOResultTypeList, "":
		return d.runSLOList(ctx, pq)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("unknown sloResultType %q", pq.SLOResultType))
	}
}

func (d *Datasource) runSLOList(ctx context.Context, pq HoneycombQuery) backend.DataResponse {
	cacheKey := "slos:list:" + pq.Dataset
	if v, ok := d.cache.Get(cacheKey); ok {
		frame := transform.SLOListToFrame(v.([]honeycomb.SLO))
		return backend.DataResponse{Frames: data.Frames{frame}}
	}

	slos, err := d.client.ListSLOs(ctx, pq.Dataset)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("list SLOs: %v", err))
	}

	d.cache.Set(cacheKey, slos, cache.TTLMetadata)
	frame := transform.SLOListToFrame(slos)
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func (d *Datasource) runSLOSingle(ctx context.Context, pq HoneycombQuery) backend.DataResponse {
	// Cache key includes detailed=true since that affects payload.
	cacheKey := "slos:single:" + pq.Dataset + ":" + pq.SLOID + ":detailed"

	if v, ok := d.cache.Get(cacheKey); ok {
		slo := v.(*honeycomb.SLO)
		frame := transform.SLODetailToFrame(slo)
		return backend.DataResponse{Frames: data.Frames{frame}}
	}

	slo, err := d.client.GetSLO(ctx, pq.Dataset, pq.SLOID, true)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("get SLO: %v", err))
	}

	d.cache.Set(cacheKey, slo, cache.TTLMetadata)
	frame := transform.SLODetailToFrame(slo)
	return backend.DataResponse{Frames: data.Frames{frame}}
}
