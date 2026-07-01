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

func detectBandFollowEntryAtCurrentBar(klines []model.Kline, options LevelOptions, latestWindowOnly bool) (BandFollowReplayEntry, bool) {
	result, decisionBar, ok := detectLiveBreakoutSignalsByWindowsVariant(klines, options, PressureBreakoutDetector(), latestWindowOnly)
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

// DetectBandFollowEntryAtCurrentBar 给 latest-only 场景使用，只允许最新窗口触发买点。
func DetectBandFollowEntryAtCurrentBar(klines []model.Kline, options LevelOptions) (BandFollowReplayEntry, bool) {
	return detectBandFollowEntryAtCurrentBar(klines, options, true)
}

// DetectBandFollowReplayEntryAtCurrentBar 给历史回放、回测和候选池实时监控使用，允许多个滑动窗口逐根触发。
func DetectBandFollowReplayEntryAtCurrentBar(klines []model.Kline, options LevelOptions) (BandFollowReplayEntry, bool) {
	return detectBandFollowEntryAtCurrentBar(klines, options, false)
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
	seenScenarioKeys := map[string]struct{}{}
	for index := 1; index < len(klines); index++ {
		entry, ok := DetectBandFollowReplayEntryAtCurrentBar(klines[:index+1], options)
		if !ok {
			continue
		}
		scenarioKey := replayScenarioKey(entry.Level)
		if _, exists := seenScenarioKeys[scenarioKey]; exists {
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
		seenScenarioKeys[scenarioKey] = struct{}{}
	}
	return entries, windows
}

// CollectBandFollowReplayEntriesFromWindows turns the already annotated K-line
// loading result into strategy entries, so backtest trades use the same bands
// and buy markers shown on the chart.
func CollectBandFollowReplayEntriesFromWindows(klines []model.Kline, windows []WindowLevelResult) []BandFollowReplayEntry {
	if len(klines) == 0 || len(windows) == 0 {
		return nil
	}
	timeIndex := make(map[string]int, len(klines))
	for index, item := range klines {
		timeIndex[item.OpenTime.Format(time.RFC3339Nano)] = index
	}
	entries := make([]BandFollowReplayEntry, 0)
	seenScenarioKeys := map[string]struct{}{}
	for windowPos, window := range windows {
		globalWindow := window.WindowIndex
		if globalWindow <= 0 {
			globalWindow = windowPos + 1
		}
		for levelIndex, level := range window.Levels {
			if level.Breakout == nil || level.Breakout.BuyPoint == nil {
				continue
			}
			entryIndex, ok := timeIndex[level.Breakout.BuyPoint.Time.Format(time.RFC3339Nano)]
			if !ok {
				continue
			}
			scenarioKey := replayScenarioKey(level)
			if _, exists := seenScenarioKeys[scenarioKey]; exists {
				continue
			}
			levelForEntry := level
			if levelForEntry.Type == "" {
				levelForEntry.Type = model.LevelTypeResistance
			}
			signal := realtimeSignalFromWindowLevel(levelForEntry, levelIndex+1, globalWindow)
			entries = append(entries, BandFollowReplayEntry{
				EntryIndex:   entryIndex,
				WindowKey:    replayWindowKey(window),
				Window:       cloneWindowLevelResult(window),
				Signal:       signal,
				Level:        levelForEntry,
				GlobalWindow: globalWindow,
			})
			seenScenarioKeys[scenarioKey] = struct{}{}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].EntryIndex != entries[j].EntryIndex {
			return entries[i].EntryIndex < entries[j].EntryIndex
		}
		if entries[i].GlobalWindow != entries[j].GlobalWindow {
			return entries[i].GlobalWindow < entries[j].GlobalWindow
		}
		return entries[i].Signal.LevelIndex < entries[j].Signal.LevelIndex
	})
	return entries
}

func realtimeSignalFromWindowLevel(level model.PriceLevel, levelIndex int, windowIndex int) RealtimeScenarioSignal {
	signal := RealtimeScenarioSignal{
		ScenarioCode:        "pressure_breakout",
		ScenarioName:        "压力带历史突破",
		WindowIndex:         windowIndex,
		LevelIndex:          levelIndex,
		LevelType:           level.Type,
		LevelMarketCap:      level.Price,
		LevelLowerMarketCap: level.Lower,
		LevelUpperMarketCap: level.Upper,
		Calculation:         level.Calculation,
		Breakout:            level.Breakout,
	}
	if level.Breakout != nil {
		if level.Breakout.BuyPoint != nil {
			signal.SignalTime = level.Breakout.BuyPoint.Time
			signal.SignalMarketCap = level.Breakout.BuyPoint.Price
		}
		if level.Breakout.BreakoutPoint != nil {
			signal.BreakoutThreshold = level.Breakout.BreakoutPoint.Price
		}
		signal.Reason = level.Breakout.BreakoutReason
	}
	return signal
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

func replayScenarioKey(level model.PriceLevel) string {
	key := levelSelectionKey(level)
	if level.Breakout == nil || len(level.Breakout.FailedTouches) == 0 {
		return key
	}
	for _, point := range level.Breakout.FailedTouches {
		key += "|" + point.Time.Format(time.RFC3339Nano)
	}
	return key
}

func cloneWindowLevelResult(window WindowLevelResult) WindowLevelResult {
	cloned := window
	cloned.Levels = append([]model.PriceLevel(nil), window.Levels...)
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
	key := levelBandKey(level)
	items := make([]model.PriceLevel, 0, len(existing)+1)
	replaced := false
	for _, item := range existing {
		if levelBandKey(item) == key {
			if shouldReplaceLevelForSelection(item, level) {
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
