package honeycomb_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The User-Agent is a wire contract: it is how Honeycomb attributes API traffic
// to this plugin, so a rename is a deliberate decision with an external
// consequence, not an internal tidy-up. Pinned here so it cannot drift silently.
func TestClient_SendsUserAgent(t *testing.T) {
	var gotUA string
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`[]`))
	})

	if _, err := client.ListDatasets(context.Background()); err != nil {
		t.Fatalf("ListDatasets: %v", err)
	}

	const wantPrefix = "honeycomb-grafana-plugin/"
	if !strings.HasPrefix(gotUA, wantPrefix) {
		t.Errorf("User-Agent = %q, want it to start with %q", gotUA, wantPrefix)
	}
	// The version suffix is what makes the header useful for spotting which
	// plugin versions are still in the field.
	if version := strings.TrimPrefix(gotUA, wantPrefix); version == "" {
		t.Errorf("User-Agent %q carries no version", gotUA)
	}
}
