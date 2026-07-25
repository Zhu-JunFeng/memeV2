package signal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/eventbus"
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

type failingSupplyProvider struct {
	calls int
}

func (p *failingSupplyProvider) GetTokenSupply(context.Context, string) (float64, error) {
	p.calls++
	return 0, errors.New("unexpected supply request")
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

type fakeTradeSignalStatusProvider struct {
	signals        map[string]model.TradeSignal
	signalErrors   map[string]error
	positions      map[string]model.TradePosition
	positionErrors map[string]error
}

type fakeRuntimePositionProvider struct {
	position model.TradePosition
	found    bool
	err      error
}

func (p fakeRuntimePositionProvider) FindRuntimePosition(context.Context, string) (model.TradePosition, bool, error) {
	return p.position, p.found, p.err
}

func (p fakeTradeSignalStatusProvider) GetSignalBySignalID(_ context.Context, signalID string) (model.TradeSignal, error) {
	if err := p.signalErrors[signalID]; err != nil {
		return model.TradeSignal{}, err
	}
	item, ok := p.signals[signalID]
	if !ok {
		return model.TradeSignal{}, errors.New("signal not found")
	}
	return item, nil
}

func (p fakeTradeSignalStatusProvider) GetOpenPositionBySignalID(_ context.Context, signalID string) (model.TradePosition, error) {
	if err := p.positionErrors[signalID]; err != nil {
		return model.TradePosition{}, err
	}
	item, ok := p.positions[signalID]
	if !ok {
		return model.TradePosition{}, errors.New("position not found")
	}
	return item, nil
}

func TestCandidateMonitorRearmsClosedPositionWithoutSameBarReentry(t *testing.T) {
	base := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 20000, MarketCapHigh: 20400, MarketCapLow: 19800, MarketCapClose: 20200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 20200, MarketCapHigh: 20600, MarketCapLow: 20000, MarketCapClose: 20400},
	}
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		EntryTime: base, EntryPrice: 20000, EntryPriceSynced: true, Level: model.PriceLevel{Price: 19600},
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, klines, map[string][]float64{"token-a": {20400}}, base.Add(2*time.Minute+30*time.Second), pub)
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signals:        map[string]model.TradeSignal{"buy-1": {SignalID: "buy-1", ConsumeStatus: "executed"}},
		positionErrors: map[string]error{"buy-1": sql.ErrNoRows},
	}
	monitor.preloadActiveKlines(context.Background())

	if err := monitor.processCandidate(context.Background(), state); err != nil {
		t.Fatalf("process closed position candidate: %v", err)
	}
	stored := store.states[state.TokenAddress]
	if stored.Status != candidateStatusWatching || stored.BuySignalID != "" || !stored.EntryTime.IsZero() || stored.EntryPrice != 0 || stored.EntryPriceSynced {
		t.Fatalf("expected closed position to rearm watching state, got %#v", stored)
	}
	latestBarTime := monitor.now().UTC().Truncate(time.Minute)
	if !stored.LastExitBarTime.Equal(latestBarTime) || !stored.LastDecisionBarTime.Equal(latestBarTime) {
		t.Fatalf("expected latest bar to block same-bar reentry, got exit=%s decision=%s", stored.LastExitBarTime, stored.LastDecisionBarTime)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected no signal while rearming closed position, got %#v", pub.tradeSignals)
	}
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
	if last.Volume != 500 {
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
	pub.beforeTrade = func(message model.TradeSignalMessage) {
		persisted := store.states[state.TokenAddress]
		if persisted.Status != candidateStatusBought || persisted.BuySignalID != message.SignalID || persisted.BuySignalAt.IsZero() {
			t.Fatalf("buy state must be durable before publishing the signal, got %#v", persisted)
		}
	}
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
	if signal.TriggerMarketCap != 21900 {
		t.Fatalf("expected buy signal to use realtime market cap, got %.2f", signal.TriggerMarketCap)
	}
	var payload map[string]any
	if err := json.Unmarshal(signal.Metadata, &payload); err != nil {
		t.Fatalf("unmarshal signal metadata: %v", err)
	}
	snapshot, ok := payload["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("expected snapshot in signal metadata, got %#v", payload)
	}
	if len(snapshot["chartKlines"].([]any)) == 0 {
		t.Fatalf("expected snapshot chart klines, got %#v", snapshot)
	}
	stored := store.states["token-a"]
	if stored.Status != candidateStatusBought || stored.BuySignalID != signal.SignalID || stored.BuySignalAt.IsZero() || stored.Level.Breakout == nil {
		t.Fatalf("expected bought state with level, got %#v", stored)
	}
	if stored.EntryPrice != 21900 || stored.Level.Breakout.BuyPoint == nil || stored.Level.Breakout.BuyPoint.Price != 21900 {
		t.Fatalf("expected bought state to use realtime entry market cap, got %#v", stored)
	}
	if len(monitor.systemKlines.(*fakeMonitorKlineStore).enqueued) == 0 {
		t.Fatalf("expected latest synthetic kline to be enqueued")
	}
}

