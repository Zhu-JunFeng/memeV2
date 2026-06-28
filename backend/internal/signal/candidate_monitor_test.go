package signal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/model"
)

type fakeCandidateStore struct {
	states   map[string]candidateMonitorState
	emitted  map[string]bool
	stopped  map[string]string
	released []string
}

func newFakeCandidateStore() *fakeCandidateStore {
	return &fakeCandidateStore{states: map[string]candidateMonitorState{}, emitted: map[string]bool{}, stopped: map[string]string{}}
}

func (s *fakeCandidateStore) UpsertCandidate(_ context.Context, state candidateMonitorState) error {
	s.states[state.TokenAddress] = state
	return nil
}
func (s *fakeCandidateStore) ListActive(context.Context) ([]candidateMonitorState, error) {
	items := make([]candidateMonitorState, 0, len(s.states))
	for _, item := range s.states {
		items = append(items, item)
	}
	return items, nil
}
func (s *fakeCandidateStore) SaveState(_ context.Context, state candidateMonitorState) error {
	s.states[state.TokenAddress] = state
	return nil
}
func (s *fakeCandidateStore) StopCandidate(_ context.Context, state candidateMonitorState, status string) error {
	state.Status = status
	s.stopped[state.TokenAddress] = status
	delete(s.states, state.TokenAddress)
	return nil
}
func (s *fakeCandidateStore) AcquireEmission(_ context.Context, signalID string) (bool, error) {
	if s.emitted[signalID] {
		return false, nil
	}
	s.emitted[signalID] = true
	return true, nil
}
func (s *fakeCandidateStore) ReleaseEmission(_ context.Context, signalID string) error {
	delete(s.emitted, signalID)
	s.released = append(s.released, signalID)
	return nil
}

func testCandidateMonitor(store *fakeCandidateStore, klines []model.Kline, pub *capturePublisher) *CandidateMonitor {
	return &CandidateMonitor{
		birdeye:   fakeKlineSource{items: klines},
		publisher: pub,
		store:     store,
		cfg: CandidateMonitorConfig{
			Enabled:        true,
			Interval:       "1m",
			MinMarketCap:   5,
			LookbackBars:   120,
			LevelOptions:   testLevelOptions(),
			BreakoutFollow: backtest.DefaultBreakoutBandFollowConfig(),
		},
	}
}

func testLevelOptions() backtest.LevelOptions {
	return backtest.LevelOptions{PivotWindow: 1, PriceTolerance: 0.02, BreakTolerance: 0.01, ConfirmBars: 1, VolumeWindow: 3, VolumeMultiplier: 1.2, MaxLevels: 4, WindowSize: 6, WindowStep: 1, MinTouches: 3}
}

func TestCandidateMonitorStopsWatchingLowMarketCap(t *testing.T) {
	base := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, []model.Kline{{TokenAddress: "token-a", Interval: "1m", OpenTime: base, MarketCapClose: 14999}}, pub)
	monitor.cfg.MinMarketCap = 15000
	state := candidateMonitorState{TokenAddress: "token-a", RunID: "run-1", CandidateAt: base, Status: candidateStatusWatching, RawPayload: json.RawMessage(`{"event":"candidate_score_passed"}`)}
	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if store.stopped["token-a"] != candidateStatusStopped {
		t.Fatalf("expected low market cap candidate to stop, got %#v", store.stopped)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected no trade signal, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorPublishesBuyAfterBreakout(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 11.2, MarketCapLow: 9.8, MarketCapClose: 10.95, Volume: 320},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, klines, pub)
	state := candidateMonitorState{TokenAddress: "token-a", RunID: "run-1", ScanNo: 7, CandidateAt: base.Add(5*time.Minute + 30*time.Second), Status: candidateStatusWatching, RawPayload: json.RawMessage(`{"event":"candidate_score_passed"}`)}
	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if len(pub.tradeSignals) != 1 {
		t.Fatalf("expected one buy signal, got %#v", pub.tradeSignals)
	}
	signal := pub.tradeSignals[0]
	if signal.SignalType != model.TradeSignalTypeBuy || signal.StrategyCode != strategyBreakoutFollow {
		t.Fatalf("unexpected buy signal: %#v", signal)
	}
	stored := store.states["token-a"]
	if stored.Status != candidateStatusBought || stored.BuySignalID != signal.SignalID || stored.Level.Breakout == nil {
		t.Fatalf("expected bought state with level, got %#v", stored)
	}
}

func TestCandidateMonitorPublishesSellAfterTakeProfit(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 10}
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-1",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Minute),
		EntryTime:    base,
		EntryPrice:   10,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
		Level:        model.PriceLevel{Price: 9.8, Upper: 9.8, Breakout: &model.BreakoutSetup{BuyPoint: &entry, BreakoutPoint: &entry}},
	}
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 10, MarketCapHigh: 10.1, MarketCapLow: 9.9, MarketCapClose: 10, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10, MarketCapHigh: 10.9, MarketCapLow: 10, MarketCapClose: 10.8, Volume: 200},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, klines, pub)
	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if len(pub.tradeSignals) != 1 {
		t.Fatalf("expected one sell signal, got %#v", pub.tradeSignals)
	}
	signal := pub.tradeSignals[0]
	if signal.SignalType != model.TradeSignalTypeSell || signal.StrategyCode != strategyBreakoutFollow {
		t.Fatalf("unexpected sell signal: %#v", signal)
	}
	if store.stopped["token-a"] != candidateStatusSold {
		t.Fatalf("expected sold state, got %#v", store.stopped)
	}
}
