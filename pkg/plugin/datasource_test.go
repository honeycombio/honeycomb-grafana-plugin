package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/plugin"
)

// newTestDatasource creates a Datasource backed by a mock Honeycomb server.
func newTestDatasource(t *testing.T, handler http.HandlerFunc) (*httptest.Server, instancemgmt.Instance) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	settings := backend.DataSourceInstanceSettings{
		UID:  "test-uid",
		Name: "Test Honeycomb",
		JSONData: mustMarshal(map[string]string{
			"apiUrl": srv.URL,
		}),
		DecryptedSecureJSONData: map[string]string{
			"apiKey": "test-api-key",
		},
	}

	ds, err := plugin.NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatalf("create datasource: %v", err)
	}
	t.Cleanup(func() {
		if d, ok := ds.(instancemgmt.InstanceDisposer); ok {
			d.Dispose()
		}
	})
	return srv, ds
}

func TestQueryData_SkipsHiddenQueries(t *testing.T) {
	callCount := 0
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]string{"id": "query-1"})
	})

	qds := ds.(backend.QueryDataHandler)
	// The "hide" flag is embedded in the query JSON (Grafana sets it in the
	// raw JSON rather than as a struct field in backend.DataQuery).
	hiddenQueryJSON := mustMarshal(map[string]interface{}{
		"hide":         true,
		"dataset":      "prod",
		"calculations": []map[string]string{{"op": "COUNT"}},
	})
	_, err := qds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  hiddenQueryJSON,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount > 0 {
		t.Errorf("expected no Honeycomb calls for hidden query, got %d", callCount)
	}
}

func TestQueryData_SkipsEmptyQueries(t *testing.T) {
	callCount := 0
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
	})

	qds := ds.(backend.QueryDataHandler)
	resp, err := qds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	qr := resp.Responses["A"]
	if qr.Error != nil {
		t.Errorf("unexpected error for empty query: %v", qr.Error)
	}
	if callCount > 0 {
		t.Errorf("expected no Honeycomb calls for empty query, got %d", callCount)
	}
}

func TestQueryData_ValidQuery_ReturnsFrames(t *testing.T) {
	var requestOrder []string
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/1/queries/"):
			requestOrder = append(requestOrder, "create-query")
			json.NewEncoder(w).Encode(map[string]string{"id": "query-1"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/1/query_results/"):
			requestOrder = append(requestOrder, "create-result")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "result-1",
				"complete": false,
				"links":    map[string]string{"query_url": "https://ui.honeycomb.io/test"},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/1/query_results/"):
			requestOrder = append(requestOrder, "get-result")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":       "result-1",
				"complete": true,
				"query": map[string]interface{}{
					"calculations": []map[string]string{{"op": "COUNT"}},
				},
				"data": map[string]interface{}{
					"series": []map[string]interface{}{
						{"time": time.Now().Unix(), "data": map[string]interface{}{"COUNT": 42.0}},
					},
					"results": []map[string]interface{}{{"COUNT": 42.0}},
				},
				"links": map[string]string{"query_url": "https://ui.honeycomb.io/test"},
			})
		}
	})

	qds := ds.(backend.QueryDataHandler)
	resp, err := qds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				TimeRange: backend.TimeRange{
					From: time.Now().Add(-1 * time.Hour),
					To:   time.Now(),
				},
				JSON: mustMarshal(map[string]interface{}{
					"dataset":      "production",
					"queryMode":    "timeseries",
					"calculations": []map[string]string{{"op": "COUNT"}},
					"limit":        100,
				}),
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	qr := resp.Responses["A"]
	if qr.Error != nil {
		t.Errorf("unexpected query error: %v", qr.Error)
	}
	if len(qr.Frames) == 0 {
		t.Error("expected at least one frame in response")
	}
	if len(requestOrder) < 3 {
		t.Errorf("expected create-query, create-result, get-result; got %v", requestOrder)
	}
}

func TestQueryData_CachesQueryID(t *testing.T) {
	createQueryCount := 0
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/1/queries/"):
			createQueryCount++
			json.NewEncoder(w).Encode(map[string]string{"id": "query-1"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/1/query_results/"):
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "result-1", "complete": false,
				"links": map[string]string{"query_url": "https://ui.honeycomb.io"},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/1/query_results/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "result-1", "complete": true,
				"query": map[string]interface{}{"calculations": []map[string]string{{"op": "COUNT"}}},
				"data": map[string]interface{}{"series": []interface{}{}, "results": []interface{}{}},
				"links": map[string]string{"query_url": "https://ui.honeycomb.io"},
			})
		}
	})

	qds := ds.(backend.QueryDataHandler)
	queryJSON := mustMarshal(map[string]interface{}{
		"dataset":      "production",
		"calculations": []map[string]string{{"op": "COUNT"}},
		"limit":        100,
	})
	tr := backend.TimeRange{
		From: time.Now().Add(-1 * time.Hour),
		To:   time.Now(),
	}

	for i := 0; i < 2; i++ {
		_, err := qds.QueryData(context.Background(), &backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{RefID: "A", TimeRange: tr, JSON: queryJSON},
			},
		})
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
	}

	if createQueryCount > 1 {
		t.Errorf("expected Create Query called once (L1 cache), got %d", createQueryCount)
	}
}

func TestCheckHealth_Success(t *testing.T) {
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{{"slug": "production"}})
	})

	checker := ds.(backend.CheckHealthHandler)
	result, err := checker.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != backend.HealthStatusOk {
		t.Errorf("expected OK status, got %v: %s", result.Status, result.Message)
	}
}

func TestCheckHealth_AuthFailure(t *testing.T) {
	_, ds := newTestDatasource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"invalid API key"}`))
	})

	checker := ds.(backend.CheckHealthHandler)
	result, err := checker.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != backend.HealthStatusError {
		t.Errorf("expected Error status for auth failure, got %v", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
