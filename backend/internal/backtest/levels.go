package backtest

import (
	"math"
	"sort"
	"strconv"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

const lowVolumeIncludeRatio = 0.5

type LevelOptions struct {
	PivotWindow      int
	PriceTolerance   float64
	BreakTolerance   float64
	ConfirmBars      int
	VolumeWindow     int
	VolumeMultiplier float64
	MaxLevels        int
	WindowSize       int
	WindowStep       int
	LevelWindowSize  int
	LevelWindowStep  int
	MinTouches       int
	EntryOffsetBars  int
	TakeProfitRR     float64
	MaxHoldBars      int
}

type pivotPoint struct {
	Type   model.LevelType
	Price  float64
	Time   time.Time
	Volume float64
}

type levelCluster struct {
	Price           float64
	Lower           float64
	Upper           float64
	Pivots          int
	Touches         int
	Volume          float64
	PivotVolume     float64
	SupportVotes    int
	ResistanceVotes int
	FirstTime       time.Time
	LastTime        time.Time
	PivotPoints     []model.LevelAnchorPoint
	SampleTouches   []model.LevelAnchorPoint
}

type indexedWindow struct {
	start int
	end   int
	items []model.Kline
}

func DefaultLevelOptions() LevelOptions {
	return LevelOptions{
		PivotWindow:      5,
		PriceTolerance:   0.02,
		BreakTolerance:   0.01,
		ConfirmBars:      2,
		VolumeWindow:     20,
		VolumeMultiplier: 1.2,
		MaxLevels:        8,
		WindowSize:       120,
		WindowStep:       1,
		LevelWindowSize:  240,
		LevelWindowStep:  50,
		MinTouches:       3,
		EntryOffsetBars:  1,
		TakeProfitRR:     2,
		MaxHoldBars:      30,
	}
}

func CalculateSupportResistanceByWindows(klines []model.Kline, options LevelOptions, windowSize int, windowStep int) []WindowLevelResult {
	return CalculateLevelScenariosByWindows(klines, options, windowSize, windowStep, pressureBreakoutDetector())
}

// CalculateLevelScenariosByWindows 把“窗口切分”和“场景识别”解耦：
// 先统一算出各个时间窗口的价位，再交给不同场景识别器决定如何标注和筛选。
func CalculateLevelScenariosByWindows(klines []model.Kline, options LevelOptions, windowSize int, windowStep int, detector ScenarioDetector) []WindowLevelResult {
	options = normalizeLevelOptions(options, len(klines))
	items := normalizedKlines(klines, options)
	if len(items) == 0 {
		return nil
	}
	if windowSize <= 0 || windowSize > len(items) {
		windowSize = len(items)
	}
	if windowStep <= 0 {
		windowStep = 1
	}
	levelWindowSize := options.LevelWindowSize
	if levelWindowSize <= 0 || levelWindowSize > len(items) {
		levelWindowSize = windowSize
	}
	if levelWindowSize <= 0 || levelWindowSize > len(items) {
		levelWindowSize = len(items)
	}
	levelWindowStep := options.LevelWindowStep
	if levelWindowStep <= 0 {
		levelWindowStep = windowStep
	}
	if levelWindowStep <= 0 {
		levelWindowStep = 1
	}
	klineWindows := buildIndexedWindows(items, windowSize, windowStep)
	levelWindows := buildIndexedWindows(items, levelWindowSize, levelWindowStep)
	levelSets := make([][]model.PriceLevel, 0, len(levelWindows))
	for _, levelWindow := range levelWindows {
		// 压力/支撑位先在独立的“压力带窗口”内计算，后面再和检测窗口做交集验证。
		levelSets = append(levelSets, calculateSupportResistanceAll(levelWindow.items, options))
	}
	results := make([]WindowLevelResult, 0)
	for _, klineWindow := range klineWindows {
		levels := make([]model.PriceLevel, 0)
		for levelIndex, levelWindow := range levelWindows {
			start, end := intersectIndexes(klineWindow, levelWindow)
			if start >= end {
				continue
			}
			intersection := items[start:end]
			if len(intersection) == 0 {
				continue
			}
			candidates := clonePriceLevels(levelSets[levelIndex])
			// 不同场景只关心自己的结构识别，窗口切分和价位计算保持通用。
			detector.AnnotateHistorical(candidates, intersection, items[end:], options)
			for _, level := range candidates {
				if level.Breakout != nil && level.Breakout.Consolidation != nil && level.Breakout.BreakoutPoint != nil {
					levels = append(levels, level)
				}
			}
		}
		if len(levels) == 0 {
			continue
		}
		levels = selectTopLevelsKeepingBreakout(levels, options.MaxLevels)
		results = append(results, WindowLevelResult{
			WindowIndex: len(results) + 1,
			StartTime:   klineWindow.items[0].OpenTime,
			EndTime:     klineWindow.items[len(klineWindow.items)-1].CloseTime,
			KlineCount:  len(klineWindow.items),
			Levels:      levels,
		})
	}
	dedupeBreakoutsByKlineSignature(results)
	return pruneWindowsWithoutBreakouts(results)
}

// CalculateRealtimeScenarioSignalsByWindows 基于“历史窗口 + 当前实时 K 线”做实时信号判定。
// 这里不会要求当前 K 线已经写入历史窗口，适合外部行情推送到来时即时判断是否触发信号。
func CalculateRealtimeScenarioSignalsByWindows(klines []model.Kline, current model.Kline, options LevelOptions, windowSize int, windowStep int, detector ScenarioDetector) RealtimeSignalResult {
	return calculateRealtimeScenarioSignalsByWindows(klines, current, options, windowSize, windowStep, detector, true)
}

// CalculateReplayScenarioSignalsByWindows 给历史回放/回测和候选池实时监控使用，
// 允许当前 bar 同时匹配多个滑动窗口，保留“多窗口回放”的历史口径。
func CalculateReplayScenarioSignalsByWindows(klines []model.Kline, current model.Kline, options LevelOptions, windowSize int, windowStep int, detector ScenarioDetector) RealtimeSignalResult {
	return calculateRealtimeScenarioSignalsByWindows(klines, current, options, windowSize, windowStep, detector, false)
}

func calculateRealtimeScenarioSignalsByWindows(klines []model.Kline, current model.Kline, options LevelOptions, windowSize int, windowStep int, detector ScenarioDetector, latestWindowOnly bool) RealtimeSignalResult {
	options = normalizeLevelOptions(options, len(klines))
	items := normalizedKlines(klines, options)
	if len(items) == 0 {
		return RealtimeSignalResult{}
	}
	if windowSize <= 0 || windowSize > len(items) {
		windowSize = len(items)
	}
	if windowStep <= 0 {
		windowStep = 1
	}
	levelWindowSize := options.LevelWindowSize
	if levelWindowSize <= 0 || levelWindowSize > len(items) {
		levelWindowSize = windowSize
	}
	if levelWindowSize <= 0 || levelWindowSize > len(items) {
		levelWindowSize = len(items)
	}
	levelWindowStep := options.LevelWindowStep
	if levelWindowStep <= 0 {
		levelWindowStep = windowStep
	}
	if levelWindowStep <= 0 {
		levelWindowStep = 1
	}
	klineWindows := buildIndexedWindows(items, windowSize, windowStep)
	levelWindows := buildIndexedWindows(items, levelWindowSize, levelWindowStep)
	levelSets := make([][]model.PriceLevel, 0, len(levelWindows))
	for _, levelWindow := range levelWindows {
		levelSets = append(levelSets, calculateSupportResistanceAll(levelWindow.items, options))
	}
	windows := make([]WindowLevelResult, 0)
	signals := make([]RealtimeScenarioSignal, 0)
	for _, klineWindow := range klineWindows {
		// 实时突破只允许基于“紧贴当前 bar 的最新连续窗口”判定，
		// 避免很早之前的旧窗口在很多小时后仍被当前 bar 复用成新信号。
		if latestWindowOnly && klineWindow.end != len(items) {
			continue
		}
		windowSignals := make([]RealtimeScenarioSignal, 0)
		windowLevels := make([]model.PriceLevel, 0)
		for levelIndex, levelWindow := range levelWindows {
			start, end := intersectIndexes(klineWindow, levelWindow)
			if start >= end {
				continue
			}
			intersection := items[start:end]
			if len(intersection) == 0 {
				continue
			}
			candidates := clonePriceLevels(levelSets[levelIndex])
			detected := detector.DetectRealtimeSignals(candidates, intersection, current, options)
			for _, signal := range detected {
				level := candidates[signal.LevelIndex-1]
				if level.Breakout != nil || (signal.Breakout != nil && signal.Breakout.Consolidation != nil) {
					windowLevels = append(windowLevels, level)
				}
				windowSignals = append(windowSignals, signal)
			}
		}
		if len(windowSignals) == 0 {
			continue
		}
		windowLevels = selectTopLevelsKeepingBreakout(windowLevels, options.MaxLevels)
		windowResult := WindowLevelResult{
			WindowIndex: len(windows) + 1,
			StartTime:   klineWindow.items[0].OpenTime,
			EndTime:     current.CloseTime,
			KlineCount:  len(klineWindow.items) + 1,
			Levels:      windowLevels,
		}
		for i := range windowSignals {
			windowSignals[i].WindowIndex = windowResult.WindowIndex
		}
		windows = append(windows, windowResult)
		signals = append(signals, windowSignals...)
	}
	return RealtimeSignalResult{
		Klines:     append(append([]model.Kline{}, items...), current),
		Windows:    windows,
		Signals:    signals,
		WindowSize: windowSize,
		WindowStep: windowStep,
	}
}

func CalculateSupportResistance(klines []model.Kline, options LevelOptions) []model.PriceLevel {
	levels := calculateSupportResistanceAll(klines, options)
	return selectTopLevels(levels, options.MaxLevels)
}

func buildIndexedWindows(items []model.Kline, windowSize int, windowStep int) []indexedWindow {
	if len(items) == 0 {
		return nil
	}
	if windowSize <= 0 || windowSize > len(items) {
		windowSize = len(items)
	}
	if windowStep <= 0 {
		windowStep = 1
	}
	windows := make([]indexedWindow, 0)
	for start := 0; start+windowSize <= len(items); start += windowStep {
		end := start + windowSize
		windows = append(windows, indexedWindow{
			start: start,
			end:   end,
			items: items[start:end],
		})
	}
	if len(windows) == 0 {
		windows = append(windows, indexedWindow{
			start: 0,
			end:   len(items),
			items: items,
		})
	}
	return windows
}

func intersectIndexes(left indexedWindow, right indexedWindow) (int, int) {
	start := left.start
	if right.start > start {
		start = right.start
	}
	end := left.end
	if right.end < end {
		end = right.end
	}
	return start, end
}

func clonePriceLevels(levels []model.PriceLevel) []model.PriceLevel {
	if len(levels) == 0 {
		return nil
	}
	cloned := make([]model.PriceLevel, len(levels))
	copy(cloned, levels)
	for i := range cloned {
		cloned[i].Breakout = nil
	}
	return cloned
}

func calculateSupportResistanceAll(klines []model.Kline, options LevelOptions) []model.PriceLevel {
	options = normalizeLevelOptions(options, len(klines))
	items := normalizedKlines(klines, options)
	if len(items) == 0 {
		return nil
	}
	if len(items) < options.PivotWindow*2+1 {
		return nil
	}
	tolerance := effectivePriceTolerance(items, options.PriceTolerance)
	pivots := collectPivots(items, options.PivotWindow, options)
	clusters := mergePivots(pivots, tolerance)
	levels := buildLevels(clusters, items, tolerance)
	scoreLevels(levels, items)
	for i := range levels {
		status, reason := detectLevelStatus(items, levels[i], options)
		levels[i].Status = status
		levels[i].Calculation.StatusReason = reason
	}
	return levels
}

func normalizeLevelOptions(options LevelOptions, itemCount int) LevelOptions {
	defaults := DefaultLevelOptions()
	if options.PivotWindow <= 0 {
		options.PivotWindow = defaults.PivotWindow
	}
	maxWindow := (itemCount - 1) / 2
	if maxWindow > 0 && options.PivotWindow > maxWindow {
		options.PivotWindow = maxWindow
	}
	if options.PriceTolerance <= 0 {
		options.PriceTolerance = defaults.PriceTolerance
	}
	if options.BreakTolerance <= 0 {
		options.BreakTolerance = defaults.BreakTolerance
	}
	if options.ConfirmBars <= 0 {
		options.ConfirmBars = defaults.ConfirmBars
	}
	if options.ConfirmBars > itemCount {
		options.ConfirmBars = itemCount
	}
	if options.VolumeWindow <= 0 {
		options.VolumeWindow = defaults.VolumeWindow
	}
	if options.VolumeMultiplier <= 0 {
		options.VolumeMultiplier = defaults.VolumeMultiplier
	}
	if options.MaxLevels <= 0 {
		options.MaxLevels = defaults.MaxLevels
	}
	if options.MinTouches <= 0 {
		options.MinTouches = defaults.MinTouches
	}
	if options.EntryOffsetBars < 0 {
		options.EntryOffsetBars = defaults.EntryOffsetBars
	}
	if options.TakeProfitRR <= 0 {
		options.TakeProfitRR = defaults.TakeProfitRR
	}
	if options.MaxHoldBars <= 0 {
		options.MaxHoldBars = defaults.MaxHoldBars
	}
	return options
}

func normalizedKlines(klines []model.Kline, options LevelOptions) []model.Kline {
	items := make([]model.Kline, 0, len(klines))
	for _, item := range klines {
		if marketHigh(item) <= 0 || marketLow(item) <= 0 || marketClose(item) <= 0 || marketHigh(item) < marketLow(item) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OpenTime.Before(items[j].OpenTime) })
	filtered := make([]model.Kline, 0, len(items))
	for i, item := range items {
		baselineVolume := rollingAverageVolume(items, i, options.VolumeWindow)
		if baselineVolume > 0 && item.Volume < baselineVolume*lowVolumeIncludeRatio {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func effectivePriceTolerance(klines []model.Kline, baseTolerance float64) float64 {
	atrPercent := averageTrueRangePercent(klines, 20)
	adaptive := atrPercent * 0.5
	if adaptive < baseTolerance {
		adaptive = baseTolerance
	}
	if adaptive > 0.08 {
		adaptive = 0.08
	}
	return adaptive
}

func averageTrueRangePercent(klines []model.Kline, window int) float64 {
	if len(klines) == 0 {
		return 0
	}
	start := len(klines) - window
	if start < 0 {
		start = 0
	}
	total := 0.0
	count := 0
	for i := start; i < len(klines); i++ {
		item := klines[i]
		previousClose := marketClose(item)
		if i > 0 && marketClose(klines[i-1]) > 0 {
			previousClose = marketClose(klines[i-1])
		}
		high := marketHigh(item)
		low := marketLow(item)
		closeValue := marketClose(item)
		trueRange := math.Max(high-low, math.Max(math.Abs(high-previousClose), math.Abs(low-previousClose)))
		if closeValue > 0 {
			total += trueRange / closeValue
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func collectPivots(klines []model.Kline, window int, options LevelOptions) []pivotPoint {
	pivots := make([]pivotPoint, 0)
	for i := window; i < len(klines)-window; i++ {
		current := klines[i]
		isLow := true
		isHigh := true
		hasLowerNeighbor := false
		hasHigherNeighbor := false
		for j := i - window; j <= i+window; j++ {
			if j == i {
				continue
			}
			if marketLow(current) > marketLow(klines[j]) {
				isLow = false
			}
			if marketLow(current) < marketLow(klines[j]) {
				hasLowerNeighbor = true
			}
			if marketHigh(current) < marketHigh(klines[j]) {
				isHigh = false
			}
			if marketHigh(current) > marketHigh(klines[j]) {
				hasHigherNeighbor = true
			}
			if !isLow && !isHigh {
				break
			}
		}
		if isLow && hasLowerNeighbor && marketLow(current) > 0 {
			pivots = append(pivots, pivotPoint{Type: model.LevelTypeSupport, Price: marketLow(current), Time: current.OpenTime, Volume: current.Volume})
		}
		if isHigh && hasHigherNeighbor && marketHigh(current) > 0 && isQualifiedPressureCandle(klines, i, options) {
			pivots = append(pivots, pivotPoint{Type: model.LevelTypeResistance, Price: marketHigh(current), Time: current.OpenTime, Volume: current.Volume})
		}
	}
	return pivots
}

func isQualifiedPressureCandle(klines []model.Kline, index int, options LevelOptions) bool {
	current := klines[index]
	if marketClose(current) <= marketOpen(current) {
		return false
	}
	baselineVolume := rollingAverageVolume(klines, index, options.VolumeWindow)
	if baselineVolume <= 0 {
		return true
	}
	return current.Volume >= baselineVolume*options.VolumeMultiplier
}

func rollingAverageVolume(klines []model.Kline, index int, window int) float64 {
	if len(klines) == 0 {
		return 0
	}
	if window <= 0 {
		window = DefaultLevelOptions().VolumeWindow
	}
	start := index - window + 1
	if start < 0 {
		start = 0
	}
	total := 0.0
	count := 0
	for i := start; i <= index && i < len(klines); i++ {
		if klines[i].Volume <= 0 {
			continue
		}
		total += klines[i].Volume
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func mergePivots(pivots []pivotPoint, tolerance float64) []levelCluster {
	clusters := make([]levelCluster, 0)
	for _, pivot := range pivots {
		matched := -1
		for i := range clusters {
			if clusters[i].Price == 0 {
				continue
			}
			if math.Abs(pivot.Price-clusters[i].Price)/clusters[i].Price <= tolerance {
				matched = i
				break
			}
		}
		if matched >= 0 {
			cluster := &clusters[matched]
			oldPivots := cluster.Pivots
			cluster.Pivots++
			cluster.Price = (cluster.Price*float64(oldPivots) + pivot.Price) / float64(cluster.Pivots)
			cluster.Lower = math.Min(cluster.Lower, pivot.Price*(1-tolerance))
			cluster.Upper = math.Max(cluster.Upper, pivot.Price*(1+tolerance))
			cluster.PivotVolume += pivot.Volume
			cluster.PivotPoints = append(cluster.PivotPoints, model.LevelAnchorPoint{Time: pivot.Time, Price: pivot.Price, Volume: pivot.Volume})
			if pivot.Type == model.LevelTypeSupport {
				cluster.SupportVotes++
			} else if pivot.Type == model.LevelTypeResistance {
				cluster.ResistanceVotes++
			}
			if pivot.Time.Before(cluster.FirstTime) {
				cluster.FirstTime = pivot.Time
			}
			if pivot.Time.After(cluster.LastTime) {
				cluster.LastTime = pivot.Time
			}
			continue
		}
		cluster := levelCluster{
			Price:       pivot.Price,
			Lower:       pivot.Price * (1 - tolerance),
			Upper:       pivot.Price * (1 + tolerance),
			Pivots:      1,
			PivotVolume: pivot.Volume,
			FirstTime:   pivot.Time,
			LastTime:    pivot.Time,
			PivotPoints: []model.LevelAnchorPoint{{Time: pivot.Time, Price: pivot.Price, Volume: pivot.Volume}},
		}
		if pivot.Type == model.LevelTypeSupport {
			cluster.SupportVotes = 1
		} else if pivot.Type == model.LevelTypeResistance {
			cluster.ResistanceVotes = 1
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func buildLevels(clusters []levelCluster, klines []model.Kline, tolerance float64) []model.PriceLevel {
	levels := make([]model.PriceLevel, 0, len(clusters))
	if len(klines) == 0 {
		return levels
	}
	currentPrice := marketClose(klines[len(klines)-1])
	for _, cluster := range clusters {
		cluster.Touches, cluster.Volume, cluster.SampleTouches = countTouchesAndVolume(cluster, klines)
		if cluster.Touches == 0 {
			cluster.Touches = cluster.Pivots
			cluster.Volume = cluster.PivotVolume
		}
		levelType, typeReason := classifyLevelType(cluster, currentPrice)
		levels = append(levels, model.PriceLevel{
			Type:      levelType,
			Price:     cluster.Price,
			Lower:     cluster.Price * (1 - tolerance),
			Upper:     cluster.Price * (1 + tolerance),
			Touches:   cluster.Touches,
			Volume:    cluster.Volume,
			FirstTime: cluster.FirstTime,
			LastTime:  cluster.LastTime,
			Calculation: model.LevelCalculation{
				PivotCount:      cluster.Pivots,
				SupportVotes:    cluster.SupportVotes,
				ResistanceVotes: cluster.ResistanceVotes,
				Tolerance:       tolerance,
				CurrentPrice:    currentPrice,
				TypeReason:      typeReason,
				Pivots:          cluster.PivotPoints,
				SampleTouches:   cluster.SampleTouches,
			},
		})
	}
	return levels
}

func countTouchesAndVolume(cluster levelCluster, klines []model.Kline) (int, float64, []model.LevelAnchorPoint) {
	touches := 0
	volume := 0.0
	samples := make([]model.LevelAnchorPoint, 0, 5)
	for _, item := range klines {
		if marketHigh(item) >= cluster.Lower && marketLow(item) <= cluster.Upper {
			touches++
			volume += item.Volume
			if len(samples) < 5 {
				samples = append(samples, model.LevelAnchorPoint{Time: item.OpenTime, Price: marketClose(item), Volume: item.Volume})
			}
		}
	}
	return touches, volume, samples
}

func classifyLevelType(cluster levelCluster, currentPrice float64) (model.LevelType, string) {
	if currentPrice > cluster.Upper {
		return model.LevelTypeSupport, "当前市值高于市值带上沿，历史压力突破后按支撑处理"
	}
	if currentPrice < cluster.Lower {
		return model.LevelTypeResistance, "当前市值低于市值带下沿，历史支撑跌破后按压力处理"
	}
	if cluster.SupportVotes >= cluster.ResistanceVotes {
		return model.LevelTypeSupport, "当前市值在市值带内，且局部低点票数不少于局部高点票数"
	}
	return model.LevelTypeResistance, "当前市值在市值带内，且局部高点票数多于局部低点票数"
}

func scoreLevels(levels []model.PriceLevel, klines []model.Kline) {
	if len(klines) == 0 {
		return
	}
	end := klines[len(klines)-1].OpenTime
	start := klines[0].OpenTime
	totalDuration := end.Sub(start).Seconds()
	if totalDuration <= 0 {
		totalDuration = 1
	}
	maxVolume := 0.0
	maxTouches := 0
	for _, level := range levels {
		if level.Volume > maxVolume {
			maxVolume = level.Volume
		}
		if level.Touches > maxTouches {
			maxTouches = level.Touches
		}
	}
	if maxVolume <= 0 {
		maxVolume = 1
	}
	if maxTouches <= 0 {
		maxTouches = 1
	}
	currentPrice := marketClose(klines[len(klines)-1])
	for i := range levels {
		touchScore := float64(levels[i].Touches) / float64(maxTouches) * 4
		volumeScore := levels[i].Volume / maxVolume * 3
		recencyScore := 1 - end.Sub(levels[i].LastTime).Seconds()/totalDuration
		if recencyScore < 0 {
			recencyScore = 0
		}
		distanceScore := distanceScore(currentPrice, levels[i].Price)
		levels[i].Score = touchScore + volumeScore + recencyScore*2 + distanceScore
		levels[i].Calculation.ScoreParts = model.LevelScoreParts{
			Touch:    touchScore,
			Volume:   volumeScore,
			Recency:  recencyScore * 2,
			Distance: distanceScore,
		}
	}
}

func distanceScore(currentPrice float64, levelPrice float64) float64 {
	if currentPrice <= 0 || levelPrice <= 0 {
		return 0
	}
	distance := math.Abs(currentPrice-levelPrice) / currentPrice
	if distance >= 0.5 {
		return 0
	}
	return (1 - distance/0.5) * 1.5
}

func detectLevelStatus(klines []model.Kline, level model.PriceLevel, options LevelOptions) (model.LevelStatus, string) {
	if len(klines) < options.ConfirmBars {
		return model.LevelStatusHolding, "K 线数量少于确认根数，暂按保持处理"
	}
	recent := klines[len(klines)-options.ConfirmBars:]
	avgVolume := averageRecentVolume(klines, options.VolumeWindow)
	lastVolume := recent[len(recent)-1].Volume
	volumeConfirmed := avgVolume <= 0 || lastVolume >= avgVolume*options.VolumeMultiplier
	if level.Type == model.LevelTypeSupport {
		threshold := level.Lower * (1 - options.BreakTolerance)
		broken := true
		for _, item := range recent {
			if marketClose(item) >= threshold {
				broken = false
				break
			}
		}
		if broken && volumeConfirmed {
			return model.LevelStatusSupportBroken, "最近连续收盘市值低于支撑跌破阈值，且成交量确认"
		}
		if broken {
			return model.LevelStatusSupportPierced, "最近连续收盘市值低于支撑跌破阈值，但成交量未确认"
		}
		return model.LevelStatusHolding, "最近收盘市值仍守在支撑跌破阈值上方"
	}
	threshold := level.Upper * (1 + options.BreakTolerance)
	broken := true
	for _, item := range recent {
		if marketClose(item) <= threshold {
			broken = false
			break
		}
	}
	if broken && volumeConfirmed {
		return model.LevelStatusResistanceBroken, "最近连续收盘市值高于压力突破阈值，且成交量确认"
	}
	if broken {
		return model.LevelStatusResistancePierced, "最近连续收盘市值高于压力突破阈值，但成交量未确认"
	}
	return model.LevelStatusRejected, "最近收盘市值未能有效突破压力阈值"
}

func averageRecentVolume(klines []model.Kline, window int) float64 {
	if len(klines) == 0 {
		return 0
	}
	start := len(klines) - window
	if start < 0 {
		start = 0
	}
	total := 0.0
	count := 0
	for _, item := range klines[start:] {
		total += item.Volume
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func selectTopLevels(levels []model.PriceLevel, maxPerType int) []model.PriceLevel {
	supports := make([]model.PriceLevel, 0)
	resistances := make([]model.PriceLevel, 0)
	for _, level := range levels {
		if level.Type == model.LevelTypeSupport {
			supports = append(supports, level)
		} else if level.Type == model.LevelTypeResistance {
			resistances = append(resistances, level)
		}
	}
	sort.Slice(supports, func(i, j int) bool { return supports[i].Score > supports[j].Score })
	sort.Slice(resistances, func(i, j int) bool { return resistances[i].Score > resistances[j].Score })
	if len(supports) > maxPerType {
		supports = supports[:maxPerType]
	}
	if len(resistances) > maxPerType {
		resistances = resistances[:maxPerType]
	}
	selected := append(supports, resistances...)
	sort.Slice(selected, func(i, j int) bool { return selected[i].Price < selected[j].Price })
	return selected
}

func selectTopLevelsKeepingBreakout(levels []model.PriceLevel, maxPerType int) []model.PriceLevel {
	selected := dedupeLevelsBySelectionKey(selectTopLevels(levels, maxPerType))
	if len(levels) == 0 {
		return selected
	}
	exists := make(map[string]struct{}, len(selected))
	for _, level := range selected {
		exists[levelSelectionKey(level)] = struct{}{}
	}
	for _, level := range levels {
		if level.Breakout == nil || level.Breakout.Consolidation == nil || level.Breakout.BreakoutPoint == nil {
			continue
		}
		key := levelSelectionKey(level)
		if _, ok := exists[key]; ok {
			continue
		}
		selected = append(selected, level)
		exists[key] = struct{}{}
	}
	selected = dedupeLevelsBySelectionKey(selected)
	sort.Slice(selected, func(i, j int) bool { return selected[i].Price < selected[j].Price })
	return selected
}

// 同一价格带可能会同时出现“普通 level”和“带 breakout 的 level”两条记录。
// 图表和回测只应该保留一条，优先保留带 breakout 的版本，其次保留分数更高的版本。
func dedupeLevelsBySelectionKey(levels []model.PriceLevel) []model.PriceLevel {
	if len(levels) <= 1 {
		return levels
	}
	bestByKey := make(map[string]model.PriceLevel, len(levels))
	order := make([]string, 0, len(levels))
	for _, level := range levels {
		key := levelBandKey(level)
		if existing, ok := bestByKey[key]; ok {
			if shouldReplaceLevelForSelection(existing, level) {
				bestByKey[key] = level
			}
			continue
		}
		bestByKey[key] = level
		order = append(order, key)
	}
	items := make([]model.PriceLevel, 0, len(bestByKey))
	for _, key := range order {
		items = append(items, bestByKey[key])
	}
	return items
}

func shouldReplaceLevelForSelection(current model.PriceLevel, next model.PriceLevel) bool {
	currentHasBreakout := current.Breakout != nil && current.Breakout.BreakoutPoint != nil
	nextHasBreakout := next.Breakout != nil && next.Breakout.BreakoutPoint != nil
	if currentHasBreakout != nextHasBreakout {
		return nextHasBreakout
	}
	return next.Score > current.Score
}

func levelBandKey(level model.PriceLevel) string {
	return string(level.Type) + "|" + strconvFormatFloat(level.Price) + "|" + strconvFormatFloat(level.Lower) + "|" + strconvFormatFloat(level.Upper)
}

func levelSelectionKey(level model.PriceLevel) string {
	return string(level.Type) + "|" + level.FirstTime.Format(time.RFC3339) + "|" + level.LastTime.Format(time.RFC3339) + "|" + strconvFormatFloat(level.Price)
}

type breakoutSelection struct {
	windowIndex int
	levelIndex  int
	score       float64
}

func dedupeBreakoutsByKlineSignature(windows []WindowLevelResult) {
	bestBySignature := make(map[string]breakoutSelection)
	for windowIndex := range windows {
		for levelIndex := range windows[windowIndex].Levels {
			level := windows[windowIndex].Levels[levelIndex]
			signature := breakoutKlineSignature(level)
			if signature == "" {
				continue
			}
			best, exists := bestBySignature[signature]
			if !exists || level.Score > best.score {
				bestBySignature[signature] = breakoutSelection{windowIndex: windowIndex, levelIndex: levelIndex, score: level.Score}
			}
		}
	}
	for windowIndex := range windows {
		for levelIndex := range windows[windowIndex].Levels {
			signature := breakoutKlineSignature(windows[windowIndex].Levels[levelIndex])
			if signature == "" {
				continue
			}
			best := bestBySignature[signature]
			if best.windowIndex == windowIndex && best.levelIndex == levelIndex {
				continue
			}
			windows[windowIndex].Levels[levelIndex].Breakout = nil
		}
	}
}

func pruneWindowsWithoutBreakouts(windows []WindowLevelResult) []WindowLevelResult {
	pruned := make([]WindowLevelResult, 0, len(windows))
	for _, window := range windows {
		levels := make([]model.PriceLevel, 0, len(window.Levels))
		for _, level := range window.Levels {
			if level.Breakout == nil || level.Breakout.Consolidation == nil || level.Breakout.BreakoutPoint == nil {
				continue
			}
			levels = append(levels, level)
		}
		if len(levels) == 0 {
			continue
		}
		window.Levels = levels
		window.WindowIndex = len(pruned) + 1
		pruned = append(pruned, window)
	}
	return pruned
}

func breakoutKlineSignature(level model.PriceLevel) string {
	if level.Breakout == nil || level.Breakout.BreakoutPoint == nil || len(level.Breakout.FailedTouches) == 0 {
		return ""
	}
	signature := "pressure"
	for _, point := range level.Breakout.FailedTouches {
		signature += "|" + point.Time.Format(time.RFC3339Nano)
	}
	signature += "|breakout|" + level.Breakout.BreakoutPoint.Time.Format(time.RFC3339Nano)
	return signature
}

func strconvFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func marketOpen(item model.Kline) float64 {
	return item.MarketCapOpen
}

func marketHigh(item model.Kline) float64 {
	return item.MarketCapHigh
}

func marketLow(item model.Kline) float64 {
	return item.MarketCapLow
}

func marketClose(item model.Kline) float64 {
	return item.MarketCapClose
}
