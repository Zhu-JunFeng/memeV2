package xxyy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchNewProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/trade/open/api/feed/NEW" || r.URL.Query().Get("chain") != "sol" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected Authorization: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if got := body["kol"]; got != "5," {
			t.Fatalf("unexpected kol filter: %v", got)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"ca":"ca-1","kolNum":6},{"tokenAddress":"ca-2","kol":"5"}]}}`))
	}))
	defer server.Close()

	items, err := NewClient(server.URL, "test-key", server.Client()).FetchNewProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].TokenAddress != "ca-1" || items[0].KOL != 6 || items[1].TokenAddress != "ca-2" || items[1].KOL != 5 {
		t.Fatalf("unexpected items: %#v", items)
	}
}
