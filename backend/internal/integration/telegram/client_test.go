package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestNotifyTradeModeChangeIncludesModesWalletAndTime(t *testing.T) {
	var message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		message = payload["text"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", "chat-1", server.Client())
	client.baseURL = server.URL
	err := client.NotifyTradeModeChange(context.Background(), model.TradeModeChange{
		PreviousMode:  model.TradeModePaper,
		CurrentMode:   model.TradeModeLive,
		WalletAddress: "wallet-1",
		ChangedAt:     time.Date(2026, 7, 14, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"交易模式已切换", "原模式：模拟盘", "新模式：实盘", "钱包：wallet-1", "时间：2026-07-14 19:30:00"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q missing %q", message, expected)
		}
	}
}

func TestNotifySignalIncludesModeAndDirection(t *testing.T) {
	var message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["chat_id"] != "chat-1" {
			t.Fatalf("unexpected chat id: %s", payload["chat_id"])
		}
		message = payload["text"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", "chat-1", server.Client())
	client.baseURL = server.URL
	if err := client.NotifySignal(context.Background(), model.TradeSignal{
		TradeMode:        model.TradeModeLive,
		SignalType:       model.TradeSignalTypeSell,
		TokenAddress:     "ca-1",
		TriggerMarketCap: 265234.41,
		RawPayloadJSON:   json.RawMessage(`{"metadata":{"profitRate":-0.0314}}`),
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"卖出信号", "模式：实盘", "CA：ca-1", "触发市值：265.23k", "盈亏（U）：-3.14%"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q missing %q", message, expected)
		}
	}
}

func TestNotifyTradeIncludesPaperMode(t *testing.T) {
	var message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		message = payload["text"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", "chat-1", server.Client())
	client.baseURL = server.URL
	if err := client.NotifyTrade(context.Background(), model.TradeFill{TradeMode: model.TradeModePaper, Side: model.TradeSignalTypeBuy, TokenAddress: "ca-2", ExecutedMarketCap: 123456.78}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"买入交易成功", "模式：模拟盘", "CA：ca-2", "成交市值：123.46k"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q missing %q", message, expected)
		}
	}
}

func TestNotifySellTradeIncludesProfitRateBasedOnQuoteValue(t *testing.T) {
	var message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		message = payload["text"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("test-token", "chat-1", server.Client())
	client.baseURL = server.URL
	if err := client.NotifyTrade(context.Background(), model.TradeFill{
		TradeMode: model.TradeModePaper, Side: model.TradeSignalTypeSell, TokenAddress: "ca-3",
		ExecutedMarketCap: 98765.43, ProfitRate: 0.0526,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"卖出交易成功", "成交市值：98.77k", "盈亏（U）：+5.26%"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q missing %q", message, expected)
		}
	}
}
