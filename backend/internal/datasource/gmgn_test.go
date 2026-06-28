package datasource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGMGNDataSourceFetchesKlinesAndMapsPriceFields(t *testing.T) {
	var gotAPIKey string
	var gotFrom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-APIKEY")
		gotFrom = r.URL.Query().Get("from")
		if r.URL.Query().Get("chain") != "sol" || r.URL.Query().Get("resolution") != "1m" || r.URL.Query().Get("address") != "token-a" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"list":[{"time":1782655620000,"open":"0.10","close":"0.12","high":"0.13","low":"0.09","volume":"42.5","amount":"1000"}]}}`))
	}))
	defer server.Close()

	source := NewGMGNDataSource(server.URL, "gmgn-test-key", "sol", 0)
	start := time.UnixMilli(1782655560000)
	end := time.UnixMilli(1782655680000)
	items, err := source.GetKlines(context.Background(), KlineQuery{TokenAddress: "token-a", Interval: "1m", StartTime: start, EndTime: end})
	if err != nil {
		t.Fatalf("GetKlines: %v", err)
	}
	if gotAPIKey != "gmgn-test-key" {
		t.Fatalf("expected api key header, got %q", gotAPIKey)
	}
	if gotFrom != "1782655560000" {
		t.Fatalf("expected millisecond from timestamp, got %q", gotFrom)
	}
	if len(items) != 1 {
		t.Fatalf("expected one kline, got %d", len(items))
	}
	item := items[0]
	if item.Open != 0.10 || item.Close != 0.12 || item.High != 0.13 || item.Low != 0.09 || item.Volume != 42.5 {
		t.Fatalf("unexpected price fields: %#v", item)
	}
	if item.MarketCapOpen != item.Open || item.MarketCapClose != item.Close || item.MarketCapHigh != item.High || item.MarketCapLow != item.Low {
		t.Fatalf("expected GMGN price values to drive algorithm fields: %#v", item)
	}
}

func TestGMGNDataSourceRequiresAPIKey(t *testing.T) {
	source := NewGMGNDataSource("", "", "", 0)
	_, err := source.GetKlines(context.Background(), KlineQuery{TokenAddress: "token-a", Interval: "1m"})
	if err != ErrGMGNNotConfigured {
		t.Fatalf("expected ErrGMGNNotConfigured, got %v", err)
	}
}
