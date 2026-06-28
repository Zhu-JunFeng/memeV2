package trade

import (
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestDecodeTradeSignalPayloadKeepsPressureBreakoutFormat(t *testing.T) {
	payload := []byte(`{
		"signalId":"sig-1",
		"signalType":"buy",
		"strategyCode":"pressure_breakout",
		"tokenAddress":"token-a",
		"interval":"1m",
		"signalTime":"2026-06-28T10:00:00Z",
		"triggerPrice":0.12,
		"triggerMarketCap":120000,
		"reason":"breakout"
	}`)

	signal, err := decodeTradeSignalPayload(payload)
	if err != nil {
		t.Fatalf("decode signal: %v", err)
	}
	if signal.SignalID != "sig-1" || signal.SignalType != model.TradeSignalTypeBuy || signal.StrategyCode != "pressure_breakout" {
		t.Fatalf("unexpected decoded signal: %#v", signal)
	}
}

func TestDecodeTradeSignalPayloadConvertsCandidateScorePassed(t *testing.T) {
	payload := []byte(`{
		"event":"candidate_score_passed",
		"runId":"run_1",
		"strategyName":"AB-B",
		"scanNo":28,
		"token":"nothing",
		"tokenAddress":"JEG4fDCBX28BTzXSJi4CQUSVK9xfCJbV3jzCkKj1pump",
		"pairAddress":"F5q3gZrPTXjPxVVY5ocru6P61PvFr571A1FNYhJVhg2q",
		"score":90.88,
		"liquidity":21526.75,
		"marketCap":83656,
		"signalPrice":0.00008365,
		"signalVolumeM5":6277.65,
		"buyRatio":0.56,
		"priceChange5m":10.88,
		"volume24h":508807.99,
		"observedAt":1782567293064,
		"expiresAt":1782567593064,
		"publishedAt":1782567302796,
		"pullback":{"maxDropPct":8}
	}`)

	signal, err := decodeTradeSignalPayload(payload)
	if err != nil {
		t.Fatalf("decode candidate signal: %v", err)
	}
	if signal.SignalID != "candidate_score_passed:run_1:28:JEG4fDCBX28BTzXSJi4CQUSVK9xfCJbV3jzCkKj1pump:1782567302796" {
		t.Fatalf("unexpected signal id: %s", signal.SignalID)
	}
	if signal.SignalType != model.TradeSignalTypeBuy {
		t.Fatalf("expected buy signal, got %s", signal.SignalType)
	}
	if signal.StrategyCode != "candidate_score_passed" || signal.Interval != "candidate_pool" {
		t.Fatalf("unexpected strategy or interval: %#v", signal)
	}
	if signal.TokenAddress != "JEG4fDCBX28BTzXSJi4CQUSVK9xfCJbV3jzCkKj1pump" {
		t.Fatalf("unexpected token address: %s", signal.TokenAddress)
	}
	if signal.TriggerPrice != 0.00008365 || signal.TriggerMarketCap != 83656 {
		t.Fatalf("unexpected trigger fields: price=%f marketCap=%f", signal.TriggerPrice, signal.TriggerMarketCap)
	}
	expectedTime := time.UnixMilli(1782567302796).UTC()
	if !signal.SignalTime.Equal(expectedTime) {
		t.Fatalf("expected signal time %s, got %s", expectedTime, signal.SignalTime)
	}
	if len(signal.Metadata) == 0 {
		t.Fatalf("expected original payload in metadata")
	}
}

func TestDecodeTradeSignalPayloadRejectsCandidateWithoutPublishedAt(t *testing.T) {
	payload := []byte(`{"event":"candidate_score_passed","runId":"run_1","tokenAddress":"token-a"}`)
	if _, err := decodeTradeSignalPayload(payload); err == nil {
		t.Fatalf("expected missing publishedAt error")
	}
}
