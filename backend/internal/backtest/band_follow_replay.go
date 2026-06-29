package backtest

import (
	"sort"
	"strconv"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

type BandFollowReplayEntry struct {
	EntryIndex   int
	WindowKey    string
	Window       WindowLevelResult
	Signal       RealtimeScenarioSignal
	Level        model.PriceLevel
	GlobalWindow int
}

// DetectBandFollowEntryAtCurrentBar 用回测同源的实时突破判定，计算“当前最后一根K线”是否构成买点。
// 这个函数是实时监控与历史回放的共同入口，避免两边再维护两套买入逻辑。
func DetectBandFollowEntryAtCurrentBar(klines []model.Kline, options LevelOptions) (BandFollowReplayEntry, bool) {
	result, decisionBar, ok := DetectLiveBreakoutSignalsByWindows(klines, options, PressureBreakoutDetector())
	if !ok || len(result.Windows) == 0 || len(result.Signals) == 0 {
		return BandFollowReplayEntry{}, false
	}
	signals := sortRealtimeSignals(result.Signals)
	if len(signals) == 0 {
		return BandFollowReplayEntry{}, false
	}
	signal := signals[0]
	if !signal.SignalTime.Equal(decisionBar.OpenTime) {
		return BandFollowReplayEntry{}, false
	}
	windowIndex := signal.WindowIndex - 1
	if windowIndex < 0 || windowIndex >= len(result.Windows) {
		return BandFollowReplayEntry{}, false
	}
	window := cloneWindowLevelResult(result.Windows[windowIndex])
	level := priceLevelFromSignal(signal)
	return BandFollowReplayEntry{
		EntryIndex: len(klines) - 1,
		WindowKey:  replayWindowKey(window),
		Window:     window,
		Signal:     signal,
		Level:      level,
	}, true
}

// CollectBandFollowReplayEntries 逐根K线回放统一的实时买入判定，
// 生成后续回测与实时监控都能复用的候选买点列表。
func CollectBandFollowReplayEntries(klines []model.Kline, options LevelOptions) ([]BandFollowReplayEntry, []WindowLevelResult) {
	if len(klines) < 2 {
		return nil, nil
	}
	entries := make([]BandFollowReplayEntry, 0)
	windows := make([]WindowLevelResult, 0)
	windowIndexByKey := map[string]int{}
	for index := 1; index < len(klines); index++ {
		entry, ok := DetectBandFollowEntryAtCurrentBar(klines[:index+1], options)
		if !ok {
			continue
		}
		if existing, exists := windowIndexByKey[entry.WindowKey]; exists {
			entry.GlobalWindow = existing
			windows[existing-1] = mergeReplayWindow(windows[existing-1], entry.Window, entry.Level)
		} else {
			entry.GlobalWindow = len(windows) + 1
			entry.Window.WindowIndex = entry.GlobalWindow
			entry.Window.Levels = mergeWindowLevels(entry.Window.Levels, entry.Level)
			windows = append(windows, entry.Window)
			windowIndexByKey[entry.WindowKey] = entry.GlobalWindow
		}
		entry.Window.WindowIndex = entry.GlobalWindow
		entries = append(entries, entry)
	}
	return entries, windows
}

func sortRealtimeSignals(signals []RealtimeScenarioSignal) []RealtimeScenarioSignal {
	items := append([]RealtimeScenarioSignal(nil), signals...)
	sort.Slice(items, func(i, j int) bool {
		if !items[i].SignalTime.Equal(items[j].SignalTime) {
			return items[i].SignalTime.Before(items[j].SignalTime)
		}
		if items[i].WindowIndex != items[j].WindowIndex {
			return items[i].WindowIndex < items[j].WindowIndex
		}
		if items[i].LevelIndex != items[j].LevelIndex {
			return items[i].LevelIndex < items[j].LevelIndex
		}
		return items[i].LevelMarketCap < items[j].LevelMarketCap
	})
	return items
}

func priceLevelFromSignal(signal RealtimeScenarioSignal) model.PriceLevel {
	return model.PriceLevel{
		Type:        signal.LevelType,
		Price:       signal.LevelMarketCap,
		Lower:       signal.LevelLowerMarketCap,
		Upper:       signal.LevelUpperMarketCap,
		Calculation: signal.Calculation,
		Breakout:    signal.Breakout,
	}
}

func replayWindowKey(window WindowLevelResult) string {
	return window.StartTime.Format(time.RFC3339Nano) + "|" + window.EndTime.Format(time.RFC3339Nano) + "|" + strconv.Itoa(window.KlineCount)
}

func cloneWindowLevelResult(window WindowLevelResult) WindowLevelResult {
	cloned := window
	cloned.Levels = clonePriceLevels(window.Levels)
	return cloned
}

func cloneWindowLevelResults(windows []WindowLevelResult) []WindowLevelResult {
	items := make([]WindowLevelResult, 0, len(windows))
	for _, window := range windows {
		items = append(items, cloneWindowLevelResult(window))
	}
	return items
}

func mergeReplayWindow(existing WindowLevelResult, incoming WindowLevelResult, level model.PriceLevel) WindowLevelResult {
	existing.StartTime = minTime(existing.StartTime, incoming.StartTime)
	existing.EndTime = maxTime(existing.EndTime, incoming.EndTime)
	if incoming.KlineCount > existing.KlineCount {
		existing.KlineCount = incoming.KlineCount
	}
	existing.Levels = mergeWindowLevels(existing.Levels, level)
	return existing
}

func mergeWindowLevels(existing []model.PriceLevel, level model.PriceLevel) []model.PriceLevel {
	if len(existing) == 0 {
		return []model.PriceLevel{level}
	}
	key := levelSelectionKey(level)
	items := make([]model.PriceLevel, 0, len(existing)+1)
	replaced := false
	for _, item := range existing {
		if levelSelectionKey(item) == key {
			if level.Score > item.Score {
				items = append(items, level)
			} else {
				items = append(items, item)
			}
			replaced = true
			continue
		}
		items = append(items, item)
	}
	if !replaced {
		items = append(items, level)
	}
	return selectTopLevelsKeepingBreakout(items, DefaultLevelOptions().MaxLevels)
}

func minTime(left time.Time, right time.Time) time.Time {
	if left.IsZero() || (!right.IsZero() && right.Before(left)) {
		return right
	}
	return left
}

func maxTime(left time.Time, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
