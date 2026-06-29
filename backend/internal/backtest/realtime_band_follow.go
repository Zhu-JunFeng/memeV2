package backtest

import (
	"time"

	"solana-meme-backtest/backend/internal/model"
)

// ClosedKlinesAt 返回在指定时刻之前已经完整收盘的 K 线，
// 实时监控只对这些已确认 bar 做判定，避免把未收盘 bar 当成回测输入。
func ClosedKlinesAt(klines []model.Kline, now time.Time) []model.Kline {
	if len(klines) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	items := make([]model.Kline, 0, len(klines))
	for _, item := range klines {
		if item.CloseTime.IsZero() || item.CloseTime.After(now) {
			continue
		}
		items = append(items, item)
	}
	return items
}

// DetectClosedBarBreakoutSignalsByWindows 把候选池实时监控和回测的 bar 语义对齐：
// 只允许用“最后一根已收盘 bar”去判定突破是否成立。
func DetectClosedBarBreakoutSignalsByWindows(klines []model.Kline, now time.Time, options LevelOptions, detector ScenarioDetector) (RealtimeSignalResult, model.Kline, bool) {
	closed := ClosedKlinesAt(klines, now)
	if len(closed) < 2 {
		return RealtimeSignalResult{}, model.Kline{}, false
	}
	decisionBar := closed[len(closed)-1]
	history := closed[:len(closed)-1]
	windowSize := options.WindowSize
	if windowSize <= 0 || windowSize > len(history) {
		windowSize = len(history)
	}
	windowStep := options.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	return CalculateRealtimeScenarioSignalsByWindows(history, decisionBar, options, windowSize, windowStep, detector), decisionBar, true
}

// DetectLiveBreakoutSignalsByWindows 直接把最后一根正在形成的 bar 视为当前实时 bar，
// 让候选池在轮询拿到最新价格后立即判定是否已经突破。
func DetectLiveBreakoutSignalsByWindows(klines []model.Kline, options LevelOptions, detector ScenarioDetector) (RealtimeSignalResult, model.Kline, bool) {
	return detectLiveBreakoutSignalsByWindowsVariant(klines, options, detector, true)
}

func detectLiveBreakoutSignalsByWindowsVariant(klines []model.Kline, options LevelOptions, detector ScenarioDetector, latestWindowOnly bool) (RealtimeSignalResult, model.Kline, bool) {
	if len(klines) < 2 {
		return RealtimeSignalResult{}, model.Kline{}, false
	}
	current := klines[len(klines)-1]
	history := klines[:len(klines)-1]
	windowSize := options.WindowSize
	if windowSize <= 0 || windowSize > len(history) {
		windowSize = len(history)
	}
	windowStep := options.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	if latestWindowOnly {
		return CalculateRealtimeScenarioSignalsByWindows(history, current, options, windowSize, windowStep, detector), current, true
	}
	return CalculateReplayScenarioSignalsByWindows(history, current, options, windowSize, windowStep, detector), current, true
}

// EvaluateClosedBarBandFollowExit 只基于已收盘 K 线复用回测退出规则，
// 保证“下一根跌破上沿止损 / 硬止损 / 动态止损 / 止盈”在实时监控和回测里一致。
func EvaluateClosedBarBandFollowExit(klines []model.Kline, now time.Time, entryTime time.Time, level model.PriceLevel, config BreakoutBandFollowConfig) (BandFollowExitDecision, model.Kline, bool) {
	closed := ClosedKlinesAt(klines, now)
	if len(closed) == 0 {
		return BandFollowExitDecision{}, model.Kline{}, false
	}
	entryIndex := -1
	for index, item := range closed {
		if item.OpenTime.Equal(entryTime) {
			entryIndex = index
			break
		}
	}
	if entryIndex < 0 {
		return BandFollowExitDecision{}, model.Kline{}, false
	}
	return EvaluateRealtimeBandFollowExit(closed, entryIndex, level, config), closed[len(closed)-1], true
}

// EvaluateLiveBandFollowExit 用“已确认历史 + 当前实时 bar”执行实时卖出判定，
// 一旦当前轮询价格已经触发止盈/止损，就在本轮直接发出卖出信号。
func EvaluateLiveBandFollowExit(klines []model.Kline, entryBarTime time.Time, level model.PriceLevel, config BreakoutBandFollowConfig) (BandFollowExitDecision, model.Kline, bool) {
	if len(klines) == 0 {
		return BandFollowExitDecision{}, model.Kline{}, false
	}
	entryIndex := -1
	for index, item := range klines {
		if item.OpenTime.Equal(entryBarTime) {
			entryIndex = index
			break
		}
	}
	if entryIndex < 0 {
		return BandFollowExitDecision{}, model.Kline{}, false
	}
	return EvaluateRealtimeBandFollowExit(klines, entryIndex, level, config), klines[len(klines)-1], true
}
