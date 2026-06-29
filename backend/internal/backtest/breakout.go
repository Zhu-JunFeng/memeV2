package backtest

import (
	"fmt"
	"math"

	"solana-meme-backtest/backend/internal/model"
)

type indexedTouch struct {
	index int
	point model.LevelAnchorPoint
}

type breakoutTouchGroup struct {
	touches        []indexedTouch
	lastTouchIndex int
	consolidation  *model.ConsolidationZone
}

const maxAllowedClosesAboveUpperBeforeBreakout = 2

func annotateBreakoutSetups(levels []model.PriceLevel, window []model.Kline, future []model.Kline, options LevelOptions) {
	if len(window) == 0 {
		return
	}
	for i := range levels {
		if levels[i].Calculation.ResistanceVotes == 0 {
			continue
		}
		setup := findBreakoutSetup(levels[i], window, future, options)
		if setup == nil {
			continue
		}
		levels[i].Breakout = setup
	}
}

func findBreakoutSetup(level model.PriceLevel, window []model.Kline, future []model.Kline, options LevelOptions) *model.BreakoutSetup {
	windowTouches := collectBullishRetestTouches(window, level, options)
	breakoutIndex, touchGroup := findBreakoutAfterTouches(window, future, level, windowTouches, options)
	if breakoutIndex < 0 || len(touchGroup.touches) == 0 {
		return nil
	}
	failedTouches := anchorPointsFromTouchGroup(touchGroup.touches)
	setup := &model.BreakoutSetup{
		Triggered:       true,
		FailedTouches:   failedTouches,
		Consolidation:   touchGroup.consolidation,
		AttemptStrategy: "n_bullish_high_touches_then_breakout_within_next_n_bars",
	}

	series := append(append([]model.Kline{}, window...), future...)
	if breakoutIndex >= len(series) {
		return setup
	}
	breakoutBar := series[breakoutIndex]
	breakoutPrice := breakoutThreshold(level, options.BreakTolerance)
	breakoutPoint := anchorFromKline(breakoutBar, breakoutPrice)
	entryIndex := breakoutIndex
	buyPoint := anchorFromKline(breakoutBar, breakoutPrice)
	setup.BreakoutPoint = &breakoutPoint
	setup.BuyPoint = &buyPoint

	stopLoss := level.Lower
	if stopLoss <= 0 || stopLoss >= buyPoint.Price {
		stopLoss = level.Price
	}
	risk := buyPoint.Price - stopLoss
	if risk <= 0 {
		return setup
	}
	takeProfit := buyPoint.Price + risk*options.TakeProfitRR
	outcome, exitPoint, holdingBars, profitRate := simulateBreakoutExit(series, entryIndex, stopLoss, takeProfit, options.MaxHoldBars)
	setup.StopLoss = stopLoss
	setup.TakeProfit = takeProfit
	setup.Risk = risk
	setup.Outcome = outcome
	setup.ExitPoint = exitPoint
	setup.HoldingBars = holdingBars
	setup.ProfitRate = profitRate
	setup.BreakoutReason = breakoutReason(level, breakoutBar, options)
	return setup
}

func findBreakoutAfterTouches(window []model.Kline, future []model.Kline, level model.PriceLevel, touches []indexedTouch, options LevelOptions) (int, breakoutTouchGroup) {
	series := append(append([]model.Kline{}, window...), future...)
	if len(series) == 0 {
		return -1, breakoutTouchGroup{}
	}
	for _, touchGroup := range buildQualifiedTouchGroups(window, level, touches, options) {
		breakoutIndex := findBreakoutIndexInRange(series, level, touchGroup.lastTouchIndex+1, touchGroup.lastTouchIndex+1+options.MinTouches, options)
		if breakoutIndex >= 0 && !hasLimitedUpperBandPierces(series, level, touchGroup.lastTouchIndex+1, breakoutIndex) {
			continue
		}
		if breakoutIndex >= 0 && hasTooManyClosesAboveUpperUntilBreakout(series, level, touchGroup, breakoutIndex, maxAllowedClosesAboveUpperBeforeBreakout) {
			continue
		}
		if breakoutIndex >= 0 {
			return breakoutIndex, touchGroup
		}
	}
	return -1, breakoutTouchGroup{}
}

