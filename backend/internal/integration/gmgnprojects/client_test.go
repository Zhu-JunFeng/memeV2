package gmgnprojects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCompletedProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/trenches" || r.URL.Query().Get("chain") != "sol" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("X-APIKEY") != "test-key" || r.URL.Query().Get("timestamp") == "" || r.URL.Query().Get("client_id") == "" {
			t.Fatalf("missing GMGN auth fields")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		completed, ok := body["completed"].(map[string]any)
		if !ok || completed["min_renowned_count"] != float64(3) || completed["limit"] != float64(80) {
			t.Fatalf("unexpected completed request: %#v", completed)
		}
		if _, exists := body["new_creation"]; exists {
			t.Fatalf("new_creation must not be requested: %#v", body)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"completed":[{"address":"ca-1","renowned_count":3},{"address":"ca-2","renowned_count":2}]}}`))
	}))
	defer server.Close()

	items, err := NewClient(server.URL, "test-key", server.Client()).FetchCompletedProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].TokenAddress != "ca-1" || items[0].KOL != 3 || items[1].KOL != 2 {
		t.Fatalf("unexpected items: %#v", items)
	}
}
