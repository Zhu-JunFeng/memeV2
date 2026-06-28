package trade

import (
	"testing"

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

func TestDecodeTradeSignalPayloadRejectsCandidateScorePassed(t *testing.T) {
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

	if _, err := decodeTradeSignalPayload(payload); err == nil {
		t.Fatalf("expected candidate_score_passed to be rejected by trade worker")
	}
}

func TestDecodeTradeSignalPayloadRejectsCandidateWithoutPublishedAt(t *testing.T) {
	payload := []byte(`{"event":"candidate_score_passed","runId":"run_1","tokenAddress":"token-a"}`)
	if _, err := decodeTradeSignalPayload(payload); err == nil {
		t.Fatalf("expected candidate event error")
	}
}
