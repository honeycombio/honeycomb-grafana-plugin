package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/honeycombio/grafana-honeycomb-datasource/pkg/plugin"
)

// recordingSender captures what CallResource sends back. backend's
// CallResourceResponseSender is a one-method interface, so this is all the test
// double needed — no mocking framework.
type recordingSender struct {
	responses []*backend.CallResourceResponse
}

func (s *recordingSender) Send(resp *backend.CallResourceResponse) error {
	s.responses = append(s.responses, resp)
	return nil
}

// only returns the single response the handler is expected to have sent.
func (s *recordingSender) only(t *testing.T) *backend.CallResourceResponse {
	t.Helper()
	if len(s.responses) != 1 {
		t.Fatalf("expected exactly 1 response, got %d", len(s.responses))
	}
	return s.responses[0]
}

func callResource(t *testing.T, ds backend.CallResourceHandler, path, rawURL string) *backend.CallResourceResponse {
	t.Helper()
	sender := &recordingSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   path,
		Method: http.MethodGet,
		URL:    rawURL,
	}, sender)
	if err != nil {
		t.Fatalf("CallResource(%q) returned error: %v", path, err)
	}
	return sender.only(t)
}

func decodeBody[T any](t *testing.T, resp *backend.CallResourceResponse) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body, err)
	}
	return out
}

func assertJSONResponse(t *testing.T, resp *backend.CallResourceResponse, wantStatus int) {
	t.Helper()
	if resp.Status != wantStatus {
		t.Errorf("status = %d, want %d (body: %s)", resp.Status, wantStatus, resp.Body)
	}
	if got := resp.Headers["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Content-Type = %v, want [application/json]", got)
	}
}

func TestCallResource_UnknownPath(t *testing.T) {
	_, ds := newTestDatasource(t, func(http.ResponseWriter, *http.Request) {
		t.Error("unknown path should not reach Honeycomb")
	})

	resp := callResource(t, ds.(backend.CallResourceHandler), "nope", "/api/.../nope")

	assertJSONResponse(t, resp, http.StatusNotFound)
	if body := decodeBody[map[string]string](t, resp); body["error"] == "" {
		t.Error("expected an error message in the body")
	}
}

func TestCallResource_ListDatasets(t *testing.T) {
	calls := 0
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.URL.Path, "/1/datasets") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]string{
			{"name": "Production", "slug": "production"},
			{"name": "Staging", "slug": "staging"},
		})
	})
	handler := ds.(backend.CallResourceHandler)

	resp := callResource(t, handler, "datasets", "/api/.../datasets")
	assertJSONResponse(t, resp, http.StatusOK)

	datasets := decodeBody[[]map[string]interface{}](t, resp)
	if len(datasets) != 2 || datasets[0]["slug"] != "production" {
		t.Fatalf("unexpected datasets payload: %s", resp.Body)
	}

	// Second call must be served from the metadata cache. This is the whole
	// reason the cache exists — Honeycomb's Create Query Result endpoint allows
	// only 10 req/min, so metadata lookups must not spend that budget.
	_ = callResource(t, handler, "datasets", "/api/.../datasets")
	if calls != 1 {
		t.Errorf("expected 1 upstream call thanks to caching, got %d", calls)
	}
}

func TestCallResource_ListDatasets_UpstreamError(t *testing.T) {
	// 401 rather than 500 deliberately: the client retries 5xx with exponential
	// backoff, which would add ~7s to every run of this suite for no extra
	// coverage. The handler's behaviour is the same either way, and the retry
	// path itself is covered in pkg/honeycomb.
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unknown API key"}`))
	})

	resp := callResource(t, ds.(backend.CallResourceHandler), "datasets", "/api/.../datasets")

	// 502, not 500: the failure is upstream at Honeycomb, not in the plugin.
	assertJSONResponse(t, resp, http.StatusBadGateway)
	if body := decodeBody[map[string]string](t, resp); !strings.Contains(body["error"], "list datasets") {
		t.Errorf("error should say which operation failed, got %q", body["error"])
	}
}

