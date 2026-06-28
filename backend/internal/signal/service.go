package signal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

type Publisher interface {
	PublishRealtimeSignals(ctx context.Context, tokenAddress string, interval string, signals []backtest.RealtimeScenarioSignal) error
	PublishTradeSignal(ctx context.Context, message model.TradeSignalMessage) error
}

type Service struct {
	klines        datasource.KlineDataSource
	namedSources  map[string]datasource.KlineDataSource
	defaultSource string
	publisher     Publisher
}

type ServiceOption func(*Service)

func NewService(klines datasource.KlineDataSource, publisher Publisher, options ...ServiceOption) *Service {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	svc := &Service{
		klines:       klines,
		namedSources: map[string]datasource.KlineDataSource{},
		publisher:    publisher,
	}
	for _, option := range options {
		option(svc)
	}
	return svc
}

func WithKlineSource(name string, source datasource.KlineDataSource) ServiceOption {
	return func(s *Service) {
		name = normalizeSourceName(name)
		if name == "" || source == nil {
			return
		}
		s.namedSources[name] = source
	}
}

func WithDefaultKlineSource(name string) ServiceOption {
	return func(s *Service) {
		s.defaultSource = normalizeSourceName(name)
	}
}

// GetKlineLevels 作为信号模块的“结构识别”入口：
// 它只负责从当前 K 线数据源里识别压力/支撑结构，不掺杂回测逻辑。
func (s *Service) GetKlineLevels(ctx context.Context, req datasource.KlineQuery, options backtest.LevelOptions) (backtest.KlineLevelsResult, error) {
	return s.GetKlineLevelsFromSource(ctx, "", req, options)
}

func (s *Service) GetKlineLevelsFromSource(ctx context.Context, source string, req datasource.KlineQuery, options backtest.LevelOptions) (backtest.KlineLevelsResult, error) {
	klineSource, err := s.source(source)
	if err != nil {
		return backtest.KlineLevelsResult{}, err
	}
	klines, err := klineSource.GetKlines(ctx, req)
	if err != nil {
		return backtest.KlineLevelsResult{}, err
	}
	if len(klines) == 0 {
		return backtest.KlineLevelsResult{}, backtest.ErrNoKlines
	}
	windowSize := options.WindowSize
	if windowSize <= 0 || windowSize > len(klines) {
		windowSize = len(klines)
	}
	windowStep := options.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	return backtest.KlineLevelsResult{
		Klines:     klines,
		Windows:    backtest.CalculateSupportResistanceByWindows(klines, options, windowSize, windowStep),
		WindowSize: windowSize,
		WindowStep: windowStep,
	}, nil
}

// DetectRealtimeSignals 用历史 K 线 + 当前实时 K 线做结构突破判断，
// 如果命中信号则同步写入 Redis，供外部订阅系统消费。
func (s *Service) DetectRealtimeSignals(ctx context.Context, req RealtimeRequest) (backtest.RealtimeSignalResult, error) {
	return s.DetectRealtimeSignalsFromSource(ctx, "", req)
}

func (s *Service) DetectRealtimeSignalsFromSource(ctx context.Context, source string, req RealtimeRequest) (backtest.RealtimeSignalResult, error) {
	klineSource, err := s.source(source)
	if err != nil {
		return backtest.RealtimeSignalResult{}, err
	}
	klines, err := klineSource.GetKlines(ctx, datasource.KlineQuery{
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
	})
	if err != nil {
		return backtest.RealtimeSignalResult{}, err
	}
	if len(klines) == 0 {
		return backtest.RealtimeSignalResult{}, backtest.ErrNoKlines
	}
	current := klines[len(klines)-1]
	history := klines
	if req.CurrentKline != nil {
		current = *req.CurrentKline
		history = mergeHistoryWithRealtimeKline(klines, current)
		if len(history) == 0 {
			return backtest.RealtimeSignalResult{}, backtest.ErrNoKlines
		}
		history = history[:len(history)-1]
	}
	windowSize := req.LevelOptions.WindowSize
	if windowSize <= 0 || windowSize > len(history) {
		windowSize = len(history)
	}
	windowStep := req.LevelOptions.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	result := backtest.CalculateRealtimeScenarioSignalsByWindows(history, current, req.LevelOptions, windowSize, windowStep, backtest.PressureBreakoutDetector())
	if len(result.Signals) > 0 {
		if err := s.publisher.PublishRealtimeSignals(ctx, req.TokenAddress, req.Interval, result.Signals); err != nil {
			return backtest.RealtimeSignalResult{}, err
		}
	}
	return result, nil
}

func (s *Service) source(name string) (datasource.KlineDataSource, error) {
	name = normalizeSourceName(name)
	if name == "" {
		name = s.defaultSource
	}
	if source, ok := s.namedSources[name]; ok && source != nil {
		return source, nil
	}
	if name == "" {
		return s.klines, nil
	}
	return nil, fmt.Errorf("%w: %s", datasource.ErrUnsupportedKlineSource, name)
}

func normalizeSourceName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type RealtimeRequest struct {
	TokenAddress string
	Interval     string
	StartTime    time.Time
	EndTime      time.Time
	LevelOptions backtest.LevelOptions
	CurrentKline *model.Kline
}

// 这里保留成 package 内部工具，避免把“实时 K 线合并”细节泄露给调用方。
func mergeHistoryWithRealtimeKline(history []model.Kline, current model.Kline) []model.Kline {
	if len(history) == 0 {
		return []model.Kline{current}
	}
	merged := append([]model.Kline{}, history...)
	last := merged[len(merged)-1]
	switch {
	case current.OpenTime.Equal(last.OpenTime):
		merged[len(merged)-1] = current
	case current.OpenTime.After(last.OpenTime):
		merged = append(merged, current)
	}
	return merged
}

type noopPublisher struct{}

func (noopPublisher) PublishRealtimeSignals(context.Context, string, string, []backtest.RealtimeScenarioSignal) error {
	return nil
}

func (noopPublisher) PublishTradeSignal(context.Context, model.TradeSignalMessage) error {
	return nil
}
