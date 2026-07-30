package honeycomb_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/honeycomb"
)

func TestClient_ListSLOs(t *testing.T) {
	var gotPath string
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "slo-1", "name": "Checkout latency", "time_period_days": 30, "target_per_million": 995000},
			{"id": "slo-2", "name": "Checkout errors", "time_period_days": 7, "target_per_million": 999000},
		})
	})

	slos, err := client.ListSLOs(context.Background(), "production")
	if err != nil {
		t.Fatalf("ListSLOs: %v", err)
	}

	if gotPath != "/1/slos/production" {
		t.Errorf("path = %q, want /1/slos/production", gotPath)
	}
	if len(slos) != 2 {
		t.Fatalf("got %d SLOs, want 2", len(slos))
	}
	if slos[0].ID != "slo-1" || slos[0].Name != "Checkout latency" {
		t.Errorf("first SLO = %+v", slos[0])
	}
	if slos[0].TargetPerMillion != 995000 {
		t.Errorf("target_per_million = %d, want 995000", slos[0].TargetPerMillion)
	}
	// This endpoint does not return detailed metrics; they must stay nil rather
	// than defaulting to 0, which would render as "0% compliant" on a panel.
	if slos[0].Compliance != nil {
		t.Errorf("Compliance = %v, want nil from the list endpoint", *slos[0].Compliance)
	}
}

// Dataset slugs reach us from user input and can contain characters that would
// otherwise change the request path.
func TestClient_ListSLOs_EscapesDataset(t *testing.T) {
	var gotPath string
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Write([]byte(`[]`))
	})

	if _, err := client.ListSLOs(context.Background(), "my dataset/prod"); err != nil {
		t.Fatalf("ListSLOs: %v", err)
	}

	if strings.Contains(gotPath, " ") {
		t.Errorf("path %q contains an unescaped space", gotPath)
	}
	if got, want := gotPath, "/1/slos/my%20dataset%2Fprod"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestClient_ListSLOs_Error(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unknown API key"}`))
	})

	_, err := client.ListSLOs(context.Background(), "production")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The wrapped message has to name the dataset, or a multi-dataset dashboard
	// gives no clue which panel failed.
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("error %q should name the dataset", err)
	}
}

func TestClient_GetSLO(t *testing.T) {
	compliance := 99.7

	t.Run("detailed adds the query parameter", func(t *testing.T) {
		var gotPath, gotQuery string
		_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":               "slo-1",
				"name":             "Checkout latency",
				"compliance":       compliance,
				"budget_remaining": 42.5,
				"status":           "ok",
				"burn_rate":        0.5,
			})
		})

		slo, err := client.GetSLO(context.Background(), "production", "slo-1", true)
		if err != nil {
			t.Fatalf("GetSLO: %v", err)
		}

		if gotPath != "/1/slos/production/slo-1" {
			t.Errorf("path = %q, want /1/slos/production/slo-1", gotPath)
		}
		if gotQuery != "detailed=true" {
			t.Errorf("query = %q, want detailed=true", gotQuery)
		}
		if slo.Compliance == nil || *slo.Compliance != compliance {
			t.Errorf("Compliance = %v, want %v", slo.Compliance, compliance)
		}
		if slo.Status != "ok" {
			t.Errorf("Status = %q, want ok", slo.Status)
		}
	})

	t.Run("non-detailed omits the query parameter", func(t *testing.T) {
		var gotQuery string
		_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]string{"id": "slo-1"})
		})

		if _, err := client.GetSLO(context.Background(), "production", "slo-1", false); err != nil {
			t.Fatalf("GetSLO: %v", err)
		}
		if gotQuery != "" {
			t.Errorf("query = %q, want empty", gotQuery)
		}
	})

	t.Run("escapes both path segments", func(t *testing.T) {
		var gotPath string
		_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			json.NewEncoder(w).Encode(map[string]string{"id": "x"})
		})

		if _, err := client.GetSLO(context.Background(), "a b", "c/d", false); err != nil {
			t.Fatalf("GetSLO: %v", err)
		}
		if got, want := gotPath, "/1/slos/a%20b/c%2Fd"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	})

	t.Run("missing SLO is reported as not found", func(t *testing.T) {
		_, client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"slo not found"}`))
		})

		_, err := client.GetSLO(context.Background(), "production", "nope", true)
		if err == nil {
			t.Fatal("expected an error")
		}
		// Callers branch on this to tell "deleted SLO" from "broken credentials".
		if !honeycomb.IsNotFound(err) {
			t.Errorf("IsNotFound(%v) = false, want true", err)
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Errorf("error %q should name the SLO id", err)
		}
	})
}
