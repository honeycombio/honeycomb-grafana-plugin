package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/cache"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// AllDatasetsSlug is Honeycomb's special meta-dataset for environment-wide
// queries. /1/columns/__all__ is not a valid endpoint, so we short-circuit
// metadata lookups for it.
const AllDatasetsSlug = "__all__"

// CallResource handles HTTP requests from the frontend to backend resource
// endpoints. These endpoints power metadata discovery in the query editor
// (dataset list, column list) without exposing the API key to the browser.
//
// Routes:
//
//	GET /datasets          – list all dataset slugs and names
//	GET /columns?dataset=X – list columns for a given dataset
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	switch req.Path {
	case "datasets":
		return d.handleListDatasets(ctx, sender)
	case "columns":
		return d.handleListColumns(ctx, req, sender)
	default:
		return sendError(sender, http.StatusNotFound, "unknown resource path")
	}
}

func (d *Datasource) handleListDatasets(ctx context.Context, sender backend.CallResourceResponseSender) error {
	const cacheKey = "meta:datasets"
	if v, ok := d.cache.Get(cacheKey); ok {
		return sendJSON(sender, http.StatusOK, v)
	}

	datasets, err := d.client.ListDatasets(ctx)
	if err != nil {
		d.logger.Error("Failed to list datasets", "error", err)
		return sendError(sender, http.StatusBadGateway, fmt.Sprintf("list datasets: %v", err))
	}

	d.cache.Set(cacheKey, datasets, cache.TTLMetadata)
	return sendJSON(sender, http.StatusOK, datasets)
}

func (d *Datasource) handleListColumns(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	// Parse query parameters from the request URL. Grafana passes the full or
	// relative URL; url.Parse handles both.
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return sendError(sender, http.StatusBadRequest, "invalid request URL")
	}
	dataset := parsed.Query().Get("dataset")
	if dataset == "" {
		return sendError(sender, http.StatusBadRequest, "dataset query parameter is required")
	}

	// __all__ is Honeycomb's environment-wide meta-dataset. /1/columns/__all__
	// returns 404; return an empty list so the editor falls back to free-text.
	if dataset == AllDatasetsSlug {
		return sendJSON(sender, http.StatusOK, []honeycomb.ColumnMeta{})
	}

	cacheKey := "meta:columns:" + dataset
	if v, ok := d.cache.Get(cacheKey); ok {
		return sendJSON(sender, http.StatusOK, v)
	}

	cols, err := d.client.ListColumns(ctx, dataset)
	if err != nil {
		d.logger.Error("Failed to list columns", "dataset", dataset, "error", err)
		return sendError(sender, http.StatusBadGateway, fmt.Sprintf("list columns: %v", err))
	}

	d.cache.Set(cacheKey, cols, cache.TTLMetadata)
	return sendJSON(sender, http.StatusOK, cols)
}

// sendJSON serialises v as JSON and sends it as a resource response.
func sendJSON(sender backend.CallResourceResponseSender, status int, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    b,
	})
}

// sendError sends a JSON error response.
func sendError(sender backend.CallResourceResponseSender, status int, msg string) error {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
}