func TestCandidateMonitorSkipsBuyFromOlderSlidingWindow(t *testing.T) {
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	}
	for minute := 6; minute < 14; minute++ {
		klines = append(klines, model.Kline{
			TokenAddress:   "token-a",
			Interval:       "1m",
			OpenTime:       base.Add(time.Duration(minute) * time.Minute),
			CloseTime:      base.Add(time.Duration(minute+1) * time.Minute),
			MarketCapOpen:  8.6,
			MarketCapHigh:  8.9,
			MarketCapLow:   8.2,
			MarketCapClose: 8.5,
			Volume:         90,
		})
	}
	klines = append(klines, model.Kline{
		TokenAddress:   "token-a",
		Interval:       "1m",
		OpenTime:       base.Add(14 * time.Minute),
		CloseTime:      base.Add(15 * time.Minute),
		MarketCapOpen:  8.5,
		MarketCapHigh:  11.2,
		MarketCapLow:   8.4,
		MarketCapClose: 10.95,
		Volume:         320,
	})
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, nil, base.Add(15*time.Minute), pub)
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-old-window",
		ScanNo:       8,
		CandidateAt:  base,
		Status:       candidateStatusWatching,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
	}
	store.states[state.TokenAddress] = state

	if err := monitor.processWatchingCandidate(context.Background(), state, klines); err != nil {
		t.Fatalf("process watching candidate: %v", err)
	}
	if len(pub.tradeSignals) != 0 {
		t.Fatalf("expected older-window breakout to be ignored, got %#v", pub.tradeSignals)
	}
	if store.states["token-a"].Status != candidateStatusWatching {
		t.Fatalf("expected candidate to keep watching, got %#v", store.states["token-a"])
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
	entry := model.LevelAnchorPoint{Time: base, Price: 20000}
	state := candidateMonitorState{
		TokenAddress: "token-a",
		RunID:        "run-1",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Minute),
		EntryTime:    base,
		EntryPrice:   20000,
		RawPayload:   json.RawMessage(`{"event":"candidate_score_passed"}`),
		Level:        model.PriceLevel{Price: 19600, Upper: 19600, Breakout: &model.BreakoutSetup{BuyPoint: &entry, BreakoutPoint: &entry}},
	}
	preloaded := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 20000, MarketCapHigh: 20200, MarketCapLow: 19800, MarketCapClose: 20000, Volume: 100},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 20000, MarketCapHigh: 22000, MarketCapLow: 19900, MarketCapClose: 21600, Volume: 120},
	}
	store := newFakeCandidateStore()
	pub := &capturePublisher{}
	now := base.Add(2*time.Minute + 30*time.Second)
	monitor := testCandidateMonitor(store, preloaded, map[string][]float64{"token-a": {21.8}}, now, pub)
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
	if signal.TriggerMarketCap != 21800 {
		t.Fatalf("expected sell signal to use realtime market cap, got %.2f", signal.TriggerMarketCap)
	}
	var payload map[string]any
	if err := json.Unmarshal(signal.Metadata, &payload); err != nil {
		t.Fatalf("unmarshal sell metadata: %v", err)
	}
	exitPoint := payload["exitPoint"].(map[string]any)
	if exitPoint["marketCap"].(float64) != 21800 {
		t.Fatalf("expected realtime exit point, got %#v", exitPoint)
	}
	strategyExitPoint := payload["strategyExitPoint"].(map[string]any)
	if strategyExitPoint["marketCap"].(float64) != 21600 {
		t.Fatalf("expected strategy exit point to use the default 8%% take profit, got %#v", strategyExitPoint)
	}
	stored := store.states["token-a"]
	if stored.Status != candidateStatusBought || stored.SellSignalID != signal.SignalID {
		t.Fatalf("expected bought state to wait for sell execution, got %#v", stored)
	}
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signals: map[string]model.TradeSignal{
			stored.BuySignalID:  {SignalID: stored.BuySignalID, ConsumeStatus: "executed"},
			stored.SellSignalID: {SignalID: stored.SellSignalID, ConsumeStatus: "executed"},
		},
		positionErrors: map[string]error{stored.BuySignalID: sql.ErrNoRows},
	}
	if err := monitor.processBoughtCandidate(context.Background(), stored, preloaded); err != nil {
		t.Fatal(err)
	}
	stored = store.states["token-a"]
	if stored.Status != candidateStatusWatching || stored.BuySignalID != "" || stored.SellSignalID != "" || !stored.EntryTime.IsZero() || stored.EntryPrice != 0 {
		t.Fatalf("expected confirmed sell to rearm watching state, got %#v", stored)
	}
}

