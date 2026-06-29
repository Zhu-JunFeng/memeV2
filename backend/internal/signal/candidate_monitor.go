package signal

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/eventbus"
	"solana-meme-backtest/backend/internal/model"
)

const (
	candidateStatusWatching = "watching"
	candidateStatusBought   = "bought"
	candidateStatusStopped  = "stopped"
	candidateStatusSold     = "sold"
	strategyBreakoutFollow  = "breakout_band_follow"
	monitorKlineCacheLimit  = 200
	monitorMinMarketCap     = 10000
)

type CandidateMonitorConfig struct {
	Enabled          bool
	CandidateChannel string
	PollInterval     time.Duration
	Interval         string
	MinMarketCap     float64
	LookbackBars     int
	RedisKeyPrefix   string
	LevelOptions     backtest.LevelOptions
	BreakoutFollow   backtest.BreakoutBandFollowConfig
	SupplyProvider   datasource.TokenSupplyProvider
	PriceProvider    datasource.TokenPriceProvider
	KlineSource      datasource.KlineDataSource
	SystemKlines     monitorKlineStore
	EventBus         *eventbus.Broker
	Now              func() time.Time
}

type CandidateMonitor struct {
	redis          *redis.Client
	priceProvider  datasource.TokenPriceProvider
	klineSource    datasource.KlineDataSource
	publisher      Publisher
	store          candidateMonitorStore
	cfg            CandidateMonitorConfig
	supplyProvider datasource.TokenSupplyProvider
	systemKlines   monitorKlineStore
	klineCache     *candidateKlineCache
	supplyMu       sync.RWMutex
	supplyCache    map[string]float64
	eventBus       *eventbus.Broker
}

type CandidateMonitorItem struct {
	TokenAddress          string          `json:"tokenAddress"`
	Symbol                string          `json:"symbol"`
	RunID                 string          `json:"runId"`
	StrategyName          string          `json:"strategyName"`
	ScanNo                int64           `json:"scanNo"`
	Status                string          `json:"status"`
	CandidateAt           time.Time       `json:"candidateAt"`
	BuySignalID           string          `json:"buySignalId"`
	EntryTime             *time.Time      `json:"entryTime,omitempty"`
	EntryMarketCap        float64         `json:"entryMarketCap"`
	CurrentMarketCap      *float64        `json:"currentMarketCap,omitempty"`
	CurrentMarketCapAt    *time.Time      `json:"currentMarketCapAt,omitempty"`
	BirdeyeKlineFetchedAt *time.Time      `json:"birdeyeKlineFetchedAt,omitempty"`
	LevelMarketCap        float64         `json:"levelMarketCap"`
	LevelLowerMarketCap   float64         `json:"levelLowerMarketCap"`
	LevelUpperMarketCap   float64         `json:"levelUpperMarketCap"`
	UpstreamScore         *float64        `json:"upstreamScore,omitempty"`
	UpstreamMarketCap     *float64        `json:"upstreamMarketCap,omitempty"`
	RawPayload            json.RawMessage `json:"rawPayload,omitempty"`
}

type candidateScorePassedMessage struct {
	Event          string          `json:"event"`
	RunID          string          `json:"runId"`
	StrategyName   string          `json:"strategyName"`
	ScanNo         int64           `json:"scanNo"`
	Token          string          `json:"token"`
	TokenAddress   string          `json:"tokenAddress"`
	PairAddress    string          `json:"pairAddress"`
	Score          float64         `json:"score"`
	Liquidity      float64         `json:"liquidity"`
	MarketCap      float64         `json:"marketCap"`
	SignalPrice    float64         `json:"signalPrice"`
	SignalVolumeM5 float64         `json:"signalVolumeM5"`
	BuyRatio       float64         `json:"buyRatio"`
	PriceChange5m  float64         `json:"priceChange5m"`
	Volume24h      float64         `json:"volume24h"`
	ObservedAt     int64           `json:"observedAt"`
	ExpiresAt      int64           `json:"expiresAt"`
	PublishedAt    int64           `json:"publishedAt"`
	Pullback       json.RawMessage `json:"pullback,omitempty"`
}

type candidateMonitorState struct {
	TokenAddress        string
	Symbol              string
	RunID               string
	StrategyName        string
	ScanNo              int64
	RawPayload          json.RawMessage
	CandidateAt         time.Time
	Status              string
	BuySignalID         string
	EntryTime           time.Time
	EntryPrice          float64
	CurrentPrice        float64
	CurrentAt           time.Time
	KlineFetchedAt      time.Time
	LastDecisionBarTime time.Time
	LastExitBarTime     time.Time
	Level               model.PriceLevel
}

type candidateMonitorStore interface {
	UpsertCandidate(ctx context.Context, state candidateMonitorState) error
	ListActive(ctx context.Context) ([]candidateMonitorState, error)
	SaveState(ctx context.Context, state candidateMonitorState) error
	StopCandidate(ctx context.Context, state candidateMonitorState, status string) error
	AcquireEmission(ctx context.Context, signalID string) (bool, error)
	ReleaseEmission(ctx context.Context, signalID string) error
}

