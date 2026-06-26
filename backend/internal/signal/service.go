package signal

import (
	"context"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

type Publisher interface {
	PublishRealtimeSignals(ctx context.Context, tokenAddress string, interval string, signals []backtest.RealtimeScenarioSignal) error
}

type Service struct {
	birdeye   datasource.KlineDataSource
	publisher Publisher
}

func NewService(birdeye datasource.KlineDataSource, publisher Publisher) *Service {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &Service{
		birdeye:   birdeye,
		publisher: publisher,
	}
}

// GetKlineLevels 作为信号模块的“结构识别”入口：
// 它只负责从 Birdeye K 线里识别压力/支撑结构，不掺杂回测逻辑。
func (s *Service) GetKlineLevels(ctx context.Context, req datasource.KlineQuery, options backtest.LevelOptions) (backtest.KlineLevelsResult, error) {
	klines, err := s.birdeye.GetKlines(ctx, req)
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
	klines, err := s.birdeye.GetKlines(ctx, datasource.KlineQuery{
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
