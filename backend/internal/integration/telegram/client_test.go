package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"solana-meme-backtest/backend/internal/model"
)

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
	if err := client.NotifySignal(context.Background(), model.TradeSignal{TradeMode: model.TradeModeLive, SignalType: model.TradeSignalTypeSell, TokenAddress: "ca-1"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"卖出信号", "模式：实盘", "CA：ca-1"} {
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
	if err := client.NotifyTrade(context.Background(), model.TradeFill{TradeMode: model.TradeModePaper, Side: model.TradeSignalTypeBuy, TokenAddress: "ca-2"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"买入交易成功", "模式：模拟盘", "CA：ca-2"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q missing %q", message, expected)
		}
	}
}