func buildQualifiedTouchGroups(window []model.Kline, level model.PriceLevel, touches []indexedTouch, options LevelOptions) []breakoutTouchGroup {
	minTouches := options.MinTouches
	if minTouches <= 0 {
		minTouches = 3
	}
	if len(touches) < minTouches {
		return nil
	}
	groups := make([]breakoutTouchGroup, 0)
	for endTouch := minTouches - 1; endTouch < len(touches); endTouch++ {
		group := append([]indexedTouch{}, touches[endTouch-minTouches+1:endTouch+1]...)
		consolidation := collectConsolidationZone(window, level, anchorPointsFromTouchGroup(group))
		groups = append(groups, breakoutTouchGroup{
			touches:        group,
			lastTouchIndex: group[len(group)-1].index,
			consolidation:  consolidation,
		})
	}
	return groups
}

func anchorPointsFromTouchGroup(touches []indexedTouch) []model.LevelAnchorPoint {
	points := make([]model.LevelAnchorPoint, 0, len(touches))
	for _, touch := range touches {
		points = append(points, touch.point)
	}
	return points
}

func hasTooManyClosesAboveUpperUntilBreakout(series []model.Kline, level model.PriceLevel, touchGroup breakoutTouchGroup, breakoutIndex int, maxAllowed int) bool {
	if len(touchGroup.touches) == 0 {
		return false
	}
	start := touchGroup.touches[0].index
	end := breakoutIndex
	if start < 0 {
		start = 0
	}
	if end >= len(series) {
		end = len(series) - 1
	}
	count := 0
	for i := start; i <= end; i++ {
		if marketClose(series[i]) > level.Upper {
			count++
			if count > maxAllowed {
				return true
			}
		}
	}
	return false
}

func hasLimitedUpperBandPierces(klines []model.Kline, level model.PriceLevel, start int, breakoutIndex int) bool {
	if breakoutIndex <= start {
		return true
	}
	pierceCount := 0
	for i := start; i < breakoutIndex && i < len(klines); i++ {
		if marketHigh(klines[i]) > level.Upper {
			pierceCount++
			if pierceCount > 1 {
				return false
			}
		}
	}
	return true
}

func collectBullishRetestTouches(window []model.Kline, level model.PriceLevel, options LevelOptions) []indexedTouch {
	touches := make([]indexedTouch, 0)
	for index, item := range window {
		if !isBullish(item) {
			continue
		}
		high := marketHigh(item)
		if high < level.Lower || high > level.Upper {
			continue
		}
		if !touchVolumeConfirmed(window, index, options) {
			continue
		}
		touches = append(touches, indexedTouch{
			index: index,
			point: model.LevelAnchorPoint{
				Time:   item.OpenTime,
				Price:  high,
				Volume: item.Volume,
			},
		})
	}
	return touches
}

func touchVolumeConfirmed(klines []model.Kline, index int, options LevelOptions) bool {
	if index < 0 || index >= len(klines) {
		return false
	}
	baselineVolume := rollingAverageVolumeBefore(klines, index, options.VolumeWindow)
	if baselineVolume <= 0 {
		return true
	}
	// 试压阳线要求比普通突破更高一些的相对量能，降低弱触碰被纳入场景的概率。
	requiredMultiplier := math.Max(options.VolumeMultiplier, 1.35)
	return klines[index].Volume >= baselineVolume*requiredMultiplier
}