func TestCandidateMonitorRechecksSellWithinSameMinuteBar(t *testing.T) {
	base := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base.Add(-time.Minute), Price: 10000}
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		CandidateAt: base.Add(-2 * time.Minute), EntryTime: base.Add(-time.Minute), EntryPrice: 10000,
		LastDecisionBarTime: base,
		Level:               model.PriceLevel{Lower: 9000, Breakout: &model.BreakoutSetup{BuyPoint: &entry}},
	}
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(-time.Minute), CloseTime: base, MarketCapOpen: 10000, MarketCapHigh: 10100, MarketCapLow: 9900, MarketCapClose: 10000},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 10000, MarketCapHigh: 11100, MarketCapLow: 9950, MarketCapClose: 11000},
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, nil, base.Add(30*time.Second), pub)

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	if len(pub.tradeSignals) != 1 || pub.tradeSignals[0].SignalType != model.TradeSignalTypeSell {
		t.Fatalf("expected same-minute recheck to publish sell, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorSyncsExecutedEntryMarketCapBeforeSellEvaluation(t *testing.T) {
	base := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 100}
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-executed",
		CandidateAt: base.Add(-time.Minute), EntryTime: base, EntryPrice: 100,
		Level: model.PriceLevel{Lower: 90, Breakout: &model.BreakoutSetup{BuyPoint: &entry}},
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	monitor := testCandidateMonitor(store, nil, nil, base.Add(30*time.Second), &capturePublisher{})
	monitor.supplyProvider = fakeSupplyProvider{supply: 1000}
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signals: map[string]model.TradeSignal{
			state.BuySignalID: {SignalID: state.BuySignalID, ConsumeStatus: "executed"},
		},
		positions: map[string]model.TradePosition{
			state.BuySignalID: {TokenAddress: state.TokenAddress, Status: model.TradePositionStatusOpen, AvgCostPrice: 0.105},
		},
	}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 100, MarketCapHigh: 106, MarketCapLow: 99, MarketCapClose: 105}}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if !stored.EntryPriceSynced || stored.EntryPrice != 105 {
		t.Fatalf("expected actual executed market cap 105, got %#v", stored)
	}
	if stored.Level.Breakout == nil || stored.Level.Breakout.BuyPoint == nil || stored.Level.Breakout.BuyPoint.Price != 105 {
		t.Fatalf("expected exit strategy entry point to use executed market cap, got %#v", stored.Level)
	}
}

func TestCandidateMonitorRecoversMissingBuySignalFromRuntimePosition(t *testing.T) {
	base := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base, Price: 100}
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "missing-buy",
		EntryTime: base, EntryPrice: 100,
		Level: model.PriceLevel{Lower: 90, Breakout: &model.BreakoutSetup{BuyPoint: &entry}},
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, nil, base.Add(2*time.Minute+30*time.Second), pub)
	monitor.supplyProvider = fakeSupplyProvider{supply: 1000}
	monitor.signalStatus = fakeTradeSignalStatusProvider{signalErrors: map[string]error{state.BuySignalID: sql.ErrNoRows}}
	monitor.runtimePosition = fakeRuntimePositionProvider{
		found: true,
		position: model.TradePosition{
			TokenAddress: state.TokenAddress, Status: model.TradePositionStatusOpen,
			AvgCostPrice: 0.105, OpenedAt: base.Add(10 * time.Second),
		},
	}
	klines := []model.Kline{
		{TokenAddress: state.TokenAddress, Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 105, MarketCapHigh: 106, MarketCapLow: 100, MarketCapClose: 103},
		{TokenAddress: state.TokenAddress, Interval: "1m", OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 103, MarketCapHigh: 104, MarketCapLow: 79, MarketCapClose: 80},
	}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if !stored.EntryPriceSynced || stored.EntryPrice != 105 || !stored.EntryTime.Equal(base) {
		t.Fatalf("expected runtime position to restore executed entry, got %#v", stored)
	}
	if len(pub.tradeSignals) != 1 || pub.tradeSignals[0].SignalType != model.TradeSignalTypeSell {
		t.Fatalf("expected recovered position to continue sell evaluation, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorRearmsMissingBuySignalAfterAckTimeout(t *testing.T) {
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "missing-buy",
		BuySignalAt: base, EntryTime: base, EntryPrice: 20000,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	store.emitted[state.BuySignalID] = true
	monitor := testCandidateMonitor(store, nil, nil, base.Add(buySignalAckTimeout), &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{signalErrors: map[string]error{state.BuySignalID: sql.ErrNoRows}}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base.Add(time.Minute), MarketCapClose: 21000}}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if stored.Status != candidateStatusWatching || stored.BuySignalID != "" || !stored.BuySignalAt.IsZero() || !stored.EntryTime.IsZero() {
		t.Fatalf("expected missing buy signal to rearm after timeout, got %#v", stored)
	}
	if store.emitted[state.BuySignalID] {
		t.Fatal("expected missing buy emission lock to be released")
	}
}

