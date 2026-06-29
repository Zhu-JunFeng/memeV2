package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
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

type fakePriceProvider struct {
	sequences map[string][]float64
	calls     map[string]int
}

func (p *fakePriceProvider) GetTokenPrice(_ context.Context, tokenAddress string) (float64, error) {
	if p == nil {
		return 0, fmt.Errorf("price provider not configured")
	}
	items := p.sequences[tokenAddress]
	if len(items) == 0 {
		return 0, fmt.Errorf("missing price for %s", tokenAddress)
	}
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	index := p.calls[tokenAddress]
	if index >= len(items) {
		index = len(items) - 1
	}
	p.calls[tokenAddress]++
	return items[index], nil
}

func (p *fakePriceProvider) GetKlines(_ context.Context, req datasource.KlineQuery) ([]model.Kline, error) {
	price, err := p.GetTokenPrice(context.Background(), req.TokenAddress)
	if err != nil {
		return nil, err
	}
	openTime := req.EndTime.UTC().Truncate(time.Minute)
	if openTime.IsZero() {
		openTime = time.Now().UTC().Truncate(time.Minute)
	}
	return []model.Kline{{
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		OpenTime:     openTime,
		CloseTime:    openTime.Add(time.Minute),
		Open:         price,
		High:         price,
		Low:          price,
		Close:        price,
		Volume:       500,
	}}, nil
}

type fakeCandidateStore struct {
	states   map[string]candidateMonitorState
	emitted  map[string]bool
	stopped  map[string]string
	released []string
}

type fakeMonitorKlineStore struct {
	recent   map[string][]model.Kline
	enqueued [][]model.Kline
}

func newFakeMonitorKlineStore() *fakeMonitorKlineStore {
	return &fakeMonitorKlineStore{recent: map[string][]model.Kline{}}
}

func (s *fakeMonitorKlineStore) GetKlines(_ context.Context, req datasource.KlineQuery) ([]model.Kline, error) {
	return append([]model.Kline(nil), s.recent[req.TokenAddress+"|"+req.Interval]...), nil
}

func (s *fakeMonitorKlineStore) GetRecentKlines(_ context.Context, tokenAddress string, interval string, limit int) ([]model.Kline, error) {
	items := append([]model.Kline(nil), s.recent[tokenAddress+"|"+interval]...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (s *fakeMonitorKlineStore) EnqueueUpsert(klines []model.Kline) {
	s.enqueued = append(s.enqueued, append([]model.Kline(nil), klines...))
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

func testCandidateMonitor(store *fakeCandidateStore, klines []model.Kline, prices map[string][]float64, now time.Time, pub *capturePublisher) *CandidateMonitor {
	systemStore := newFakeMonitorKlineStore()
	if len(klines) > 0 {
		systemStore.recent[klines[0].TokenAddress+"|1m"] = append([]model.Kline(nil), klines...)
	}
	priceProvider := &fakePriceProvider{sequences: prices, calls: map[string]int{}}
	return &CandidateMonitor{
		priceProvider:  priceProvider,
		klineSource:    priceProvider,
		publisher:      pub,
		store:          store,
		supplyProvider: fakeSupplyProvider{supply: 1},
		systemKlines:   systemStore,
		klineCache:     newCandidateKlineCache(0),
		supplyCache:    map[string]float64{},
		cfg: CandidateMonitorConfig{
			Enabled:        true,
			Interval:       "1m",
			MinMarketCap:   monitorMinMarketCap,
			LookbackBars:   120,
			LevelOptions:   testLevelOptions(),
			BreakoutFollow: backtest.DefaultBreakoutBandFollowConfig(),
			SupplyProvider: fakeSupplyProvider{supply: 1},
			PriceProvider:  priceProvider,
			KlineSource:    priceProvider,
			SystemKlines:   systemStore,
			Now:            func() time.Time { return now },
		},
	}
}

func testLevelOptions() backtest.LevelOptions {
	return backtest.LevelOptions{PivotWindow: 1, PriceTolerance: 0.02, BreakTolerance: 0.01, ConfirmBars: 1, VolumeWindow: 3, VolumeMultiplier: 1.2, MaxLevels: 4, WindowSize: 6, WindowStep: 1, MinTouches: 3}
}

func TestCandidateMonitorPreloadSanitizesSystemKlines(t *testing.T) {
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	state := candidateMonitorState{TokenAddress: "token-a", Status: candidateStatusWatching, CandidateAt: base}
	store.states[state.TokenAddress] = state
	monitor := testCandidateMonitor(store, []model.Kline{{
		TokenAddress:   "token-a",
		Interval:       "1m",
		OpenTime:       base,
		CloseTime:      base.Add(time.Minute),
		Open:           10,
		High:           12,
		Low:            9,
		Close:          11,
		MarketCapOpen:  20000,
		MarketCapHigh:  22000,
		MarketCapLow:   19000,
		MarketCapClose: 21000,
		Volume:         123,
	}}, nil, base, &capturePublisher{})

	monitor.preloadActiveKlines(context.Background())
	cached := monitor.klineCache.Get("token-a", "1m")
	if len(cached) != 1 {
		t.Fatalf("expected one cached kline, got %d", len(cached))
	}
	if cached[0].Volume != 123 {
		t.Fatalf("expected preload volume to be preserved, got %#v", cached[0])
	}
	if cached[0].Open != 20000 || cached[0].High != 22000 || cached[0].Low != 19000 || cached[0].Close != 21000 {
		t.Fatalf("expected preload to use market cap values, got %#v", cached[0])
	}
}

func TestCandidateMonitorStopsWatchingLowMarketCap(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 30, 0, time.UTC)
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, map[string][]float64{"token-a": {9.999}}, base, pub)
	state := candidateMonitorState{TokenAddress: "token-a", RunID: "run-1", CandidateAt: base.Add(-time.Minute), Status: candidateStatusWatching, RawPayload: json.RawMessage(`{"event":"candidate_score_passed"}`)}
	store.states[state.TokenAddress] = state
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

func TestCandidateMonitorUsesRealGMGNVolumeForRealtimeBars(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 7, 30, 0, time.UTC)
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, map[string][]float64{"token-a": {10.95}}, base, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 2000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 2000}
	monitor.preloadActiveKlines(context.Background())

	klines, err := monitor.loadLatestKlines(context.Background(), candidateMonitorState{TokenAddress: "token-a"})
	if err != nil {
		t.Fatalf("load latest klines: %v", err)
	}
	if len(klines) == 0 {
		t.Fatalf("expected realtime klines")
	}
	last := klines[len(klines)-1]
	if last.Volume != 501 {
		t.Fatalf("expected realtime bar to keep GMGN volume, got %#v", last)
	}
	if last.MarketCapClose != 21900 {
		t.Fatalf("expected market cap close 21900, got %#v", last)
	}
}