type monitorKlineStore interface {
	GetKlines(ctx context.Context, req datasource.KlineQuery) ([]model.Kline, error)
	GetRecentKlines(ctx context.Context, tokenAddress string, interval string, limit int) ([]model.Kline, error)
	EnqueueUpsert(klines []model.Kline)
}

func NewCandidateMonitor(redisClient *redis.Client, priceProvider datasource.TokenPriceProvider, publisher Publisher, cfg CandidateMonitorConfig) *CandidateMonitor {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	cfg.PriceProvider = priceProvider
	if cfg.KlineSource == nil {
		if source, ok := priceProvider.(datasource.KlineDataSource); ok {
			cfg.KlineSource = source
		}
	}
	cfg = normalizeCandidateMonitorConfig(cfg)
	return &CandidateMonitor{
		redis:          redisClient,
		priceProvider:  cfg.PriceProvider,
		klineSource:    cfg.KlineSource,
		publisher:      publisher,
		store:          newRedisCandidateMonitorStore(redisClient, cfg.RedisKeyPrefix),
		cfg:            cfg,
		supplyProvider: cfg.SupplyProvider,
		systemKlines:   cfg.SystemKlines,
		klineCache:     newCandidateKlineCache(monitorKlineCacheLimit),
		supplyCache:    map[string]float64{},
		eventBus:       cfg.EventBus,
	}
}

func normalizeCandidateMonitorConfig(cfg CandidateMonitorConfig) CandidateMonitorConfig {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if strings.TrimSpace(cfg.Interval) == "" {
		cfg.Interval = "1m"
	}
	if cfg.MinMarketCap < 0 {
		cfg.MinMarketCap = 0
	}
	if cfg.LookbackBars <= 0 {
		cfg.LookbackBars = 120
	}
	if strings.TrimSpace(cfg.RedisKeyPrefix) == "" {
		cfg.RedisKeyPrefix = "solana_meme_v2:signal_monitor"
	}
	if strings.TrimSpace(cfg.CandidateChannel) == "" {
		cfg.CandidateChannel = "solana_scalper:candidate_pool"
	}
	if cfg.LevelOptions.WindowSize <= 0 {
		cfg.LevelOptions = backtest.DefaultLevelOptions()
	}
	if cfg.BreakoutFollow.TakeProfitRate <= 0 {
		cfg.BreakoutFollow = backtest.DefaultBreakoutBandFollowConfig()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return cfg
}

func (m *CandidateMonitor) Start(ctx context.Context) {
	if m == nil || !m.cfg.Enabled || m.redis == nil || m.priceProvider == nil || m.klineSource == nil || m.publisher == nil || m.store == nil {
		return
	}
	m.preloadActiveKlines(ctx)
	go m.subscribeCandidates(ctx)
	go m.pollCandidates(ctx)
}

func (m *CandidateMonitor) preloadActiveKlines(ctx context.Context) {
	if m == nil || m.systemKlines == nil || m.klineCache == nil {
		return
	}
	items, err := m.store.ListActive(ctx)
	if err != nil {
		log.Printf("candidate monitor preload active candidates failed: %v", err)
		return
	}
	for _, item := range items {
		klines, err := m.systemKlines.GetRecentKlines(ctx, item.TokenAddress, m.cfg.Interval, monitorKlineCacheLimit)
		if err != nil {
			log.Printf("candidate monitor preload klines failed: ca=%s err=%v", item.TokenAddress, err)
			continue
		}
		// 候选池监控统一使用本地维护的市值K线，并保留系统内累计出来的样本量能。
		m.klineCache.Set(item.TokenAddress, m.cfg.Interval, sanitizeMonitorKlines(klines))
	}
}

func (m *CandidateMonitor) ListCandidates(ctx context.Context) ([]CandidateMonitorItem, error) {
	if m == nil || m.store == nil {
		return []CandidateMonitorItem{}, nil
	}
	states, err := m.store.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]CandidateMonitorItem, 0, len(states))
	for _, state := range states {
		items = append(items, newCandidateMonitorItem(state))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CandidateAt.After(items[j].CandidateAt)
	})
	return items, nil
}