func TestCandidateMonitorKeepsRecentMissingBuySignalPending(t *testing.T) {
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "missing-buy",
		BuySignalAt: base, EntryTime: base, EntryPrice: 20000,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	store.emitted[state.BuySignalID] = true
	monitor := testCandidateMonitor(store, nil, nil, base.Add(buySignalAckTimeout-time.Millisecond), &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{signalErrors: map[string]error{state.BuySignalID: sql.ErrNoRows}}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base.Add(time.Minute), MarketCapClose: 21000}}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if stored.Status != candidateStatusBought || stored.BuySignalID != state.BuySignalID || !stored.BuySignalAt.Equal(base) {
		t.Fatalf("expected recent buy signal to remain pending, got %#v", stored)
	}
	if !store.emitted[state.BuySignalID] {
		t.Fatal("expected recent buy emission lock to remain held")
	}
}

func TestCandidateMonitorRearmsLegacyMissingBuySignalWithoutTimestamp(t *testing.T) {
	base := time.Date(2026, 7, 19, 10, 30, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-legacy", Status: candidateStatusBought, BuySignalID: "missing-legacy-buy",
		EntryTime: base.Add(-time.Hour), EntryPrice: 20000,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	store.emitted[state.BuySignalID] = true
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{signalErrors: map[string]error{state.BuySignalID: sql.ErrNoRows}}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base, MarketCapClose: 21000}}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	if stored := store.states[state.TokenAddress]; stored.Status != candidateStatusWatching || stored.BuySignalID != "" {
		t.Fatalf("expected legacy missing buy signal to rearm immediately, got %#v", stored)
	}
}

func TestCandidateMonitorStopsClosedPositionBelowMarketCapThreshold(t *testing.T) {
	base := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		EntryTime: base, EntryPrice: 12000, EntryPriceSynced: true,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	monitor := testCandidateMonitor(store, nil, nil, base.Add(time.Minute), &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signals:        map[string]model.TradeSignal{state.BuySignalID: {SignalID: state.BuySignalID, ConsumeStatus: "executed"}},
		positionErrors: map[string]error{state.BuySignalID: sql.ErrNoRows},
	}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base, MarketCapClose: monitorMinMarketCap - 1}}

	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	if store.stopped[state.TokenAddress] != candidateStatusSold {
		t.Fatalf("expected closed low-market-cap candidate to stop, got %#v", store.stopped)
	}
}

func TestCandidateMonitorRearmsClosedPositionAtMarketCapThreshold(t *testing.T) {
	base := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		EntryTime: base, EntryPrice: 16000, EntryPriceSynced: true,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	monitor := testCandidateMonitor(store, nil, nil, base.Add(time.Minute), &capturePublisher{})
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base, MarketCapClose: monitorMinMarketCap}}

	if err := monitor.rearmAfterClosedPosition(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	if stored := store.states[state.TokenAddress]; stored.Status != candidateStatusWatching {
		t.Fatalf("expected threshold candidate to rearm, got %#v", stored)
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
	pending := store.states["token-a"]
	if pending.Status != candidateStatusBought || pending.SellSignalID == "" {
		t.Fatalf("expected sell to remain pending before execution confirmation, got %#v", pending)
	}
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signals: map[string]model.TradeSignal{
			pending.BuySignalID:  {SignalID: pending.BuySignalID, ConsumeStatus: "executed"},
			pending.SellSignalID: {SignalID: pending.SellSignalID, ConsumeStatus: "executed"},
		},
		positionErrors: map[string]error{pending.BuySignalID: sql.ErrNoRows},
	}
	confirmedKlines := append([]model.Kline(nil), preloaded...)
	confirmedKlines = append(confirmedKlines, model.Kline{
		TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute),
		MarketCapOpen: 9000, MarketCapHigh: 9000, MarketCapLow: 9000, MarketCapClose: 9000,
	})
	if err := monitor.processBoughtCandidate(context.Background(), pending, confirmedKlines); err != nil {
		t.Fatal(err)
	}
	if store.stopped["token-a"] != candidateStatusSold {
		t.Fatalf("expected sold state, got %#v", store.stopped)
	}
}

