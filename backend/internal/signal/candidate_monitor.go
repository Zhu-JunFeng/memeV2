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
	"time"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

const (
	candidateStatusWatching = "watching"
	candidateStatusBought   = "bought"
	candidateStatusStopped  = "stopped"
	candidateStatusSold     = "sold"
	strategyBreakoutFollow  = "breakout_band_follow"
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
}

type CandidateMonitor struct {
	redis     *redis.Client
	birdeye   datasource.KlineDataSource
	publisher Publisher
	store     candidateMonitorStore
	cfg       CandidateMonitorConfig
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
	TokenAddress string
	Symbol       string
	RunID        string
	StrategyName string
	ScanNo       int64
	RawPayload   json.RawMessage
	CandidateAt  time.Time
	Status       string
	BuySignalID  string
	EntryTime    time.Time
	EntryPrice   float64
	Level        model.PriceLevel
}

type candidateMonitorStore interface {
	UpsertCandidate(ctx context.Context, state candidateMonitorState) error
	ListActive(ctx context.Context) ([]candidateMonitorState, error)
	SaveState(ctx context.Context, state candidateMonitorState) error
	StopCandidate(ctx context.Context, state candidateMonitorState, status string) error
	AcquireEmission(ctx context.Context, signalID string) (bool, error)
	ReleaseEmission(ctx context.Context, signalID string) error
}

func NewCandidateMonitor(redisClient *redis.Client, birdeye datasource.KlineDataSource, publisher Publisher, cfg CandidateMonitorConfig) *CandidateMonitor {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	cfg = normalizeCandidateMonitorConfig(cfg)
	return &CandidateMonitor{
		redis:     redisClient,
		birdeye:   birdeye,
		publisher: publisher,
		store:     newRedisCandidateMonitorStore(redisClient, cfg.RedisKeyPrefix),
		cfg:       cfg,
	}
}