func (m *CandidateMonitor) AddManualCandidate(ctx context.Context, tokenAddress string) (CandidateMonitorItem, error) {
	tokenAddress = strings.TrimSpace(tokenAddress)
	if tokenAddress == "" {
		return CandidateMonitorItem{}, errors.New("CA 不能为空")
	}
	if m == nil || m.store == nil {
		return CandidateMonitorItem{}, errors.New("候选池监控未启用")
	}
	states, err := m.store.ListActive(ctx)
	if err != nil {
		return CandidateMonitorItem{}, err
	}
	for _, state := range states {
		if state.TokenAddress == tokenAddress {
			return newCandidateMonitorItem(state), nil
		}
	}
	now := m.now()
	rawPayload, err := json.Marshal(map[string]any{
		"event":        "manual_candidate_added",
		"tokenAddress": tokenAddress,
		"publishedAt":  now.UnixMilli(),
	})
	if err != nil {
		return CandidateMonitorItem{}, err
	}
	state := candidateMonitorState{
		TokenAddress: tokenAddress,
		RunID:        "manual:" + strconv.FormatInt(now.UnixMilli(), 10),
		StrategyName: "manual",
		RawPayload:   rawPayload,
		CandidateAt:  now,
		Status:       candidateStatusWatching,
	}
	if err := m.store.UpsertCandidate(ctx, state); err != nil {
		return CandidateMonitorItem{}, err
	}
	m.publishCandidateUpsert(state)
	log.Printf("candidate monitor accepted manual candidate: ca=%s", state.TokenAddress)
	return newCandidateMonitorItem(state), nil
}

func newCandidateMonitorItem(state candidateMonitorState) CandidateMonitorItem {
	var upstream struct {
		Score     *float64 `json:"score"`
		MarketCap *float64 `json:"marketCap"`
	}
	if len(state.RawPayload) > 0 {
		_ = json.Unmarshal(state.RawPayload, &upstream)
	}
	var entryTime *time.Time
	if !state.EntryTime.IsZero() {
		value := state.EntryTime
		entryTime = &value
	}
	var currentMarketCap *float64
	var currentMarketCapAt *time.Time
	if !state.CurrentAt.IsZero() {
		value := state.CurrentPrice
		currentMarketCap = &value
		at := state.CurrentAt
		currentMarketCapAt = &at
	}
	var birdeyeKlineFetchedAt *time.Time
	if !state.KlineFetchedAt.IsZero() {
		value := state.KlineFetchedAt
		birdeyeKlineFetchedAt = &value
	}
	return CandidateMonitorItem{
		TokenAddress:          state.TokenAddress,
		Symbol:                state.Symbol,
		RunID:                 state.RunID,
		StrategyName:          state.StrategyName,
		ScanNo:                state.ScanNo,
		Status:                state.Status,
		CandidateAt:           state.CandidateAt,
		BuySignalID:           state.BuySignalID,
		EntryTime:             entryTime,
		EntryMarketCap:        state.EntryPrice,
		CurrentMarketCap:      currentMarketCap,
		CurrentMarketCapAt:    currentMarketCapAt,
		BirdeyeKlineFetchedAt: birdeyeKlineFetchedAt,
		LevelMarketCap:        state.Level.Price,
		LevelLowerMarketCap:   state.Level.Lower,
		LevelUpperMarketCap:   state.Level.Upper,
		UpstreamScore:         upstream.Score,
		UpstreamMarketCap:     upstream.MarketCap,
		RawPayload:            append(json.RawMessage{}, state.RawPayload...),
	}
}

func (m *CandidateMonitor) subscribeCandidates(ctx context.Context) {
	pubsub := m.redis.Subscribe(ctx, m.cfg.CandidateChannel)
	defer pubsub.Close()
	if _, err := pubsub.Receive(ctx); err != nil {
		log.Printf("candidate monitor subscribe redis channel failed: channel=%s err=%v", m.cfg.CandidateChannel, err)
		return
	}
	log.Printf("candidate monitor subscribed redis channel: %s", m.cfg.CandidateChannel)
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := m.handleCandidatePayload(ctx, []byte(msg.Payload)); err != nil {
				log.Printf("candidate monitor handle payload failed: %v", err)
			}
		}
	}
}

func (m *CandidateMonitor) pollCandidates(ctx context.Context) {
	m.pollOnce(ctx)
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pollOnce(ctx)
		}
	}
}