func TestCandidateMonitorRetriesFailedSellWithoutDroppingBoughtState(t *testing.T) {
	base := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	entry := model.LevelAnchorPoint{Time: base.Add(-time.Minute), Price: 10000}
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		SellSignalID: "sell-1", SellSignalAt: base.Add(-time.Second),
		CandidateAt: base.Add(-2 * time.Minute), EntryTime: base.Add(-time.Minute), EntryPrice: 10000, EntryPriceSynced: true,
		Level: model.PriceLevel{Lower: 9000, Breakout: &model.BreakoutSetup{BuyPoint: &entry}},
	}
	klines := []model.Kline{
		{
			TokenAddress: "token-a", Interval: "1m", OpenTime: base.Add(-time.Minute), CloseTime: base,
			MarketCapOpen: 10000, MarketCapHigh: 10100, MarketCapLow: 9900, MarketCapClose: 10000,
		},
		{
			TokenAddress: "token-a", Interval: "1m", OpenTime: base, CloseTime: base.Add(time.Minute),
			MarketCapOpen: 10000, MarketCapHigh: 11100, MarketCapLow: 9950, MarketCapClose: 11000,
		},
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	store.emitted[state.SellSignalID] = true
	pub := &capturePublisher{}
	monitor := testCandidateMonitor(store, nil, nil, base.Add(30*time.Second), pub)
	monitor.signalStatus = fakeTradeSignalStatusProvider{signals: map[string]model.TradeSignal{
		state.BuySignalID:  {SignalID: state.BuySignalID, ConsumeStatus: "executed"},
		state.SellSignalID: {SignalID: state.SellSignalID, ConsumeStatus: "failed"},
	}}
	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if stored.Status != candidateStatusBought || stored.BuySignalID != state.BuySignalID || stored.SellSignalID != "" {
		t.Fatalf("expected failed sell to keep bought state and clear pending sell, got %#v", stored)
	}
	if store.emitted[state.SellSignalID] {
		t.Fatal("expected failed sell emission lock to be released")
	}
	monitor.signalStatus = fakeTradeSignalStatusProvider{signals: map[string]model.TradeSignal{
		state.BuySignalID: {SignalID: state.BuySignalID, ConsumeStatus: "executed"},
	}, positions: map[string]model.TradePosition{
		state.BuySignalID: {TokenAddress: state.TokenAddress, Status: model.TradePositionStatusOpen, AvgCostPrice: 10},
	}}
	if err := monitor.processBoughtCandidate(context.Background(), stored, klines); err != nil {
		t.Fatal(err)
	}
	if len(pub.tradeSignals) != 1 || pub.tradeSignals[0].SignalID == state.SellSignalID {
		t.Fatalf("expected retry to publish a new sell signal id, got %#v", pub.tradeSignals)
	}
}

func TestCandidateMonitorRetriesSellWhenPublishedSignalWasNotConsumed(t *testing.T) {
	base := time.Date(2026, 7, 15, 3, 30, 0, 0, time.UTC)
	state := candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought, BuySignalID: "buy-1",
		SellSignalID: "sell-missing", SellSignalAt: base.Add(-sellSignalAckTimeout),
		EntryTime: base.Add(-time.Minute), EntryPrice: 10000,
	}
	store := newFakeCandidateStore()
	store.states[state.TokenAddress] = state
	store.emitted[state.SellSignalID] = true
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{
		signalErrors: map[string]error{state.SellSignalID: sql.ErrNoRows},
	}
	klines := []model.Kline{{TokenAddress: state.TokenAddress, OpenTime: base, MarketCapClose: 11000}}
	if err := monitor.processBoughtCandidate(context.Background(), state, klines); err != nil {
		t.Fatal(err)
	}
	stored := store.states[state.TokenAddress]
	if stored.Status != candidateStatusBought || stored.SellSignalID != "" || !stored.SellSignalAt.IsZero() {
		t.Fatalf("expected missing sell signal to be cleared for retry, got %#v", stored)
	}
	if store.emitted[state.SellSignalID] {
		t.Fatal("expected missing sell signal emission lock to be released")
	}
}

