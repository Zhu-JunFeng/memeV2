package signal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/model"
)

type fakeSupplyProvider struct {
	supply float64
}

func (p fakeSupplyProvider) GetTokenSupply(context.Context, string) (float64, error) {
	if p.supply <= 0 {
		return 1, nil
	}
	return p.supply, nil
}

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
		birdeye:        fakeKlineSource{items: klines},
		publisher:      pub,
		store:          store,
		supplyProvider: fakeSupplyProvider{supply: 1},
		cfg: CandidateMonitorConfig{
			Enabled:        true,
			Interval:       "1m",
			MinMarketCap:   5,
			LookbackBars:   120,
			LevelOptions:   testLevelOptions(),
			BreakoutFollow: backtest.DefaultBreakoutBandFollowConfig(),
			SupplyProvider: fakeSupplyProvider{supply: 1},
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

func TestCandidateMonitorListCandidates(t *testing.T) {
	base := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, &capturePublisher{})
	store.states["token-old"] = candidateMonitorState{
		TokenAddress: "token-old",
		Symbol:       "OLD",
		RunID:        "run-old",
		StrategyName: "score-v1",
		ScanNo:       1,
		CandidateAt:  base,
		Status:       candidateStatusWatching,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed","score":86.5,"marketCap":21000}`),
	}
	store.states["token-new"] = candidateMonitorState{
		TokenAddress:   "token-new",
		Symbol:         "NEW",
		RunID:          "run-new",
		StrategyName:   "score-v1",
		ScanNo:         2,
		CandidateAt:    base.Add(time.Minute),
		Status:         candidateStatusBought,
		BuySignalID:    "buy-1",
		EntryTime:      base.Add(2 * time.Minute),
		EntryPrice:     24000,
		CurrentPrice:   25500,
		CurrentAt:      base.Add(3 * time.Minute),
		KlineFetchedAt: base.Add(3*time.Minute + 10*time.Second),
		Level:          model.PriceLevel{Price: 23000, Lower: 22800, Upper: 23200},
		RawPayload:     json.RawMessage(`{"event":"candidate_score_passed","score":91.2,"marketCap":26000}`),
	}
	items, err := monitor.ListCandidates(context.Background())
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(items))
	}
	first := items[0]
	if first.TokenAddress != "token-new" || first.UpstreamScore == nil || *first.UpstreamScore != 91.2 || first.UpstreamMarketCap == nil || *first.UpstreamMarketCap != 26000 {
		t.Fatalf("unexpected first item: %#v", first)
	}
	if first.EntryTime == nil || !first.EntryTime.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("expected entry time, got %#v", first.EntryTime)
	}
	if first.CurrentMarketCap == nil || *first.CurrentMarketCap != 25500 {
		t.Fatalf("expected current market cap, got %#v", first.CurrentMarketCap)
	}
	if first.CurrentMarketCapAt == nil || !first.CurrentMarketCapAt.Equal(base.Add(3*time.Minute)) {
		t.Fatalf("expected current market cap time, got %#v", first.CurrentMarketCapAt)
	}
	if first.BirdeyeKlineFetchedAt == nil || !first.BirdeyeKlineFetchedAt.Equal(base.Add(3*time.Minute+10*time.Second)) {
		t.Fatalf("expected Birdeye fetch time, got %#v", first.BirdeyeKlineFetchedAt)
	}
	if first.LevelMarketCap != 23000 || first.LevelLowerMarketCap != 22800 || first.LevelUpperMarketCap != 23200 {
		t.Fatalf("unexpected level fields: %#v", first)
	}
	if items[1].EntryTime != nil {
		t.Fatalf("watching candidate should not expose empty entry time: %#v", items[1].EntryTime)
	}
}