func (m *CandidateMonitor) pollOnce(ctx context.Context) {
	items, err := m.store.ListActive(ctx)
	if err != nil {
		log.Printf("candidate monitor list active failed: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}
	log.Printf("candidate monitor polling active candidates: count=%d", len(items))
	for _, item := range items {
		if err := m.processCandidate(ctx, item); err != nil {
			log.Printf("candidate monitor process candidate failed: ca=%s err=%v", item.TokenAddress, err)
		}
	}
}

func (m *CandidateMonitor) handleCandidatePayload(ctx context.Context, payload []byte) error {
	candidate, err := decodeCandidateScorePassed(payload)
	if err != nil {
		return err
	}
	state := candidateMonitorState{
		TokenAddress: candidate.TokenAddress,
		Symbol:       candidate.Token,
		RunID:        candidate.RunID,
		StrategyName: candidate.StrategyName,
		ScanNo:       candidate.ScanNo,
		RawPayload:   append(json.RawMessage{}, payload...),
		CandidateAt:  time.UnixMilli(candidate.PublishedAt).UTC(),
		Status:       candidateStatusWatching,
	}
	if err := m.store.UpsertCandidate(ctx, state); err != nil {
		return err
	}
	m.publishCandidateUpsert(state)
	log.Printf("candidate monitor accepted candidate: ca=%s symbol=%s runId=%s", state.TokenAddress, state.Symbol, state.RunID)
	return nil
}

func decodeCandidateScorePassed(payload []byte) (candidateScorePassedMessage, error) {
	var envelope struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return candidateScorePassedMessage{}, err
	}
	if envelope.Event != "candidate_score_passed" {
		return candidateScorePassedMessage{}, fmt.Errorf("unsupported candidate event: %s", envelope.Event)
	}
	var candidate candidateScorePassedMessage
	if err := json.Unmarshal(payload, &candidate); err != nil {
		return candidateScorePassedMessage{}, err
	}
	if candidate.RunID == "" {
		return candidateScorePassedMessage{}, errors.New("candidate_score_passed missing runId")
	}
	if candidate.TokenAddress == "" {
		return candidateScorePassedMessage{}, errors.New("candidate_score_passed missing tokenAddress")
	}
	if candidate.PublishedAt <= 0 {
		return candidateScorePassedMessage{}, errors.New("candidate_score_passed missing publishedAt")
	}
	return candidate, nil
}

func (m *CandidateMonitor) processCandidate(ctx context.Context, state candidateMonitorState) error {
	klines, err := m.loadLatestKlines(ctx, state)
	if err != nil {
		return err
	}
	if len(klines) == 0 {
		return nil
	}
	latest := klines[len(klines)-1]
	currentMarketCap := latest.MarketCapClose
	state.CurrentPrice = currentMarketCap
	state.CurrentAt = m.now()
	state.KlineFetchedAt = m.now()
	if state.Status == candidateStatusWatching && currentMarketCap < m.minMarketCapThreshold() {
		if err := m.store.StopCandidate(ctx, state, candidateStatusStopped); err != nil {
			return err
		}
		m.publishCandidateDelete(state)
		log.Printf("candidate monitor stopped low market cap: ca=%s marketCap=%.2f", state.TokenAddress, currentMarketCap)
		return nil
	}
	switch state.Status {
	case candidateStatusWatching:
		return m.processWatchingCandidate(ctx, state, klines)
	case candidateStatusBought:
		return m.processBoughtCandidate(ctx, state, klines)
	default:
		return nil
	}
}

func (m *CandidateMonitor) loadLatestKlines(ctx context.Context, state candidateMonitorState) ([]model.Kline, error) {
	if m.klineSource == nil {
		return nil, errors.New("candidate monitor kline source not configured")
	}
	sampleAt := m.now()
	interval, err := intervalDuration(m.cfg.Interval)
	if err != nil {
		return nil, err
	}
	lookbackBars := m.cfg.LookbackBars
	if lookbackBars < monitorKlineCacheLimit {
		lookbackBars = monitorKlineCacheLimit
	}
	supply, err := m.tokenSupply(ctx, state.TokenAddress)
	if err != nil {
		return nil, err
	}
	// 首次进入缓存时补足回测所需窗口，后续轮询只增量拉最近几根 GMGN K 线。
	fetchBars := 5
	existing := m.klineCache.Get(state.TokenAddress, m.cfg.Interval)
	if len(existing) == 0 {
		fetchBars = lookbackBars
	}
	start := sampleAt.Add(-time.Duration(fetchBars) * interval)
	incoming, err := m.klineSource.GetKlines(ctx, datasource.KlineQuery{
		TokenAddress: state.TokenAddress,
		Interval:     m.cfg.Interval,
		StartTime:    start,
		EndTime:      sampleAt,
	})
	if err != nil {
		return nil, err
	}
	normalized := scalePriceKlinesToMarketCap(incoming, supply)
	merged := m.klineCache.MergePreferIncoming(state.TokenAddress, m.cfg.Interval, normalized)
	if m.systemKlines != nil && len(normalized) > 0 {
		m.systemKlines.EnqueueUpsert(normalized)
	}
	currentPrice, err := m.priceProvider.GetTokenPrice(ctx, state.TokenAddress)
	if err == nil && currentPrice > 0 {
		currentMarketCap := currentPrice * supply
		merged, _ = m.klineCache.ApplyPriceSample(state.TokenAddress, m.cfg.Interval, sampleAt, currentMarketCap)
	}
	if len(merged) == 0 {
		return nil, nil
	}
	latest := merged[len(merged)-1]
	if latest.MarketCapClose <= 0 {
		return nil, fmt.Errorf("candidate monitor invalid market cap kline: ca=%s", state.TokenAddress)
	}
	lookbackStart := sampleAt.Add(-time.Duration(lookbackBars) * interval)
	return filterKlinesAfter(merged, lookbackStart), nil
}

func scalePriceKlinesToMarketCap(klines []model.Kline, supply float64) []model.Kline {
	if supply <= 0 || len(klines) == 0 {
		return append([]model.Kline(nil), klines...)
	}
	items := make([]model.Kline, 0, len(klines))
	for _, item := range klines {
		scaled := item
		scaled.MarketCapOpen = item.Open * supply
		scaled.MarketCapHigh = item.High * supply
		scaled.MarketCapLow = item.Low * supply
		scaled.MarketCapClose = item.Close * supply
		items = append(items, scaled)
	}
	return items
}

func (m *CandidateMonitor) tokenSupply(ctx context.Context, tokenAddress string) (float64, error) {
	if m.supplyProvider == nil {
		return 0, errors.New("candidate monitor token supply provider not configured")
	}
	m.supplyMu.RLock()
	if value, ok := m.supplyCache[tokenAddress]; ok && value > 0 {
		m.supplyMu.RUnlock()
		return value, nil
	}
	m.supplyMu.RUnlock()
	supply, err := m.supplyProvider.GetTokenSupply(ctx, tokenAddress)
	if err != nil {
		return 0, err
	}
	if supply <= 0 {
		return 0, fmt.Errorf("candidate monitor token supply invalid: ca=%s supply=%.12f", tokenAddress, supply)
	}
	m.supplyMu.Lock()
	m.supplyCache[tokenAddress] = supply
	m.supplyMu.Unlock()
	return supply, nil
}

func (m *CandidateMonitor) saveState(ctx context.Context, state candidateMonitorState) error {
	if err := m.store.SaveState(ctx, state); err != nil {
		return err
	}
	m.publishCandidateUpsert(state)
	return nil
}

func (m *CandidateMonitor) publishCandidateUpsert(state candidateMonitorState) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(eventbus.TopicCandidates, eventbus.Event{Type: eventbus.EventUpsert, ID: state.TokenAddress, Data: newCandidateMonitorItem(state)})
}

