package datasource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeBirdeyeKeyPool struct {
	keys          []string
	unavailable   []string
	unavailableBy map[string]string
	successful    []string
}

func (p *fakeBirdeyeKeyPool) ListAvailableBirdeyeKeys(context.Context) ([]string, error) {
	keys := make([]string, len(p.keys))
	copy(keys, p.keys)
	return keys, nil
}

func (p *fakeBirdeyeKeyPool) MarkBirdeyeKeyUnavailable(_ context.Context, apiKey string, reason string) error {
	p.unavailable = append(p.unavailable, apiKey)
	if p.unavailableBy == nil {
		p.unavailableBy = map[string]string{}
	}
	p.unavailableBy[apiKey] = reason
	return nil
}

func (p *fakeBirdeyeKeyPool) MarkBirdeyeKeySuccessful(_ context.Context, apiKey string) error {
	p.successful = append(p.successful, apiKey)
	return nil
}

func TestBirdeyeDataSourceMarksComputeLimitedKeyAndTriesNextKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") == "bad-key" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "Compute units usage limit exceeded"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"circulating_supply": 1000,
				"market_cap":         10000,
			},
		})
	}))
	defer server.Close()

	pool := &fakeBirdeyeKeyPool{keys: []string{"bad-key", "good-key"}}
	source := NewBirdeyeDataSource(server.URL, nil, "solana").WithKeyPool(pool)
	body, err := source.fetchMarketData(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetch market data: %v", err)
	}
	if body.Data.CirculatingSupply != 1000 {
		t.Fatalf("unexpected body: %#v", body.Data)
	}
	if len(pool.unavailable) != 1 || pool.unavailable[0] != "bad-key" {
		t.Fatalf("expected bad key to be marked unavailable, got %#v", pool.unavailable)
	}
	if len(pool.successful) != 1 || pool.successful[0] != "good-key" {
		t.Fatalf("expected good key to be marked successful, got %#v", pool.successful)
	}
}