func TestCallResource_ListColumns(t *testing.T) {
	var requestedPaths []string
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"key_name": "duration_ms", "hidden": false},
			{"key_name": "internal", "hidden": true},
		})
	})
	handler := ds.(backend.CallResourceHandler)

	resp := callResource(t, handler, "columns", "/api/.../columns?dataset=production")
	assertJSONResponse(t, resp, http.StatusOK)

	cols := decodeBody[[]map[string]interface{}](t, resp)
	// The backend returns everything; the frontend filters hidden columns.
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d: %s", len(cols), resp.Body)
	}

	// Columns are cached per dataset, so a different dataset must still hit
	// upstream rather than returning the first dataset's columns.
	_ = callResource(t, handler, "columns", "/api/.../columns?dataset=staging")
	if len(requestedPaths) != 2 {
		t.Fatalf("expected 2 upstream calls for 2 datasets, got %d: %v", len(requestedPaths), requestedPaths)
	}
	if requestedPaths[0] == requestedPaths[1] {
		t.Errorf("both calls hit the same path %q; the cache key is not dataset-scoped", requestedPaths[0])
	}

	// Repeating the first dataset must now be cached.
	_ = callResource(t, handler, "columns", "/api/.../columns?dataset=production")
	if len(requestedPaths) != 2 {
		t.Errorf("expected the repeated dataset to be cached, got %d calls", len(requestedPaths))
	}
}

func TestCallResource_ListColumns_MissingDataset(t *testing.T) {
	_, ds := newTestDatasource(t, func(http.ResponseWriter, *http.Request) {
		t.Error("a request without a dataset should not reach Honeycomb")
	})

	resp := callResource(t, ds.(backend.CallResourceHandler), "columns", "/api/.../columns")

	assertJSONResponse(t, resp, http.StatusBadRequest)
	if body := decodeBody[map[string]string](t, resp); !strings.Contains(body["error"], "dataset") {
		t.Errorf("error should mention the missing dataset parameter, got %q", body["error"])
	}
}

// __all__ is Honeycomb's environment-wide meta-dataset. /1/columns/__all__ is
// not a real endpoint and 404s, so the handler has to short-circuit it. If this
// regresses, the query editor shows an error instead of falling back to
// free-text column entry.
func TestCallResource_ListColumns_AllDatasetsShortCircuits(t *testing.T) {
	_, ds := newTestDatasource(t, func(http.ResponseWriter, *http.Request) {
		t.Errorf("dataset %q must not be sent to Honeycomb", plugin.AllDatasetsSlug)
	})

	resp := callResource(t, ds.(backend.CallResourceHandler),
		"columns", "/api/.../columns?dataset="+plugin.AllDatasetsSlug)

	assertJSONResponse(t, resp, http.StatusOK)
	if cols := decodeBody[[]interface{}](t, resp); len(cols) != 0 {
		t.Errorf("expected an empty column list, got %s", resp.Body)
	}
}

func TestCallResource_ListColumns_UpstreamError(t *testing.T) {
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"unknown API key"}`))
	})

	resp := callResource(t, ds.(backend.CallResourceHandler), "columns", "/api/.../columns?dataset=production")

	assertJSONResponse(t, resp, http.StatusBadGateway)
	if body := decodeBody[map[string]string](t, resp); !strings.Contains(body["error"], "list columns") {
		t.Errorf("error should say which operation failed, got %q", body["error"])
	}
}

// Grafana may hand the handler a relative URL or a full one; url.Parse copes
// with both, and the dataset must be read either way.
func TestCallResource_ListColumns_AcceptsAbsoluteAndRelativeURLs(t *testing.T) {
	for _, rawURL := range []string{
		"/api/plugins/x/resources/columns?dataset=production",
		"http://localhost:3000/api/plugins/x/resources/columns?dataset=production",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, ds := newTestDatasource(t, func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode([]map[string]interface{}{{"key_name": "col"}})
			})

			resp := callResource(t, ds.(backend.CallResourceHandler), "columns", rawURL)
			assertJSONResponse(t, resp, http.StatusOK)
		})
	}
}