func (m *CandidateMonitor) publishCandidateDelete(state candidateMonitorState) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(eventbus.TopicCandidates, eventbus.Event{Type: eventbus.EventDelete, ID: state.TokenAddress, Data: state.TokenAddress})
}

func (m *CandidateMonitor) processWatchingCandidate(ctx context.Context, state candidateMonitorState, klines []model.Kline) error {
	result, decisionBar, ok := backtest.DetectLiveBreakoutSignalsByWindows(klines, m.cfg.LevelOptions, backtest.PressureBreakoutDetector())
	if !ok {
		return m.saveState(ctx, state)
	}
	if !state.LastDecisionBarTime.IsZero() && !decisionBar.OpenTime.After(state.LastDecisionBarTime) {
		return m.saveState(ctx, state)
	}
	if !decisionBar.OpenTime.After(state.CandidateAt) && !decisionBar.CloseTime.After(state.CandidateAt) {
		state.LastDecisionBarTime = decisionBar.OpenTime
		return m.saveState(ctx, state)
	}
	signals := candidateSignalsAfter(result.Signals, state.CandidateAt, decisionBar)
	signals = candidateSignalsAfterExit(signals, state.LastExitBarTime)
	if len(signals) == 0 {
		state.LastDecisionBarTime = decisionBar.OpenTime
		return m.saveState(ctx, state)
	}
	sig := signals[0]
	message, level, err := m.buildBuySignal(state, decisionBar, sig)
	if err != nil {
		return err
	}
	acquired, err := m.store.AcquireEmission(ctx, message.SignalID)
	if err != nil || !acquired {
		return err
	}
	if err := m.publisher.PublishTradeSignal(ctx, message); err != nil {
		_ = m.store.ReleaseEmission(ctx, message.SignalID)
		return err
	}
	state.Status = candidateStatusBought
	state.BuySignalID = message.SignalID
	state.EntryTime = decisionBar.OpenTime
	state.EntryPrice = message.TriggerMarketCap
	state.LastDecisionBarTime = decisionBar.OpenTime
	state.Level = level
	if err := m.saveState(ctx, state); err != nil {
		return err
	}
	log.Printf("candidate monitor published buy signal: ca=%s signalId=%s marketCap=%.2f", state.TokenAddress, message.SignalID, message.TriggerMarketCap)
	return nil
}

func candidateSignalsAfter(signals []backtest.RealtimeScenarioSignal, at time.Time, decisionBar model.Kline) []backtest.RealtimeScenarioSignal {
	if !decisionBar.CloseTime.After(at) {
		return nil
	}
	items := make([]backtest.RealtimeScenarioSignal, 0, len(signals))
	for _, signal := range signals {
		items = append(items, signal)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SignalTime.Equal(items[j].SignalTime) {
			return items[i].LevelIndex < items[j].LevelIndex
		}
		return items[i].SignalTime.Before(items[j].SignalTime)
	})
	return items
}

func candidateSignalsAfterExit(signals []backtest.RealtimeScenarioSignal, lastExitBarTime time.Time) []backtest.RealtimeScenarioSignal {
	if lastExitBarTime.IsZero() {
		return signals
	}
	items := make([]backtest.RealtimeScenarioSignal, 0, len(signals))
	for _, signal := range signals {
		if signal.SignalTime.After(lastExitBarTime) {
			items = append(items, signal)
		}
	}
	return items
}

