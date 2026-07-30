package honeycomb_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

// newTestServer creates an httptest.Server that handles requests with the
// provided handler function.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *honeycomb.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := honeycomb.New(honeycomb.Config{
		APIURL: srv.URL,
		APIKey: "test-api-key",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return srv, client
}

func TestClient_CreateQuery_Success(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-Honeycomb-Team") != "test-api-key" {
			t.Errorf("missing or wrong API key header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "query-123"})
	})

	id, err := client.CreateQuery(context.Background(), "production", honeycomb.Query{
		Calculations: []honeycomb.Calculation{{Op: "COUNT"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "query-123" {
		t.Errorf("expected query ID 'query-123', got %s", id)
	}
}

func TestClient_CreateQuery_PropagatesAPIError(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"invalid query"}`))
	})

	_, err := client.CreateQuery(context.Background(), "production", honeycomb.Query{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !honeycomb.IsRateLimit(err) && err.Error() == "" {
		t.Error("expected descriptive error message")
	}
}

func TestClient_CreateQueryResult_Success(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(honeycomb.QueryResultCreateResponse{
			ID:       "result-456",
			Complete: false,
			Links:    honeycomb.Links{QueryURL: "https://ui.honeycomb.io/test"},
		})
	})

	resp, err := client.CreateQueryResult(context.Background(), "production", honeycomb.QueryResultRequest{
		QueryID: "query-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "result-456" {
		t.Errorf("expected result ID 'result-456', got %s", resp.ID)
	}
}

func TestClient_CreateQueryResult_RateLimit429(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"rate limited"}`))
	})

	_, err := client.CreateQueryResult(context.Background(), "production", honeycomb.QueryResultRequest{
		QueryID: "query-123",
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !honeycomb.IsRateLimit(err) {
		t.Errorf("expected IsRateLimit(err)==true, got false; err=%v", err)
	}
}

func TestClient_GetQueryResult_PollsUntilComplete(t *testing.T) {
	callCount := 0
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		complete := callCount >= 3
		json.NewEncoder(w).Encode(honeycomb.QueryResultResponse{
			ID:       "result-456",
			Complete: complete,
			Data:     &honeycomb.ResultData{},
			Links:    honeycomb.Links{QueryURL: "https://ui.honeycomb.io/test"},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetQueryResult(ctx, "production", "result-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Complete {
		t.Error("expected complete=true")
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 poll calls, got %d", callCount)
	}
}

func TestClient_GetQueryResult_TimesOutIfNeverComplete(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(honeycomb.QueryResultResponse{
			ID:       "result-456",
			Complete: false,
		})
	})

	// Use a very short context to force timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GetQueryResult(ctx, "production", "result-456")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClient_HealthCheck_Success(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{{"name": "production", "slug": "production"}})
	})

	err := client.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("unexpected health check error: %v", err)
	}
}

func TestClient_HealthCheck_AuthFailure(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"invalid API key"}`))
	})

	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestClient_New_RequiresAPIKey(t *testing.T) {
	_, err := honeycomb.New(honeycomb.Config{APIKey: ""})
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestClient_RateLimitHeaderParsing(t *testing.T) {
	tests := []struct {
		header    string
		wantLimit int
		wantRem   int
		wantReset time.Duration
	}{
		{"limit=10, remaining=8, reset=54", 10, 8, 54 * time.Second},
		{"limit=100, remaining=0, reset=1", 100, 0, 1 * time.Second},
		{"", 0, 0, 0},
	}
	for _, tc := range tests {
		info := honeycomb.ParseRateLimitHeader(tc.header)
		if info.Limit != tc.wantLimit {
			t.Errorf("header %q: limit: got %d, want %d", tc.header, info.Limit, tc.wantLimit)
		}
		if info.Remaining != tc.wantRem {
			t.Errorf("header %q: remaining: got %d, want %d", tc.header, info.Remaining, tc.wantRem)
		}
		if info.Reset != tc.wantReset {
			t.Errorf("header %q: reset: got %s, want %s", tc.header, info.Reset, tc.wantReset)
		}
	}
}

func TestClient_ListDatasets(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/datasets" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]honeycomb.DatasetMeta{
			{Name: "Production", Slug: "production"},
			{Name: "Staging", Slug: "staging"},
		})
	})

	datasets, err := client.ListDatasets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(datasets) != 2 {
		t.Errorf("expected 2 datasets, got %d", len(datasets))
	}
	if datasets[0].Slug != "production" {
		t.Errorf("expected slug 'production', got %s", datasets[0].Slug)
	}
}

func TestClient_ListColumns(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]honeycomb.ColumnMeta{
			{KeyName: "duration_ms", Type: "float"},
			{KeyName: "service.name", Type: "string"},
		})
	})

	cols, err := client.ListColumns(context.Background(), "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 columns, got %d", len(cols))
	}
}