func normalizeCandidateMonitorConfig(cfg CandidateMonitorConfig) CandidateMonitorConfig {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if strings.TrimSpace(cfg.Interval) == "" {
		cfg.Interval = "1m"
	}
	if cfg.MinMarketCap <= 0 {
		cfg.MinMarketCap = 15000
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
	return cfg
}

func (m *CandidateMonitor) Start(ctx context.Context) {
	if m == nil || !m.cfg.Enabled || m.redis == nil || m.birdeye == nil || m.publisher == nil || m.store == nil {
		return
	}
	go m.subscribeCandidates(ctx)
	go m.pollCandidates(ctx)
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
	if state.Status == candidateStatusWatching && latest.MarketCapClose < m.cfg.MinMarketCap {
		if err := m.store.StopCandidate(ctx, state, candidateStatusStopped); err != nil {
			return err
		}
		log.Printf("candidate monitor stopped low market cap: ca=%s marketCap=%.2f", state.TokenAddress, latest.MarketCapClose)
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
	interval, err := intervalDuration(m.cfg.Interval)
	if err != nil {
		return nil, err
	}
	start := state.CandidateAt.Add(-time.Duration(m.cfg.LookbackBars) * interval)
	end := time.Now().UTC().Add(interval)
	return m.birdeye.GetKlines(ctx, datasource.KlineQuery{TokenAddress: state.TokenAddress, Interval: m.cfg.Interval, StartTime: start, EndTime: end})
}

func (m *CandidateMonitor) processWatchingCandidate(ctx context.Context, state candidateMonitorState, klines []model.Kline) error {
	if len(klines) < 2 {
		return nil
	}
	history := klines[:len(klines)-1]
	current := klines[len(klines)-1]
	windowSize := m.cfg.LevelOptions.WindowSize
	if windowSize <= 0 || windowSize > len(history) {
		windowSize = len(history)
	}
	windowStep := m.cfg.LevelOptions.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	result := backtest.CalculateRealtimeScenarioSignalsByWindows(history, current, m.cfg.LevelOptions, windowSize, windowStep, backtest.PressureBreakoutDetector())
	signals := candidateSignalsAfter(result.Signals, state.CandidateAt)
	if len(signals) == 0 {
		return nil
	}
	sig := signals[0]
	message, level, err := m.buildBuySignal(state, sig)
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
	state.EntryTime = message.SignalTime
	state.EntryPrice = message.TriggerMarketCap
	state.Level = level
	if err := m.store.SaveState(ctx, state); err != nil {
		return err
	}
	log.Printf("candidate monitor published buy signal: ca=%s signalId=%s marketCap=%.2f", state.TokenAddress, message.SignalID, message.TriggerMarketCap)
	return nil
}

func candidateSignalsAfter(signals []backtest.RealtimeScenarioSignal, at time.Time) []backtest.RealtimeScenarioSignal {
	items := make([]backtest.RealtimeScenarioSignal, 0, len(signals))
	for _, signal := range signals {
		if signal.SignalTime.After(at) {
			items = append(items, signal)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SignalTime.Equal(items[j].SignalTime) {
			return items[i].LevelIndex < items[j].LevelIndex
		}
		return items[i].SignalTime.Before(items[j].SignalTime)
	})
	return items
}

func (m *CandidateMonitor) buildBuySignal(state candidateMonitorState, sig backtest.RealtimeScenarioSignal) (model.TradeSignalMessage, model.PriceLevel, error) {
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
		"source":       "candidate_monitor",
		"upstream":     json.RawMessage(state.RawPayload),
		"realtime":     sig,
		"strategyCode": strategyBreakoutFollow,
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
		SignalTime:       sig.SignalTime,
		TriggerPrice:     sig.SignalMarketCap,
		TriggerMarketCap: sig.SignalMarketCap,
		Reason:           fmt.Sprintf("候选池项目出现突破压力带买入场景: %s", sig.Reason),
		Metadata:         metadata,
	}, level, nil
}

func (m *CandidateMonitor) processBoughtCandidate(ctx context.Context, state candidateMonitorState, klines []model.Kline) error {
	entryIndex := findKlineIndex(klines, state.EntryTime)
	if entryIndex < 0 {
		return nil
	}
	decision := backtest.EvaluateRealtimeBandFollowExit(klines, entryIndex, state.Level, m.cfg.BreakoutFollow)
	if !decision.Triggered || decision.ExitPoint == nil {
		return nil
	}
	message, err := m.buildSellSignal(state, decision)
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
	if err := m.store.StopCandidate(ctx, state, candidateStatusSold); err != nil {
		return err
	}
	log.Printf("candidate monitor published sell signal: ca=%s signalId=%s reason=%s", state.TokenAddress, message.SignalID, decision.Reason)
	return nil
}

func findKlineIndex(klines []model.Kline, target time.Time) int {
	for index, item := range klines {
		if item.OpenTime.Equal(target) {
			return index
		}
	}
	return -1
}

func (m *CandidateMonitor) buildSellSignal(state candidateMonitorState, decision backtest.BandFollowExitDecision) (model.TradeSignalMessage, error) {
	metadata, err := json.Marshal(map[string]any{
		"source":       "candidate_monitor",
		"upstream":     json.RawMessage(state.RawPayload),
		"buySignalId":  state.BuySignalID,
		"outcome":      decision.Outcome,
		"holdingBars":  decision.HoldingBars,
		"profitRate":   decision.ProfitRate,
		"strategyCode": strategyBreakoutFollow,
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
		SignalTime:       decision.ExitPoint.Time,
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
		"tokenAddress": state.TokenAddress,
		"symbol":       state.Symbol,
		"runId":        state.RunID,
		"strategyName": state.StrategyName,
		"scanNo":       strconv.FormatInt(state.ScanNo, 10),
		"rawPayload":   string(state.RawPayload),
		"candidateAt":  strconv.FormatInt(state.CandidateAt.UnixMilli(), 10),
		"status":       state.Status,
		"buySignalId":  state.BuySignalID,
		"entryTime":    strconv.FormatInt(state.EntryTime.UnixMilli(), 10),
		"entryPrice":   strconv.FormatFloat(state.EntryPrice, 'f', -1, 64),
		"level":        string(levelJSON),
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
	if fields["level"] != "" {
		if err := json.Unmarshal([]byte(fields["level"]), &state.Level); err != nil {
			return candidateMonitorState{}, err
		}
	}
	return state, nil
}