func (m *CandidateMonitor) buildBuySignal(state candidateMonitorState, decisionBar model.Kline, sig backtest.RealtimeScenarioSignal) (model.TradeSignalMessage, model.PriceLevel, error) {
	if sig.Breakout == nil || sig.Breakout.BuyPoint == nil {
		return model.TradeSignalMessage{}, model.PriceLevel{}, errors.New("breakout signal missing buy point")
	}
	level := model.PriceLevel{
		Type:        sig.LevelType,
		Price:       sig.LevelMarketCap,
		Lower:       sig.LevelLowerMarketCap,
		Upper:       sig.LevelUpperMarketCap,
		Calculation: sig.Calculation,
		Breakout:    sig.Breakout,
	}
	metadata, err := json.Marshal(map[string]any{
		"source":          "candidate_monitor",
		"upstream":        json.RawMessage(state.RawPayload),
		"realtime":        sig,
		"strategyCode":    strategyBreakoutFollow,
		"decisionBarTime": decisionBar.OpenTime,
		"entryBarTime":    sig.SignalTime,
		"volumeMode":      "gmgn_volume",
	})
	if err != nil {
		return model.TradeSignalMessage{}, model.PriceLevel{}, err
	}
	signalID := compactSignalID("bbf:buy", state.RunID, strconv.FormatInt(state.ScanNo, 10), state.TokenAddress, strconv.FormatInt(sig.SignalTime.UnixMilli(), 10))
	return model.TradeSignalMessage{
		SignalID:         signalID,
		SignalType:       model.TradeSignalTypeBuy,
		StrategyCode:     strategyBreakoutFollow,
		TokenAddress:     state.TokenAddress,
		Interval:         m.cfg.Interval,
		SignalTime:       m.now(),
		TriggerPrice:     sig.SignalMarketCap,
		TriggerMarketCap: sig.SignalMarketCap,
		Reason:           fmt.Sprintf("候选池项目出现突破压力带买入场景: %s", sig.Reason),
		Metadata:         metadata,
	}, level, nil
}

func (m *CandidateMonitor) processBoughtCandidate(ctx context.Context, state candidateMonitorState, klines []model.Kline) error {
	decision, decisionBar, ok := backtest.EvaluateLiveBandFollowExit(klines, state.EntryTime, state.Level, m.cfg.BreakoutFollow)
	if !ok {
		return m.saveState(ctx, state)
	}
	if !state.LastDecisionBarTime.IsZero() && !decisionBar.OpenTime.After(state.LastDecisionBarTime) {
		return m.saveState(ctx, state)
	}
	state.LastDecisionBarTime = decisionBar.OpenTime
	if !decision.Triggered || decision.ExitPoint == nil {
		return m.saveState(ctx, state)
	}
	message, err := m.buildSellSignal(state, decisionBar, decision)
	if err != nil {
		return err
	}
	acquired, err := m.store.AcquireEmission(ctx, message.SignalID)
	if err != nil || !acquired {
		return err
	}
	if err := m.publisher.PublishTradeSignal(ctx, message); err != nil {
		_ = m.store.ReleaseEmission(ctx, message.SignalID)
		return err
	}
	latest := klines[len(klines)-1]
	state.LastExitBarTime = decisionBar.OpenTime
	if latest.MarketCapClose > m.minMarketCapThreshold() {
		state.Status = candidateStatusWatching
		state.BuySignalID = ""
		state.EntryTime = time.Time{}
		state.EntryPrice = 0
		state.Level = model.PriceLevel{}
		state.CurrentPrice = latest.MarketCapClose
		state.CurrentAt = m.now()
		state.KlineFetchedAt = m.now()
		if err := m.saveState(ctx, state); err != nil {
			return err
		}
		log.Printf("candidate monitor rearmed after sell: ca=%s marketCap=%.2f", state.TokenAddress, latest.MarketCapClose)
		return nil
	}
	if err := m.store.StopCandidate(ctx, state, candidateStatusSold); err != nil {
		return err
	}
	m.publishCandidateDelete(state)
	log.Printf("candidate monitor published sell signal: ca=%s signalId=%s reason=%s", state.TokenAddress, message.SignalID, decision.Reason)
	return nil
}