func TestCandidateMonitorStateRoundTripsPendingSellFields(t *testing.T) {
	base := time.Date(2026, 7, 15, 4, 0, 0, 0, time.UTC)
	fields, err := encodeCandidateState(candidateMonitorState{
		TokenAddress: "token-a", Status: candidateStatusBought,
		BuySignalID: "buy-1", BuySignalAt: base, SellSignalID: "sell-1", SellSignalAt: base, SellAttempt: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := make(map[string]string, len(fields))
	for key, value := range fields {
		encoded[key] = fmt.Sprint(value)
	}
	state, err := decodeCandidateState(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if state.BuySignalAt.IsZero() || !state.BuySignalAt.Equal(base) || state.SellSignalID != "sell-1" || !state.SellSignalAt.Equal(base) || state.SellAttempt != 2 {
		t.Fatalf("pending sell fields did not round trip: %#v", state)
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
	if first.TokenAddress != "token-old" {
		t.Fatalf("expected oldest candidate first, got %#v", first)
	}
	second := items[1]
	if second.TokenAddress != "token-new" || second.UpstreamScore == nil || *second.UpstreamScore != 91.2 || second.UpstreamMarketCap == nil || *second.UpstreamMarketCap != 26000 {
		t.Fatalf("unexpected first item: %#v", first)
	}
	if second.EntryTime == nil || !second.EntryTime.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("expected entry time, got %#v", second.EntryTime)
	}
	if second.CurrentMarketCap == nil || *second.CurrentMarketCap != 25500 {
		t.Fatalf("expected current market cap, got %#v", second.CurrentMarketCap)
	}
	if second.CurrentMarketCapAt == nil || !second.CurrentMarketCapAt.Equal(base.Add(3*time.Minute)) {
		t.Fatalf("expected current market cap time, got %#v", second.CurrentMarketCapAt)
	}
	if second.BirdeyeKlineFetchedAt == nil || !second.BirdeyeKlineFetchedAt.Equal(base.Add(3*time.Minute+10*time.Second)) {
		t.Fatalf("expected kline fetch time, got %#v", second.BirdeyeKlineFetchedAt)
	}
	if second.LevelMarketCap != 23000 || second.LevelLowerMarketCap != 22800 || second.LevelUpperMarketCap != 23200 {
		t.Fatalf("unexpected level fields: %#v", second)
	}
	if items[0].EntryTime != nil {
		t.Fatalf("watching candidate should not expose empty entry time: %#v", items[0].EntryTime)
	}
}

func TestCandidateMonitorDeleteCandidate(t *testing.T) {
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	bus := eventbus.NewBroker()
	events, cancel := bus.Subscribe(eventbus.TopicCandidates)
	defer cancel()
	monitor.eventBus = bus
	store.states["token-a"] = candidateMonitorState{
		TokenAddress: "token-a",
		Symbol:       "TOK",
		CandidateAt:  base,
		Status:       candidateStatusWatching,
	}

	item, err := monitor.DeleteCandidate(context.Background(), "token-a")
	if err != nil {
		t.Fatalf("delete candidate: %v", err)
	}
	if item.TokenAddress != "token-a" {
		t.Fatalf("unexpected deleted item: %#v", item)
	}
	if _, ok := store.states["token-a"]; ok {
		t.Fatalf("expected candidate to be removed from active set")
	}
	if store.stopped["token-a"] != candidateStatusStopped {
		t.Fatalf("expected stopped status, got %#v", store.stopped)
	}
	select {
	case event := <-events:
		if event.Type != eventbus.EventDelete || event.ID != "token-a" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected candidate delete event")
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

func TestCandidateMonitorRejectsNewCandidateWhenRuntimeDisabled(t *testing.T) {
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	monitor.cfg.RuntimeEnabled = func() bool { return false }

	if _, err := monitor.AddManualCandidate(context.Background(), "manual-token"); !errors.Is(err, ErrCandidateMonitoringDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if len(store.states) != 0 {
		t.Fatalf("disabled monitor should not persist candidates: %#v", store.states)
	}
}

type fakeCABlacklistStore struct {
	states map[string]model.CABlacklistState
}

func (s *fakeCABlacklistStore) GetCABlacklistState(_ context.Context, tokenAddress string) (model.CABlacklistState, error) {
	return s.states[tokenAddress], nil
}

func (s *fakeCABlacklistStore) BlacklistCA(_ context.Context, tokenAddress string, reason string, source string) (model.CABlacklistState, error) {
	item := s.states[tokenAddress]
	item.TokenAddress = tokenAddress
	item.IsBlacklisted = true
	item.BlacklistReason = reason
	item.BlacklistSource = source
	s.states[tokenAddress] = item
	return item, nil
}

func TestCandidateMonitorRejectsBlacklistedCandidateAndManualBlacklistStopsMonitoring(t *testing.T) {
	store := newFakeCandidateStore()
	manualCA := "So11111111111111111111111111111111111111112"
	store.states[manualCA] = candidateMonitorState{TokenAddress: manualCA, Status: candidateStatusWatching}
	blacklist := &fakeCABlacklistStore{states: map[string]model.CABlacklistState{
		"already-blacklisted": {TokenAddress: "already-blacklisted", IsBlacklisted: true},
	}}
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	monitor.caBlacklist = blacklist

	if _, _, err := monitor.AddProjectCandidate(context.Background(), "already-blacklisted", "BLOCKED", 20000); !errors.Is(err, ErrCandidateBlacklisted) {
		t.Fatalf("expected blacklisted error, got %v", err)
	}
	if _, err := monitor.BlacklistCandidate(context.Background(), manualCA, "手动拉黑"); err != nil {
		t.Fatalf("manual blacklist: %v", err)
	}
	if _, ok := store.states[manualCA]; ok {
		t.Fatal("manually blacklisted CA must be removed from active candidates")
	}
	if !blacklist.states[manualCA].IsBlacklisted || blacklist.states[manualCA].BlacklistSource != "manual" {
		t.Fatalf("unexpected blacklist state: %+v", blacklist.states[manualCA])
	}
}

func TestCandidateMonitorRejectsInvalidManualBlacklistCA(t *testing.T) {
	monitor := testCandidateMonitor(newFakeCandidateStore(), nil, nil, time.Now().UTC(), &capturePublisher{})
	monitor.caBlacklist = &fakeCABlacklistStore{states: map[string]model.CABlacklistState{}}
	if _, err := monitor.BlacklistCandidate(context.Background(), "not-a-ca", "手动拉黑"); err == nil || !strings.Contains(err.Error(), "格式不合法") {
		t.Fatalf("expected invalid CA error, got %v", err)
	}
}

func TestCandidateMonitorProjectCandidateStoresSymbolAndMarketCap(t *testing.T) {
	base := time.Date(2026, 6, 29, 14, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	item, added, err := monitor.AddProjectCandidate(context.Background(), "project-token", "TOKEN", 75000)
	if err != nil || !added {
		t.Fatalf("add project candidate: added=%v err=%v", added, err)
	}
	if item.Symbol != "TOKEN" || item.CurrentMarketCap == nil || *item.CurrentMarketCap != 75000 {
		t.Fatalf("unexpected project candidate: %#v", item)
	}
}

func TestCandidateMonitorPreloadRestoresSupplyCacheFromPersistedKlines(t *testing.T) {
	base := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	store := newFakeCandidateStore()
	store.states["token-a"] = candidateMonitorState{
		TokenAddress: "token-a",
		Status:       candidateStatusBought,
		CandidateAt:  base.Add(-time.Minute),
	}
	systemStore := newFakeMonitorKlineStore()
	systemStore.recent["token-a|1m"] = []model.Kline{{
		TokenAddress:   "token-a",
		Interval:       "1m",
		OpenTime:       base.Add(-time.Minute),
		CloseTime:      base,
		Close:          0.00002,
		MarketCapClose: 20000,
	}}
	provider := &failingSupplyProvider{}
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	monitor.systemKlines = systemStore
	monitor.supplyProvider = provider

	monitor.preloadActiveKlines(context.Background())
	supply, err := monitor.tokenSupply(context.Background(), "token-a")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(supply-1_000_000_000) > 0.001 {
		t.Fatalf("expected restored supply 1,000,000,000, got %.2f", supply)
	}
	if provider.calls != 0 {
		t.Fatalf("expected no Solana RPC supply request, got %d", provider.calls)
	}
}

func TestCandidateMonitorRejectsNewCandidateWhenPoolIsFull(t *testing.T) {
	store := newFakeCandidateStore()
	for index := 0; index < candidatePoolLimit; index++ {
		address := fmt.Sprintf("token-%02d", index)
		store.states[address] = candidateMonitorState{TokenAddress: address, Status: candidateStatusWatching, CandidateAt: time.Now().UTC(), CurrentPrice: 100000}
	}
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	if _, _, err := monitor.AddProjectCandidate(context.Background(), "overflow", "OVER", 100000); !errors.Is(err, ErrCandidatePoolFull) {
		t.Fatalf("expected pool full error, got %v", err)
	}
}

func TestCandidateMonitorReplacesLowerPriorityWatchingCandidateWhenPoolIsFull(t *testing.T) {
	store := newFakeCandidateStore()
	base := time.Now().UTC()
	store.states["lower-priority"] = candidateMonitorState{
		TokenAddress: "lower-priority", Status: candidateStatusWatching,
		CandidateAt: base.Add(-time.Hour), CurrentPrice: 250000,
	}
	store.states["bought"] = candidateMonitorState{
		TokenAddress: "bought", Status: candidateStatusBought, BuySignalID: "buy-1",
		CandidateAt: base.Add(-time.Hour), CurrentPrice: 5000,
	}
	for index := 0; index < candidatePoolLimit-2; index++ {
		address := fmt.Sprintf("preferred-%02d", index)
		store.states[address] = candidateMonitorState{
			TokenAddress: address, Status: candidateStatusWatching,
			CandidateAt: base.Add(time.Duration(index) * time.Second), CurrentPrice: 100000,
		}
	}
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	item, added, err := monitor.AddProjectCandidate(context.Background(), "new-preferred", "NEW", 120000)
	if err != nil || !added {
		t.Fatalf("expected preferred candidate replacement, added=%v err=%v", added, err)
	}
	if item.TokenAddress != "new-preferred" {
		t.Fatalf("unexpected added candidate: %#v", item)
	}
	if _, ok := store.states["lower-priority"]; ok {
		t.Fatal("expected lower priority watching candidate to be replaced")
	}
	if _, ok := store.states["bought"]; !ok {
		t.Fatal("expected bought candidate to remain in pool")
	}
	if len(store.states) != candidatePoolLimit {
		t.Fatalf("expected pool size %d, got %d", candidatePoolLimit, len(store.states))
	}
}

func TestCandidateMonitorTrimPrefersMarketCapBand(t *testing.T) {
	store := newFakeCandidateStore()
	base := time.Now().UTC()
	for index := 0; index < candidatePoolLimit+2; index++ {
		address := fmt.Sprintf("token-%02d", index)
		marketCap := float64(250000 + index)
		if index == candidatePoolLimit || index == candidatePoolLimit+1 {
			marketCap = 100000
		}
		store.states[address] = candidateMonitorState{TokenAddress: address, Status: candidateStatusWatching, CandidateAt: base.Add(time.Duration(index) * time.Second), CurrentPrice: marketCap}
	}
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	if err := monitor.TrimCandidatePool(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.states) != candidatePoolLimit {
		t.Fatalf("expected %d candidates, got %d", candidatePoolLimit, len(store.states))
	}
	for _, address := range []string{"token-50", "token-51"} {
		if _, ok := store.states[address]; !ok {
			t.Fatalf("expected preferred candidate %s to be retained", address)
		}
	}
}

func TestCandidateMonitorTrimRemovesKnownMarketCapBelowThreshold(t *testing.T) {
	store := newFakeCandidateStore()
	store.states["low"] = candidateMonitorState{TokenAddress: "low", Status: candidateStatusWatching, CurrentPrice: 14999}
	store.states["kept"] = candidateMonitorState{TokenAddress: "kept", Status: candidateStatusWatching, CurrentPrice: 15000}
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	if err := monitor.TrimCandidatePool(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.states["low"]; ok {
		t.Fatal("expected low market cap candidate to be removed")
	}
	if _, ok := store.states["kept"]; !ok {
		t.Fatal("expected threshold candidate to be retained")
	}
}

func TestCandidateMonitorRemovalThresholdCannotBeConfiguredBelow15K(t *testing.T) {
	monitor := &CandidateMonitor{cfg: CandidateMonitorConfig{MinMarketCap: 10000}}
	if got := monitor.minMarketCapThreshold(); got != 15000 {
		t.Fatalf("expected 15k minimum removal threshold, got %.2f", got)
	}
	monitor.cfg.MinMarketCap = 20000
	if got := monitor.minMarketCapThreshold(); got != 20000 {
		t.Fatalf("expected higher configured threshold, got %.2f", got)
	}
}

func TestCandidateMonitorTrimRetainsBoughtCandidateBelowThreshold(t *testing.T) {
	store := newFakeCandidateStore()
	store.states["bought-low"] = candidateMonitorState{
		TokenAddress: "bought-low",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-low",
		CurrentPrice: 9999,
	}
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	if err := monitor.TrimCandidatePool(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.states["bought-low"]; !ok {
		t.Fatal("expected low market cap bought candidate to remain monitored")
	}
}

func TestCandidateMonitorTrimNeverRemovesBoughtCandidateWhenPoolIsOverLimit(t *testing.T) {
	store := newFakeCandidateStore()
	base := time.Now().UTC()
	store.states["bought"] = candidateMonitorState{
		TokenAddress: "bought",
		Status:       candidateStatusBought,
		BuySignalID:  "buy-1",
		CandidateAt:  base.Add(-time.Hour),
		CurrentPrice: 5000,
	}
	for index := 0; index < candidatePoolLimit; index++ {
		address := fmt.Sprintf("watching-%02d", index)
		store.states[address] = candidateMonitorState{
			TokenAddress: address,
			Status:       candidateStatusWatching,
			CandidateAt:  base.Add(time.Duration(index) * time.Second),
			CurrentPrice: 100000,
		}
	}
	monitor := testCandidateMonitor(store, nil, nil, base, &capturePublisher{})
	if err := monitor.TrimCandidatePool(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.states["bought"]; !ok {
		t.Fatal("expected bought candidate to survive over-limit trimming")
	}
	if len(store.states) != candidatePoolLimit {
		t.Fatalf("expected %d total candidates, got %d", candidatePoolLimit, len(store.states))
	}
}

func TestCandidateMonitorUpdatesMissingSymbol(t *testing.T) {
	store := newFakeCandidateStore()
	store.states["token"] = candidateMonitorState{TokenAddress: "token", Status: candidateStatusWatching}
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	missing, err := monitor.MissingSymbolCandidates(context.Background())
	if err != nil || len(missing) != 1 || missing[0] != "token" {
		t.Fatalf("unexpected missing symbols: %#v err=%v", missing, err)
	}
	if err := monitor.UpdateCandidateSymbol(context.Background(), "token", "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if store.states["token"].Symbol != "TOKEN" {
		t.Fatalf("symbol was not updated: %#v", store.states["token"])
	}
}

func TestCandidateMonitorRearmsAfterBuyRejection(t *testing.T) {
	store := newFakeCandidateStore()
	state := candidateMonitorState{
		TokenAddress: "token-rejected", Status: candidateStatusBought, BuySignalID: "buy-rejected",
		EntryTime: time.Now().UTC(), EntryPrice: 100, Level: model.PriceLevel{Price: 90},
	}
	store.states[state.TokenAddress] = state
	monitor := testCandidateMonitor(store, nil, nil, time.Now().UTC(), &capturePublisher{})
	monitor.signalStatus = fakeTradeSignalStatusProvider{signals: map[string]model.TradeSignal{
		state.BuySignalID: {SignalID: state.BuySignalID, ConsumeStatus: "rejected"},
	}}
	if err := monitor.processBoughtCandidate(context.Background(), state, nil); err != nil {
		t.Fatal(err)
	}
	got := store.states[state.TokenAddress]
	if got.Status != candidateStatusWatching || got.BuySignalID != "" || !got.EntryTime.IsZero() || got.EntryPrice != 0 || got.Level.Price != 0 {
		t.Fatalf("expected rejected buy to rearm candidate, got %#v", got)
	}
}
