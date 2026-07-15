package gmgnprojects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"solana-meme-backtest/backend/internal/integration/gmgnkeys"
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

func TestFetchTokenSymbol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/token/info" || r.URL.Query().Get("address") != "ca-1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"symbol":"TOKEN","name":"Token Name"}}`))
	}))
	defer server.Close()

	symbol, err := NewClient(server.URL, "test-key", server.Client()).FetchTokenSymbol(context.Background(), "ca-1")
	if err != nil {
		t.Fatal(err)
	}
	if symbol != "TOKEN" {
		t.Fatalf("unexpected symbol: %s", symbol)
	}
}

func TestClientUsesSharedKeySchedulerForAllRequests(t *testing.T) {
	var gotKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = append(gotKeys, r.Header.Get("X-APIKEY"))
		switch r.URL.Path {
		case "/v1/trenches":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"completed":[]}}`))
		case "/v1/token/info":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"symbol":"TOKEN"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	scheduler := gmgnkeys.NewScheduler(nil, []string{"key-a", "key-b"}, 0)
	client := NewClient(server.URL, "ignored-key", server.Client()).WithKeyScheduler(scheduler)
	if _, err := client.FetchCompletedProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchTokenSymbol(context.Background(), "ca-1"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"key-a", "key-b"}; !reflect.DeepEqual(gotKeys, want) {
		t.Fatalf("expected shared key rotation %#v, got %#v", want, gotKeys)
	}
}

func TestClientRetriesNextKeyOnRateLimit(t *testing.T) {
	var gotKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-APIKEY")
		gotKeys = append(gotKeys, key)
		if key == "limited-key" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":429,"message":"rate limit"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"completed":[]}}`))
	}))
	defer server.Close()

	scheduler := gmgnkeys.NewScheduler(nil, []string{"limited-key", "good-key"}, 0)
	client := NewClient(server.URL, "", server.Client()).WithKeyScheduler(scheduler)
	if _, err := client.FetchCompletedProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"limited-key", "good-key"}; !reflect.DeepEqual(gotKeys, want) {
		t.Fatalf("expected rate-limit retry %#v, got %#v", want, gotKeys)
	}
}