func (m *CandidateMonitor) buildSellSignal(state candidateMonitorState, decisionBar model.Kline, decision backtest.BandFollowExitDecision) (model.TradeSignalMessage, error) {
	metadata, err := json.Marshal(map[string]any{
		"source":          "candidate_monitor",
		"upstream":        json.RawMessage(state.RawPayload),
		"buySignalId":     state.BuySignalID,
		"outcome":         decision.Outcome,
		"holdingBars":     decision.HoldingBars,
		"profitRate":      decision.ProfitRate,
		"strategyCode":    strategyBreakoutFollow,
		"entryBarTime":    state.EntryTime,
		"exitBarTime":     decisionBar.OpenTime,
		"decisionBarTime": decisionBar.OpenTime,
		"volumeMode":      "gmgn_volume",
	})
	if err != nil {
		return model.TradeSignalMessage{}, err
	}
	signalID := compactSignalID("bbf:sell", state.BuySignalID, state.TokenAddress, strconv.FormatInt(decision.ExitPoint.Time.UnixMilli(), 10))
	return model.TradeSignalMessage{
		SignalID:         signalID,
		SignalType:       model.TradeSignalTypeSell,
		StrategyCode:     strategyBreakoutFollow,
		TokenAddress:     state.TokenAddress,
		Interval:         m.cfg.Interval,
		SignalTime:       m.now(),
		TriggerPrice:     decision.ExitPoint.Price,
		TriggerMarketCap: decision.ExitPoint.Price,
		Reason:           decision.Reason,
		Metadata:         metadata,
	}, nil
}

func compactSignalID(prefix string, parts ...string) string {
	joined := strings.Join(parts, "|")
	sum := sha1.Sum([]byte(joined))
	return prefix + ":" + hex.EncodeToString(sum[:8])
}

func intervalDuration(interval string) (time.Duration, error) {
	switch strings.TrimSpace(interval) {
	case "1m":
		return time.Minute, nil
	case "3m":
		return 3 * time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported signal interval: %s", interval)
	}
}

func sanitizeMonitorKlines(klines []model.Kline) []model.Kline {
	items := make([]model.Kline, 0, len(klines))
	for _, item := range klines {
		if item.OpenTime.IsZero() {
			continue
		}
		openValue := preferMarketCap(item.MarketCapOpen, item.Open)
		highValue := preferMarketCap(item.MarketCapHigh, item.High)
		lowValue := preferMarketCap(item.MarketCapLow, item.Low)
		closeValue := preferMarketCap(item.MarketCapClose, item.Close)
		if closeValue <= 0 {
			continue
		}
		current := item
		current.Open = openValue
		current.High = highValue
		current.Low = lowValue
		current.Close = closeValue
		current.MarketCapOpen = openValue
		current.MarketCapHigh = highValue
		current.MarketCapLow = lowValue
		current.MarketCapClose = closeValue
		if current.Volume < 0 {
			current.Volume = 0
		}
		items = append(items, current)
	}
	return items
}

func preferMarketCap(marketCap float64, fallback float64) float64 {
	if marketCap > 0 {
		return marketCap
	}
	return fallback
}

func (m *CandidateMonitor) minMarketCapThreshold() float64 {
	if m != nil && m.cfg.MinMarketCap > 0 {
		return m.cfg.MinMarketCap
	}
	return monitorMinMarketCap
}

func (m *CandidateMonitor) now() time.Time {
	if m == nil || m.cfg.Now == nil {
		return time.Now().UTC()
	}
	now := m.cfg.Now()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

type redisCandidateMonitorStore struct {
	client *redis.Client
	prefix string
}

func newRedisCandidateMonitorStore(client *redis.Client, prefix string) *redisCandidateMonitorStore {
	return &redisCandidateMonitorStore{client: client, prefix: strings.TrimRight(prefix, ":")}
}

func (s *redisCandidateMonitorStore) activeKey() string {
	return s.prefix + ":active"
}

func (s *redisCandidateMonitorStore) candidateKey(tokenAddress string) string {
	return s.prefix + ":candidate:" + tokenAddress
}

func (s *redisCandidateMonitorStore) emittedKey(signalID string) string {
	return s.prefix + ":emitted:" + signalID
}

func (s *redisCandidateMonitorStore) UpsertCandidate(ctx context.Context, state candidateMonitorState) error {
	if s == nil || s.client == nil {
		return errors.New("candidate monitor redis store not configured")
	}
	active, err := s.client.SIsMember(ctx, s.activeKey(), state.TokenAddress).Result()
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	if err := s.client.SAdd(ctx, s.activeKey(), state.TokenAddress).Err(); err != nil {
		return err
	}
	return s.SaveState(ctx, state)
}

func (s *redisCandidateMonitorStore) ListActive(ctx context.Context) ([]candidateMonitorState, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("candidate monitor redis store not configured")
	}
	tokens, err := s.client.SMembers(ctx, s.activeKey()).Result()
	if err != nil {
		return nil, err
	}
	items := make([]candidateMonitorState, 0, len(tokens))
	for _, token := range tokens {
		fields, err := s.client.HGetAll(ctx, s.candidateKey(token)).Result()
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 {
			_ = s.client.SRem(ctx, s.activeKey(), token).Err()
			continue
		}
		state, err := decodeCandidateState(fields)
		if err != nil {
			log.Printf("candidate monitor decode redis state failed: ca=%s err=%v", token, err)
			continue
		}
		items = append(items, state)
	}
	return items, nil
}

func (s *redisCandidateMonitorStore) SaveState(ctx context.Context, state candidateMonitorState) error {
	fields, err := encodeCandidateState(state)
	if err != nil {
		return err
	}
	return s.client.HSet(ctx, s.candidateKey(state.TokenAddress), fields).Err()
}

