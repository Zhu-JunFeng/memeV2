package trade

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	solana "github.com/gagliardetto/solana-go"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

type stubPriceProvider struct {
	prices map[string]float64
}

func (p stubPriceProvider) GetTokenPrice(_ context.Context, tokenAddress string) (float64, error) {
	if value, ok := p.prices[tokenAddress]; ok {
		return value, nil
	}
	return 0, nil
}

func TestLocalizeJupiterMessage(t *testing.T) {
	if got := localizeJupiterMessage("Insufficient funds"); got != "余额不足" {
		t.Fatalf("expected localized insufficient funds, got %q", got)
	}
}

func TestPaperModeUsesQuoteWithoutWalletBalance(t *testing.T) {
	privateKey, err := solana.NewRandomPrivateKey()
	if err != nil {
		t.Fatalf("new random private key: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/swap/v1/quote") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"inputMint":      wrappedSOLMint,
				"inAmount":       "50000000",
				"outputMint":     "token-a",
				"outAmount":      "2500000",
				"slippageBps":    500,
				"priceImpactPct": "0.01",
			})
			return
		}
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"result": map[string]any{
					"value": map[string]any{
						"decimals": 6,
					},
				},
			})
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	executor := &JupiterExecutor{
		cfg: config.TradeConfig{
			Jupiter:      config.JupiterConfig{BaseURL: server.URL, APIKey: "test"},
			SolanaRPCURL: server.URL,
			SlippageBPS:  500,
		},
		client:        server.Client(),
		priceProvider: stubPriceProvider{prices: map[string]float64{wrappedSOLMint: 200}},
		privateKey:    privateKey,
		walletAddress: privateKey.PublicKey().String(),
	}
	req := ExecutionRequest{
		Account: model.TradeAccount{BuyAmountSOL: 0.1},
		Signal: model.TradeSignal{
			TokenAddress: "token-a",
			SignalType:   model.TradeSignalTypeBuy,
		},
		Order: model.TradeOrder{Side: model.TradeSignalTypeBuy},
		Config: config.TradeConfig{
			SlippageBPS: 500,
		},
		Mode: model.TradeModePaper,
	}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute paper trade: %v", err)
	}
	if !result.Simulated {
		t.Fatalf("expected simulated result")
	}
	if result.ExecutionChannel != string(model.TradeExecutionChannelJupiterPaper) {
		t.Fatalf("unexpected execution channel: %s", result.ExecutionChannel)
	}
	if result.FilledQuote != 10 {
		t.Fatalf("expected filled quote 10 USD, got %f", result.FilledQuote)
	}
	if result.ExecutedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("unexpected executed time: %s", result.ExecutedAt)
	}
}

var _ datasource.TokenPriceProvider = stubPriceProvider{}
