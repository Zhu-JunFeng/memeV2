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
	minTouches := options.MinTouches
	if minTouches <= 0 {
		minTouches = 3
	}
	windowTouches := collectBullishRetestTouches(window, level, options)
	if len(windowTouches) < minTouches {
		return nil
	}

	breakoutIndex, failedTouchIndexes := findBreakoutAfterTouches(window, future, level, windowTouches, minTouches, options)
	if breakoutIndex < 0 || len(failedTouchIndexes) < minTouches {
		return nil
	}

	failedTouches := make([]model.LevelAnchorPoint, 0, len(failedTouchIndexes))
	for _, touch := range failedTouchIndexes {
		failedTouches = append(failedTouches, touch.point)
	}
	consolidation := collectConsolidationZone(window, level, failedTouches)
	setup := &model.BreakoutSetup{
		Triggered:       true,
		FailedTouches:   failedTouches,
		Consolidation:   consolidation,
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

func findBreakoutAfterTouches(window []model.Kline, future []model.Kline, level model.PriceLevel, touches []indexedTouch, minTouches int, options LevelOptions) (int, []indexedTouch) {
	series := append(append([]model.Kline{}, window...), future...)
	if len(series) == 0 {
		return -1, nil
	}
	for endTouch := minTouches - 1; endTouch < len(touches); endTouch++ {
		touchGroup := append([]indexedTouch{}, touches[endTouch-minTouches+1:endTouch+1]...)
		lastTouchIndex := touchGroup[len(touchGroup)-1].index
		breakoutIndex := findBreakoutIndexInRange(series, level, lastTouchIndex+1, lastTouchIndex+1+minTouches, options)
		if breakoutIndex >= 0 && !hasLimitedUpperBandPierces(series, level, lastTouchIndex+1, breakoutIndex) {
			continue
		}
		if breakoutIndex >= 0 {
			return breakoutIndex, touchGroup
		}
	}
	return -1, nil
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
	threshold := breakoutThreshold(level, options.BreakTolerance)
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
		confirmed := true
		for j := 0; j < confirmBars; j++ {
			if marketClose(klines[i+j]) <= threshold {
				confirmed = false
				break
			}
		}
		if !confirmed {
			continue
		}
		if !volumeBreakoutConfirmed(klines, i, options) {
			continue
		}
		return i
	}
	return -1
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