func (s *redisCandidateMonitorStore) StopCandidate(ctx context.Context, state candidateMonitorState, status string) error {
	state.Status = status
	if err := s.SaveState(ctx, state); err != nil {
		return err
	}
	return s.client.SRem(ctx, s.activeKey(), state.TokenAddress).Err()
}

func (s *redisCandidateMonitorStore) AcquireEmission(ctx context.Context, signalID string) (bool, error) {
	return s.client.SetNX(ctx, s.emittedKey(signalID), strconv.FormatInt(time.Now().UnixMilli(), 10), 0).Result()
}

func (s *redisCandidateMonitorStore) ReleaseEmission(ctx context.Context, signalID string) error {
	return s.client.Del(ctx, s.emittedKey(signalID)).Err()
}

func encodeCandidateState(state candidateMonitorState) (map[string]any, error) {
	levelJSON, err := json.Marshal(state.Level)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"tokenAddress":        state.TokenAddress,
		"symbol":              state.Symbol,
		"runId":               state.RunID,
		"strategyName":        state.StrategyName,
		"scanNo":              strconv.FormatInt(state.ScanNo, 10),
		"rawPayload":          string(state.RawPayload),
		"candidateAt":         strconv.FormatInt(state.CandidateAt.UnixMilli(), 10),
		"status":              state.Status,
		"buySignalId":         state.BuySignalID,
		"entryTime":           strconv.FormatInt(state.EntryTime.UnixMilli(), 10),
		"entryPrice":          strconv.FormatFloat(state.EntryPrice, 'f', -1, 64),
		"currentPrice":        strconv.FormatFloat(state.CurrentPrice, 'f', -1, 64),
		"currentAt":           strconv.FormatInt(state.CurrentAt.UnixMilli(), 10),
		"klineFetchedAt":      strconv.FormatInt(state.KlineFetchedAt.UnixMilli(), 10),
		"lastDecisionBarTime": strconv.FormatInt(state.LastDecisionBarTime.UnixMilli(), 10),
		"lastExitBarTime":     strconv.FormatInt(state.LastExitBarTime.UnixMilli(), 10),
		"level":               string(levelJSON),
	}, nil
}

func decodeCandidateState(fields map[string]string) (candidateMonitorState, error) {
	state := candidateMonitorState{
		TokenAddress: fields["tokenAddress"],
		Symbol:       fields["symbol"],
		RunID:        fields["runId"],
		StrategyName: fields["strategyName"],
		RawPayload:   json.RawMessage(fields["rawPayload"]),
		Status:       fields["status"],
		BuySignalID:  fields["buySignalId"],
	}
	if state.TokenAddress == "" {
		return candidateMonitorState{}, errors.New("missing tokenAddress")
	}
	if state.Status == "" {
		return candidateMonitorState{}, errors.New("missing status")
	}
	if fields["scanNo"] != "" {
		value, err := strconv.ParseInt(fields["scanNo"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		state.ScanNo = value
	}
	if fields["candidateAt"] != "" {
		value, err := strconv.ParseInt(fields["candidateAt"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		state.CandidateAt = time.UnixMilli(value).UTC()
	}
	if fields["entryTime"] != "" {
		value, err := strconv.ParseInt(fields["entryTime"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		if value > 0 {
			state.EntryTime = time.UnixMilli(value).UTC()
		}
	}
	if fields["entryPrice"] != "" {
		value, err := strconv.ParseFloat(fields["entryPrice"], 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		state.EntryPrice = value
	}
	if fields["currentPrice"] != "" {
		value, err := strconv.ParseFloat(fields["currentPrice"], 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		state.CurrentPrice = value
	}
	if fields["currentAt"] != "" {
		value, err := strconv.ParseInt(fields["currentAt"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		if value > 0 {
			state.CurrentAt = time.UnixMilli(value).UTC()
		}
	}
	if fields["klineFetchedAt"] != "" {
		value, err := strconv.ParseInt(fields["klineFetchedAt"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		if value > 0 {
			state.KlineFetchedAt = time.UnixMilli(value).UTC()
		}
	}
	if fields["lastDecisionBarTime"] != "" {
		value, err := strconv.ParseInt(fields["lastDecisionBarTime"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		if value > 0 {
			state.LastDecisionBarTime = time.UnixMilli(value).UTC()
		}
	}
	if fields["lastExitBarTime"] != "" {
		value, err := strconv.ParseInt(fields["lastExitBarTime"], 10, 64)
		if err != nil {
			return candidateMonitorState{}, err
		}
		if value > 0 {
			state.LastExitBarTime = time.UnixMilli(value).UTC()
		}
	}
	if fields["level"] != "" {
		if err := json.Unmarshal([]byte(fields["level"]), &state.Level); err != nil {
			return candidateMonitorState{}, err
		}
	}
	return state, nil
}