func TestCandidateMonitorPublishesBuyAfterBreakout(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 18000, MarketCapHigh: 18800, MarketCapLow: 17600, MarketCapClose: 18200, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 18200, MarketCapHigh: 20800, MarketCapLow: 18000, MarketCapClose: 19600, Volume: 200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 19600, MarketCapHigh: 19800, MarketCapLow: 18400, MarketCapClose: 18800, Volume: 120},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 18800, MarketCapHigh: 20900, MarketCapLow: 18600, MarketCapClose: 19700, Volume: 240},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 19700, MarketCapHigh: 19900, MarketCapLow: 18800, MarketCapClose: 19000, Volume: 140},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 19000, MarketCapHigh: 21000, MarketCapLow: 18900, MarketCapClose: 19800, Volume: 280},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 19800, MarketCapHigh: 22500, MarketCapLow: 19600, MarketCapClose: 21900, Volume: 320},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(7*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {10.95}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 2000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 2000}
	// 实时监控现在只会使用入池后的累计K线，因此测试要保证试压窗口也发生在入池之后。
	state := candidateMonitorState{TokenAddress: "token-a", RunID: "run-1", ScanNo: 7, CandidateAt: base, Status: candidateStatusWatching, RawPayload: json.RawMessage(`{"event":"candidate_score_passed"}`)}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

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
	if len(monitor.systemKlines.(*fakeMonitorKlineStore).enqueued) == 0 {
		t.Fatalf("expected latest synthetic kline to be enqueued")
	}
}

