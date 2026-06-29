package datasource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBirdeyeDataSourceConvertsTokenVolumeToTurnover(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/defi/v3/token/market-data":
			_, _ = w.Write([]byte(`{"success":true,"data":{"circulating_supply":1000,"market_cap":2500}}`))
		case "/defi/v3/ohlcv":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"unix_time":1782457200,"address":"token-a","type":"1m","o":2.3,"h":2.6,"l":2.1,"c":2.5,"v":123.4}]}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	source := NewBirdeyeDataSource(server.URL, []string{"key-a"}, "solana")
	klines, err := source.GetKlines(context.Background(), KlineQuery{
		TokenAddress: "token-a",
		Interval:     "1m",
		StartTime:    time.Unix(1782457140, 0),
		EndTime:      time.Unix(1782457260, 0),
	})
	if err != nil {
		t.Fatalf("get klines: %v", err)
	}
	if len(klines) != 1 {
		t.Fatalf("expected 1 kline, got %d", len(klines))
	}
	if got := klines[0].Volume; got != 308.5 {
		t.Fatalf("expected turnover volume 308.5, got %f", got)
	}
}
