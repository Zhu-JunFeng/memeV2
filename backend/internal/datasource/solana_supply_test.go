package datasource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSOLBalanceUsesConfirmedRPCBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Method != "getBalance" || len(payload.Params) != 2 || payload.Params[0] != "wallet-1" {
			t.Fatalf("unexpected RPC payload: %+v", payload)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"value":998000000},"id":1}`))
	}))
	defer server.Close()

	provider := NewSolanaRPCSupplyProvider(server.URL)
	balance, err := provider.GetSOLBalance(context.Background(), "wallet-1")
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0.998 {
		t.Fatalf("expected 0.998 SOL, got %.9f", balance)
	}
}