func detectRealtimeBreakoutSignal(level model.PriceLevel, window []model.Kline, current model.Kline, options LevelOptions) *RealtimeScenarioSignal {
	if level.Calculation.ResistanceVotes == 0 || len(window) == 0 {
		return nil
	}
	touches := collectBullishRetestTouches(window, level, options)
	groups := buildQualifiedTouchGroups(window, level, touches, options)
	if len(groups) == 0 {
		return nil
	}
	latestGroup := groups[len(groups)-1]
	minTouches := options.MinTouches
	if minTouches <= 0 {
		minTouches = 3
	}
	// 实时信号只接受“试压刚形成后不久就突破”的结构，避免旧结构在很久以后被误判成新信号。
	if len(window) > latestGroup.lastTouchIndex+minTouches {
		return nil
	}
	series := append(append([]model.Kline{}, window...), current)
	currentIndex := len(series) - 1
	if !hasLimitedUpperBandPierces(series, level, latestGroup.lastTouchIndex+1, currentIndex) {
		return nil
	}
	if hasTooManyClosesAboveUpperUntilBreakout(series, level, latestGroup, currentIndex, maxAllowedClosesAboveUpperBeforeBreakout) {
		return nil
	}
	if !isBreakoutConfirmedAtIndex(series, level, currentIndex, options) {
		return nil
	}
	breakoutPrice := breakoutThreshold(level, options.BreakTolerance)
	breakoutPoint := anchorFromKline(current, breakoutPrice)
	setup := &model.BreakoutSetup{
		Triggered:       true,
		FailedTouches:   anchorPointsFromTouchGroup(latestGroup.touches),
		Consolidation:   latestGroup.consolidation,
		BreakoutPoint:   &breakoutPoint,
		BuyPoint:        &breakoutPoint,
		AttemptStrategy: "n_bullish_high_touches_then_realtime_breakout_signal",
		BreakoutReason:  breakoutReason(level, current, options),
	}
	level.Breakout = setup
	return &RealtimeScenarioSignal{
		LevelType:           level.Type,
		LevelMarketCap:      level.Price,
		LevelLowerMarketCap: level.Lower,
		LevelUpperMarketCap: level.Upper,
		SignalTime:          current.OpenTime,
		SignalMarketCap:     breakoutPrice,
		BreakoutThreshold:   breakoutPrice,
		Reason:              setup.BreakoutReason,
		Calculation:         level.Calculation,
		Breakout:            setup,
	}
}