func TestCandidateMonitorSkipsImmediateBuyWhenRealtimeVolumeStillWeak(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 18000, MarketCapHigh: 18800, MarketCapLow: 17600, MarketCapClose: 18200, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 18200, MarketCapHigh: 20800, MarketCapLow: 18000, MarketCapClose: 19600, Volume: 200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 19600, MarketCapHigh: 19800, MarketCapLow: 18400, MarketCapClose: 18800, Volume: 120},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 18800, MarketCapHigh: 20900, MarketCapLow: 18600, MarketCapClose: 19700, Volume: 240},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 19700, MarketCapHigh: 19900, MarketCapLow: 18800, MarketCapClose: 19000, Volume: 140},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 19000, MarketCapHigh: 21000, MarketCapLow: 18900, MarketCapClose: 19800, Volume: 280},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(5*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {10.95}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 2000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 2000}
	state := candidateMonitorState{TokenAddress: "token-a", RunID: "run-1", ScanNo: 7, CandidateAt: base.Add(4*time.Minute + 30*time.Second), Status: candidateStatusWatching, RawPayload: json.RawMessage(`{"event":"candidate_score_passed"}`)}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected no buy signal when realtime breakout volume is still weak, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorSkipsSameBarReentryAfterSell(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 18000, MarketCapHigh: 18800, MarketCapLow: 17600, MarketCapClose: 18200, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 18200, MarketCapHigh: 20800, MarketCapLow: 18000, MarketCapClose: 19600, Volume: 200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 19600, MarketCapHigh: 19800, MarketCapLow: 18400, MarketCapClose: 18800, Volume: 120},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 18800, MarketCapHigh: 20900, MarketCapLow: 18600, MarketCapClose: 19700, Volume: 240},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 19700, MarketCapHigh: 19900, MarketCapLow: 18800, MarketCapClose: 19000, Volume: 140},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 19000, MarketCapHigh: 21000, MarketCapLow: 18900, MarketCapClose: 19800, Volume: 280},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 19800, MarketCapHigh: 22500, MarketCapLow: 19600, MarketCapClose: 21900, Volume: 320},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(7*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {10.95}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 2000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 2000}
	state := candidateMonitorState{
		TokenAddress:        "token-a",
		RunID:               "run-1",
		ScanNo:              7,
		CandidateAt:         base.Add(4 * time.Minute),
		Status:              candidateStatusWatching,
		LastDecisionBarTime: base.Add(7 * time.Minute),
		LastExitBarTime:     base.Add(7 * time.Minute),
		RawPayload:          json.RawMessage(`{"event":"candidate_score_passed"}`),
	}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected same closed bar reentry to be skipped, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorPublishesSellAfterTakeProfit(t *testing.T) {
	base := time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 10000}
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-1",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Minute),
		EntryTime:    base,
		EntryPrice:   10000,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
		Level:        model.PriceLevel{Price: 9800, Upper: 9800, Breakout: &model.BreakoutSetup{BuyPoint: &entry, BreakoutPoint: &entry}},
	}
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 10000, MarketCapHigh: 10100, MarketCapLow: 9900, MarketCapClose: 10000, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10000, MarketCapHigh: 11000, MarketCapLow: 9950, MarketCapClose: 10800, Volume: 120},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(2*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {10.8}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 1000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 1000}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

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
	stored := store.states["token-a"]
	if stored.Status != candidateStatusWatching {
		t.Fatalf("expected rearmed watching state, got %#v", stored)
	}
	if stored.BuySignalID != "" || !stored.EntryTime.IsZero() || stored.EntryPrice != 0 {
		t.Fatalf("expected cleared entry fields after rearm, got %#v", stored)
	}
}

func TestCandidateMonitorBoughtCandidateKeepsMonitoringBelowTenK(t *testing.T) {
	base := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 9500}
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-1",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Minute),
		EntryTime:    base,
		EntryPrice:   9500,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
		Level:        model.PriceLevel{Price: 9000, Upper: 9000, Breakout: &model.BreakoutSetup{BuyPoint: &entry, BreakoutPoint: &entry}},
	}
	preloaded := []model.Kline{{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 9500, MarketCapHigh: 9600, MarketCapLow: 9400, MarketCapClose: 9500, Volume: 100}}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {9.8}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 1000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 1000}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected no sell signal yet, got %#v", pub.tradeSignals)
	}
	stored := store.states["token-a"]
	if stored.Status != candidateStatusBought {
		t.Fatalf("expected bought state to remain active, got %#v", stored)
	}
}

func TestCandidateMonitorSellStopsWhenMarketCapNotAboveTenK(t *testing.T) {
	base := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 10000}
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-1",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Minute),
		EntryTime:    base,
		EntryPrice:   10000,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
		Level:        model.PriceLevel{Price: 9500, Upper: 9500, Breakout: &model.BreakoutSetup{BuyPoint: &entry, BreakoutPoint: &entry}},
	}
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 10000, MarketCapHigh: 10100, MarketCapLow: 9900, MarketCapClose: 10000, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10000, MarketCapHigh: 11000, MarketCapLow: 9950, MarketCapClose: 10800, Volume: 120},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(2*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {9.0}}, now, pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 1000}
	monitor.cfg.SupplyProvider = fakeSupplyProvider{supply: 1000}
	store.states[state.TokenAddress] = state
	monitor.preloadActiveKlines(context.Background())

	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process candidate: %v", err)
	}
	if store.stopped["token-a"] != candidateStatusSold {
		t.Fatalf("expected sold state, got %#v", store.stopped)
	}
}

func TestCandidateMonitorListCandidates(t *testing.T) {
	base := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
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
		t.Fatalf("expected kline fetch time, got %#v", first.BirdeyeKlineFetchedAt)
	}
	if first.LevelMarketCap != 23000 || first.LevelLowerMarketCap != 22800 || first.LevelUpperMarketCap != 23200 {
		t.Fatalf("unexpected level fields: %#v", first)
	}
	if items[1].EntryTime != nil {
		t.Fatalf("watching candidate should not expose empty entry time: %#v", items[1].EntryTime)
	}
}

func TestCandidateMonitorAddManualCandidate(t *testing.T) {
	base := time.Date(2026, 6, 29, 14, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	item, err := monitor.AddManualCandidate(context.Background(), "manual-token")
	if err != nil {
		t.Fatalf("add manual candidate: %v", err)
	}
	if item.TokenAddress != "manual-token" || item.Status != candidateStatusWatching {
		t.Fatalf("unexpected manual candidate: %#v", item)
	}
	state, ok := store.states["manual-token"]
	if !ok {
		t.Fatalf("expected candidate to be stored")
	}
	if state.StrategyName != "manual" || state.RunID == "" || !state.CandidateAt.Equal(base) {
		t.Fatalf("unexpected stored state: %#v", state)
	}
}