func rollingAverageVolumeBefore(klines []model.Kline, index int, window int) float64 {
	if len(klines) == 0 || index <= 0 {
		return 0
	}
	if window <= 0 {
		window = DefaultLevelOptions().VolumeWindow
	}
	start := index - window
	if start < 0 {
		start = 0
	}
	total := 0.0
	count := 0
	for i := start; i < index && i < len(klines); i++ {
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

func collectConsolidationZone(window []model.Kline, level model.PriceLevel, failedTouches []model.LevelAnchorPoint) *model.ConsolidationZone {
	if len(failedTouches) == 0 {
		return nil
	}
	firstTouchTime := failedTouches[0].Time
	lastTouchTime := failedTouches[len(failedTouches)-1].Time
	zone := &model.ConsolidationZone{
		StartTime:  firstTouchTime,
		EndTime:    lastTouchTime,
		TouchCount: len(failedTouches),
	}
	first := true
	for _, item := range window {
		if item.OpenTime.Before(firstTouchTime) || item.OpenTime.After(lastTouchTime) {
			continue
		}
		if first {
			zone.HighPrice = marketHigh(item)
			zone.LowPrice = marketLow(item)
			first = false
		}
		if marketHigh(item) > zone.HighPrice {
			zone.HighPrice = marketHigh(item)
		}
		if marketLow(item) < zone.LowPrice {
			zone.LowPrice = marketLow(item)
		}
		zone.BarCount++
	}
	if first {
		return nil
	}
	if zone.HighPrice < level.Upper {
		zone.HighPrice = level.Upper
	}
	return zone
}

func findBreakoutIndex(future []model.Kline, level model.PriceLevel, options LevelOptions) int {
	return findBreakoutIndexInRange(future, level, 0, len(future), options)
}

func findBreakoutIndexInRange(klines []model.Kline, level model.PriceLevel, start int, end int, options LevelOptions) int {
	confirmBars := options.ConfirmBars
	if confirmBars <= 0 {
		confirmBars = 1
	}
	if start < 0 {
		start = 0
	}
	if end > len(klines) {
		end = len(klines)
	}
	for i := start; i+confirmBars <= end; i++ {
		breakoutIndex := i + confirmBars - 1
		if isBreakoutConfirmedAtIndex(klines, level, breakoutIndex, options) {
			return breakoutIndex
		}
	}
	return -1
}

func isBreakoutConfirmedAtIndex(klines []model.Kline, level model.PriceLevel, breakoutIndex int, options LevelOptions) bool {
	if breakoutIndex < 0 || breakoutIndex >= len(klines) {
		return false
	}
	threshold := breakoutThreshold(level, options.BreakTolerance)
	confirmBars := options.ConfirmBars
	if confirmBars <= 0 {
		confirmBars = 1
	}
	start := breakoutIndex - confirmBars + 1
	if start < 0 {
		return false
	}
	for i := start; i <= breakoutIndex; i++ {
		if marketClose(klines[i]) <= threshold {
			return false
		}
	}
	return volumeBreakoutConfirmed(klines, breakoutIndex, options)
}

func volumeBreakoutConfirmed(klines []model.Kline, index int, options LevelOptions) bool {
	if index < 0 || index >= len(klines) {
		return false
	}
	avgVolume := averageRecentVolume(klines[:index+1], options.VolumeWindow)
	if avgVolume <= 0 {
		return true
	}
	return klines[index].Volume >= avgVolume*options.VolumeMultiplier
}

func simulateBreakoutExit(klines []model.Kline, entryIndex int, stopLoss float64, takeProfit float64, maxHoldBars int) (model.BreakoutOutcome, *model.LevelAnchorPoint, int, float64) {
	if entryIndex < 0 || entryIndex >= len(klines) {
		return model.BreakoutOutcomePending, nil, 0, 0
	}
	entryPrice := marketOpen(klines[entryIndex])
	if entryPrice <= 0 {
		entryPrice = marketClose(klines[entryIndex])
	}
	if maxHoldBars <= 0 {
		maxHoldBars = 30
	}
	last := minInt(len(klines)-1, entryIndex+maxHoldBars)
	for i := entryIndex; i <= last; i++ {
		item := klines[i]
		if marketLow(item) <= stopLoss {
			exit := anchorFromKline(item, stopLoss)
			return model.BreakoutOutcomeStopLoss, &exit, i - entryIndex + 1, profitRate(entryPrice, stopLoss)
		}
		if marketHigh(item) >= takeProfit {
			exit := anchorFromKline(item, takeProfit)
			return model.BreakoutOutcomeTakeProfit, &exit, i - entryIndex + 1, profitRate(entryPrice, takeProfit)
		}
	}
	exitBar := klines[last]
	exitPrice := marketClose(exitBar)
	exit := anchorFromKline(exitBar, exitPrice)
	return model.BreakoutOutcomeTimeout, &exit, last - entryIndex + 1, profitRate(entryPrice, exitPrice)
}

func breakoutReason(level model.PriceLevel, breakoutBar model.Kline, options LevelOptions) string {
	threshold := breakoutThreshold(level, options.BreakTolerance)
	minTouches := options.MinTouches
	if minTouches <= 0 {
		minTouches = 3
	}
	return fmt.Sprintf("%d 根阳线最高价触及压力带后，后续 %d 根 K 线内在 %s 收盘到 %.2f，站上突破阈值 %.2f", minTouches, minTouches, breakoutBar.OpenTime.Format("2006-01-02T15:04:05Z07:00"), marketClose(breakoutBar), threshold)
}

func breakoutThreshold(level model.PriceLevel, breakTolerance float64) float64 {
	centerThreshold := level.Price * (1 + breakTolerance)
	if centerThreshold < level.Upper {
		return level.Upper
	}
	return centerThreshold
}

func anchorFromKline(item model.Kline, price float64) model.LevelAnchorPoint {
	return model.LevelAnchorPoint{Time: item.OpenTime, Price: price, Volume: item.Volume}
}

func profitRate(entryPrice float64, exitPrice float64) float64 {
	if entryPrice <= 0 {
		return 0
	}
	return (exitPrice - entryPrice) / entryPrice
}

func isBullish(item model.Kline) bool {
	return marketClose(item) > marketOpen(item)
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
