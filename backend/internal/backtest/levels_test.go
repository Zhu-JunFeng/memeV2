package backtest

import (
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestCalculateSupportResistanceFindsLevels(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	prices := []struct{ high, low, close float64 }{
		{10, 8, 9}, {11, 8.1, 10}, {12, 7, 11}, {11, 8.2, 10}, {10, 8, 9},
		{13, 9, 12}, {15, 10, 14}, {13.8, 9.5, 11}, {12, 8.2, 9}, {11, 7.1, 8},
		{12, 8, 11}, {15.2, 10, 14}, {13, 9, 12}, {11, 8, 9}, {10, 7.2, 8},
	}
	klines := makeKlines(base, time.Minute, "1m", prices)
	levels := CalculateSupportResistance(klines, LevelOptions{PivotWindow: 2, PriceTolerance: 0.03, MaxLevels: 4})
	if len(levels) == 0 {
		t.Fatal("expected at least one level")
	}
	hasSupport := false
	hasResistance := false
	for _, level := range levels {
		if level.Type == model.LevelTypeSupport {
			hasSupport = true
		}
		if level.Type == model.LevelTypeResistance {
			hasResistance = true
		}
		if level.Touches == 0 || level.Score == 0 || level.Lower >= level.Upper {
			t.Fatalf("expected scored price band with touches, got %#v", level)
		}
	}
	if !hasSupport || !hasResistance {
		t.Fatalf("expected support and resistance levels, got %#v", levels)
	}
}

func TestCalculateSupportResistanceWorksAcrossIntervals(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	prices := []struct{ high, low, close float64 }{
		{10, 7, 9}, {11, 8, 10}, {12, 9, 11}, {11, 8, 10}, {10, 7.1, 9},
		{13, 9, 12}, {16, 11, 15}, {14, 10, 13}, {12, 7.2, 9}, {11, 8, 10},
		{13, 9, 12}, {16.2, 11, 15}, {15, 10, 14}, {13, 8, 12}, {12, 7.3, 9},
	}
	minuteLevels := CalculateSupportResistance(makeKlines(base, time.Minute, "1m", prices), LevelOptions{PivotWindow: 2, PriceTolerance: 0.03, MaxLevels: 4})
	hourLevels := CalculateSupportResistance(makeKlines(base, time.Hour, "1h", prices), LevelOptions{PivotWindow: 2, PriceTolerance: 0.03, MaxLevels: 4})
	if len(minuteLevels) == 0 || len(hourLevels) == 0 {
		t.Fatalf("expected levels for both intervals, minute=%#v hour=%#v", minuteLevels, hourLevels)
	}
	if countByType(minuteLevels, model.LevelTypeSupport) == 0 || countByType(hourLevels, model.LevelTypeSupport) == 0 {
		t.Fatalf("expected support levels across intervals, minute=%#v hour=%#v", minuteLevels, hourLevels)
	}
	if countByType(minuteLevels, model.LevelTypeResistance) == 0 || countByType(hourLevels, model.LevelTypeResistance) == 0 {
		t.Fatalf("expected resistance levels across intervals, minute=%#v hour=%#v", minuteLevels, hourLevels)
	}
}

func TestCalculateSupportResistanceFlipsBrokenResistanceToSupport(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	prices := []struct{ high, low, close float64 }{
		{10, 8, 9}, {11, 8.5, 10}, {12, 9, 11}, {11, 8.8, 10}, {10, 8, 9},
		{12.2, 9.2, 11}, {13, 10, 12.5}, {12.1, 9.5, 11}, {13.5, 10.5, 13},
		{15, 12, 14.5}, {16, 13, 15.5}, {17, 14, 16.5}, {18, 15, 17.5},
	}
	levels := CalculateSupportResistance(makeKlines(base, time.Minute, "1m", prices), LevelOptions{PivotWindow: 2, PriceTolerance: 0.03, VolumeMultiplier: 1.0, MaxLevels: 8})
	for _, level := range levels {
		if level.Price >= 11.5 && level.Price <= 12.5 && level.Type == model.LevelTypeSupport {
			return
		}
	}
	t.Fatalf("expected old resistance near 12 to become support after price breaks above it, got %#v", levels)
}

func TestCalculateSupportResistanceSkipsLowVolumePressureCandles(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.3, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.8, MarketCapLow: 9.0, MarketCapClose: 10.2, Volume: 20},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.2, MarketCapHigh: 10.3, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 110},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.9, MarketCapLow: 9.3, MarketCapClose: 10.4, Volume: 180},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 10.4, MarketCapHigh: 10.5, MarketCapLow: 9.5, MarketCapClose: 9.6, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.6, MarketCapHigh: 10.2, MarketCapLow: 9.4, MarketCapClose: 9.8, Volume: 130},
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 10.0, MarketCapLow: 9.2, MarketCapClose: 9.5, Volume: 125},
	}
	levels := CalculateSupportResistance(klines, LevelOptions{
		PivotWindow:      1,
		PriceTolerance:   0.03,
		VolumeWindow:     3,
		VolumeMultiplier: 1.2,
		MaxLevels:        4,
	})
	for _, level := range levels {
		if level.Type == model.LevelTypeResistance && level.Price > 10.7 && level.Price < 10.85 {
			t.Fatalf("expected low-volume pressure candle near 10.8 to be excluded, got %#v", level)
		}
	}
	foundQualifiedResistance := false
	for _, level := range levels {
		if level.Type == model.LevelTypeResistance && level.Price >= 10.85 {
			foundQualifiedResistance = true
		}
	}
	if !foundQualifiedResistance {
		t.Fatalf("expected higher-volume bullish pressure candle to remain, got %#v", levels)
	}
}

func TestCalculateSupportResistanceByWindowsAnnotatesBreakoutSetup(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 8.9, MarketCapHigh: 9.2, MarketCapLow: 8.6, MarketCapClose: 9.0, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.8, MarketCapLow: 8.9, MarketCapClose: 9.4, Volume: 110},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.5, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 210},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.1, MarketCapClose: 9.4, Volume: 130},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.7, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.7, MarketCapHigh: 9.8, MarketCapLow: 9.0, MarketCapClose: 9.3, Volume: 150},
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.2, MarketCapHigh: 10.45, MarketCapLow: 9.1, MarketCapClose: 9.8, Volume: 280},
		{Interval: "1m", OpenTime: base.Add(7 * time.Minute), CloseTime: base.Add(8 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.95, MarketCapLow: 9.1, MarketCapClose: 9.4, Volume: 170},
		{Interval: "1m", OpenTime: base.Add(8 * time.Minute), CloseTime: base.Add(9 * time.Minute), MarketCapOpen: 9.7, MarketCapHigh: 11.4, MarketCapLow: 9.7, MarketCapClose: 11.2, Volume: 320},
		{Interval: "1m", OpenTime: base.Add(9 * time.Minute), CloseTime: base.Add(10 * time.Minute), MarketCapOpen: 11.2, MarketCapHigh: 11.6, MarketCapLow: 10.9, MarketCapClose: 11.4, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(10 * time.Minute), CloseTime: base.Add(11 * time.Minute), MarketCapOpen: 11.4, MarketCapHigh: 11.8, MarketCapLow: 11.1, MarketCapClose: 11.6, Volume: 230},
		{Interval: "1m", OpenTime: base.Add(11 * time.Minute), CloseTime: base.Add(12 * time.Minute), MarketCapOpen: 11.6, MarketCapHigh: 12.0, MarketCapLow: 11.3, MarketCapClose: 11.8, Volume: 240},
	}
	results := CalculateSupportResistanceByWindows(
		klines,
		LevelOptions{
			PivotWindow:      1,
			PriceTolerance:   0.02,
			BreakTolerance:   0.01,
			ConfirmBars:      1,
			VolumeWindow:     1,
			VolumeMultiplier: 0.5,
			MaxLevels:        4,
			WindowSize:       8,
			WindowStep:       1,
			MinTouches:       3,
			EntryOffsetBars:  1,
			TakeProfitRR:     1.5,
			MaxHoldBars:      3,
		},
		8,
		1,
	)
	if len(results) == 0 {
		t.Fatal("expected at least one window result")
	}
	for _, result := range results {
		for _, level := range result.Levels {
			if level.Calculation.ResistanceVotes == 0 || level.Breakout == nil || !level.Breakout.Triggered {
				continue
			}
			if level.Breakout.Consolidation == nil {
				t.Fatalf("expected consolidation zone, got %#v", level.Breakout)
			}
			if level.Breakout.Consolidation.BarCount == 0 || level.Breakout.Consolidation.TouchCount < 3 {
				t.Fatalf("expected meaningful consolidation zone, got %#v", level.Breakout.Consolidation)
			}
			if level.Breakout.BreakoutPoint == nil {
				t.Fatalf("expected breakout point, got %#v", level.Breakout)
			}
			return
		}
	}
	t.Fatalf("expected breakout setup in any deduped window, got %#v", results)
}

func TestCalculateSupportResistanceByWindowsSkipsLowVolumeRetestTouches(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.3, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.45, MarketCapLow: 9.0, MarketCapClose: 9.9, Volume: 105},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 10.40, MarketCapLow: 9.3, MarketCapClose: 10.0, Volume: 110},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.48, MarketCapLow: 9.5, MarketCapClose: 10.1, Volume: 115},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 10.1, MarketCapHigh: 11.3, MarketCapLow: 10.0, MarketCapClose: 11.0, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 11.0, MarketCapHigh: 11.5, MarketCapLow: 10.9, MarketCapClose: 11.2, Volume: 230},
	}
	results := CalculateSupportResistanceByWindows(
		klines,
		LevelOptions{
			PivotWindow:      1,
			PriceTolerance:   0.02,
			BreakTolerance:   0.01,
			ConfirmBars:      1,
			VolumeWindow:     3,
			VolumeMultiplier: 1.2,
			MaxLevels:        4,
			WindowSize:       6,
			WindowStep:       1,
			MinTouches:       3,
			EntryOffsetBars:  1,
			TakeProfitRR:     1.5,
			MaxHoldBars:      3,
		},
		6,
		1,
	)
	for _, result := range results {
		for _, level := range result.Levels {
			if level.Breakout != nil {
				t.Fatalf("expected low-volume retest touches to block breakout setup, got %#v", level.Breakout)
			}
		}
	}
}

func TestCalculateSupportResistanceByWindowsSkipsScenarioWithMultipleUpperPierces(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 10.9, MarketCapLow: 9.4, MarketCapClose: 9.7, Volume: 180},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.7, MarketCapHigh: 10.5, MarketCapLow: 9.5, MarketCapClose: 9.9, Volume: 280},
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 10.95, MarketCapLow: 9.8, MarketCapClose: 9.95, Volume: 190},
		{Interval: "1m", OpenTime: base.Add(7 * time.Minute), CloseTime: base.Add(8 * time.Minute), MarketCapOpen: 9.95, MarketCapHigh: 11.2, MarketCapLow: 9.9, MarketCapClose: 10.95, Volume: 320},
	}
	results := CalculateSupportResistanceByWindows(
		klines,
		LevelOptions{
			PivotWindow:      1,
			PriceTolerance:   0.02,
			BreakTolerance:   0.01,
			ConfirmBars:      1,
			VolumeWindow:     3,
			VolumeMultiplier: 1.2,
			MaxLevels:        4,
			WindowSize:       8,
			WindowStep:       1,
			MinTouches:       3,
			EntryOffsetBars:  1,
			TakeProfitRR:     1.5,
			MaxHoldBars:      3,
		},
		8,
		1,
	)
	for _, result := range results {
		for _, level := range result.Levels {
			if level.Breakout != nil {
				t.Fatalf("expected scenario with two upper-band pierces to be skipped, got %#v", level.Breakout)
			}
		}
	}
}

func TestCalculateSupportResistanceByWindowsSkipsScenarioWithTooManyClosesAboveUpperBetweenFirstTouchAndBreakout(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 10.7, Volume: 200},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.7, MarketCapHigh: 10.8, MarketCapLow: 9.8, MarketCapClose: 10.75, Volume: 210},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 10.75, MarketCapHigh: 10.85, MarketCapLow: 9.9, MarketCapClose: 10.8, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 10.8, MarketCapHigh: 10.45, MarketCapLow: 9.7, MarketCapClose: 10.72, Volume: 230},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 10.72, MarketCapHigh: 10.5, MarketCapLow: 9.6, MarketCapClose: 9.9, Volume: 280},
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 11.2, MarketCapLow: 9.8, MarketCapClose: 10.95, Volume: 320},
	}
	results := CalculateSupportResistanceByWindows(
		klines,
		LevelOptions{
			PivotWindow:      1,
			PriceTolerance:   0.02,
			BreakTolerance:   0.01,
			ConfirmBars:      1,
			VolumeWindow:     3,
			VolumeMultiplier: 1.2,
			MaxLevels:        4,
			WindowSize:       7,
			WindowStep:       1,
			MinTouches:       3,
			EntryOffsetBars:  1,
			TakeProfitRR:     1.5,
			MaxHoldBars:      3,
		},
		7,
		1,
	)
	for _, result := range results {
		for _, level := range result.Levels {
			if level.Breakout != nil {
				t.Fatalf("expected scenario with more than 2 closes above upper between first touch and breakout to be skipped, got %#v", level.Breakout)
			}
		}
	}
}

func TestHasTooManyClosesAboveUpperUntilBreakout(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	series := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), MarketCapClose: 9.8},
		{OpenTime: base.Add(1 * time.Minute), MarketCapClose: 10.1},
		{OpenTime: base.Add(2 * time.Minute), MarketCapClose: 10.3},
		{OpenTime: base.Add(3 * time.Minute), MarketCapClose: 10.25},
		{OpenTime: base.Add(4 * time.Minute), MarketCapClose: 10.5},
	}
	group := breakoutTouchGroup{
		touches: []indexedTouch{
			{index: 0},
			{index: 1},
			{index: 2},
		},
		lastTouchIndex: 2,
	}
	level := model.PriceLevel{Upper: 10.0}
	if !hasTooManyClosesAboveUpperUntilBreakout(series, level, group, 4, 2) {
		t.Fatal("expected more than 2 closes above upper between first touch and breakout")
	}
}

func TestCalculateSupportResistanceByWindowsFindsBreakoutInsideWindow(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 11.2, MarketCapLow: 9.8, MarketCapClose: 10.95, Volume: 320},
		{Interval: "1m", OpenTime: base.Add(7 * time.Minute), CloseTime: base.Add(8 * time.Minute), MarketCapOpen: 10.95, MarketCapHigh: 11.3, MarketCapLow: 10.8, MarketCapClose: 11.1, Volume: 220},
	}
	results := CalculateSupportResistanceByWindows(
		klines,
		LevelOptions{
			PivotWindow:      1,
			PriceTolerance:   0.02,
			BreakTolerance:   0.01,
			ConfirmBars:      1,
			VolumeWindow:     1,
			VolumeMultiplier: 0.5,
			MaxLevels:        4,
			WindowSize:       8,
			WindowStep:       1,
			MinTouches:       3,
			EntryOffsetBars:  1,
			TakeProfitRR:     1.5,
			MaxHoldBars:      3,
		},
		8,
		1,
	)
	if len(results) == 0 {
		t.Fatal("expected window results")
	}
	for _, level := range results[0].Levels {
		if level.Breakout == nil {
			continue
		}
		if level.Breakout.BreakoutPoint == nil {
			continue
		}
		if !level.Breakout.BreakoutPoint.Time.Equal(base.Add(6 * time.Minute)) {
			t.Fatalf("expected breakout inside window at minute 6, got %#v", level.Breakout.BreakoutPoint)
		}
		if level.Breakout.BuyPoint == nil || !level.Breakout.BuyPoint.Time.Equal(level.Breakout.BreakoutPoint.Time) {
			t.Fatalf("expected buy point to equal breakout point, breakout=%#v buy=%#v", level.Breakout.BreakoutPoint, level.Breakout.BuyPoint)
		}
		expectedBreakoutPrice := breakoutThreshold(level, 0.01)
		if !levelsAlmostEqual(level.Breakout.BuyPoint.Price, expectedBreakoutPrice) {
			t.Fatalf("expected buy point price %.4f, got %#v", expectedBreakoutPrice, level.Breakout.BuyPoint)
		}
		return
	}
	t.Fatalf("expected in-window breakout setup, got %#v", results[0].Levels)
}

func TestCalculateRealtimeScenarioSignalsByWindowsDetectsLatestBreakout(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	history := []model.Kline{
		{Interval: "5m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "5m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(10 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{Interval: "5m", OpenTime: base.Add(10 * time.Minute), CloseTime: base.Add(15 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "5m", OpenTime: base.Add(15 * time.Minute), CloseTime: base.Add(20 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "5m", OpenTime: base.Add(20 * time.Minute), CloseTime: base.Add(25 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{Interval: "5m", OpenTime: base.Add(25 * time.Minute), CloseTime: base.Add(30 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	}
	current := model.Kline{
		Interval:       "5m",
		OpenTime:       base.Add(30 * time.Minute),
		CloseTime:      base.Add(35 * time.Minute),
		MarketCapOpen:  9.9,
		MarketCapHigh:  11.2,
		MarketCapLow:   9.8,
		MarketCapClose: 10.95,
		Volume:         320,
	}
	result := CalculateRealtimeScenarioSignalsByWindows(history, current, LevelOptions{
		PivotWindow:      1,
		PriceTolerance:   0.02,
		BreakTolerance:   0.01,
		ConfirmBars:      1,
		VolumeWindow:     3,
		VolumeMultiplier: 1.2,
		MaxLevels:        4,
		WindowSize:       6,
		WindowStep:       1,
		MinTouches:       3,
	}, 6, 1, pressureBreakoutDetector())
	if len(result.Signals) == 0 {
		t.Fatalf("expected realtime breakout signal, got %#v", result)
	}
	if result.Signals[0].ScenarioCode != "pressure_breakout" {
		t.Fatalf("expected pressure_breakout signal, got %#v", result.Signals[0])
	}
}

func TestCalculateRealtimeScenarioSignalsByWindowsUsesRetestLookbackBeyondLevelWindow(t *testing.T) {
	base := time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC)
	history := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 10.4, MarketCapLow: 8.6, MarketCapClose: 9.8, Volume: 260},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 8.8, MarketCapClose: 9.2, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.2, MarketCapHigh: 9.7, MarketCapLow: 8.5, MarketCapClose: 9.1, Volume: 130},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.45, MarketCapLow: 8.7, MarketCapClose: 9.85, Volume: 280},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 8.6, MarketCapClose: 9.1, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.5, MarketCapLow: 8.8, MarketCapClose: 9.9, Volume: 300},
	}
	current := model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(6 * time.Minute),
		CloseTime:      base.Add(7 * time.Minute),
		MarketCapOpen:  9.9,
		MarketCapHigh:  12.0,
		MarketCapLow:   9.8,
		MarketCapClose: 11.8,
		Volume:         360,
	}
	result := CalculateRealtimeScenarioSignalsByWindows(history, current, LevelOptions{
		PivotWindow:        1,
		PriceTolerance:     0.02,
		BreakTolerance:     0.01,
		ConfirmBars:        1,
		VolumeWindow:       3,
		VolumeMultiplier:   1.0,
		MaxLevels:          4,
		WindowSize:         4,
		WindowStep:         1,
		LevelWindowSize:    4,
		LevelWindowStep:    1,
		MinTouches:         3,
		MinWindowRange:     0.08,
		MinLevelSpace:      0.06,
		MinRetestPullback:  0.03,
		MinRetestSpanBars:  4,
		RetestLookbackBars: 7,
	}, 4, 1, pressureBreakoutDetector())
	if len(result.Signals) == 0 {
		t.Fatalf("expected long retest lookback to preserve earlier touch, got %#v", result)
	}
}

func TestDetectRealtimeBreakoutSignalSkipsNarrowRangeFakePressure(t *testing.T) {
	base := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	window := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 99.6, MarketCapHigh: 99.9, MarketCapLow: 99.3, MarketCapClose: 99.7, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 99.7, MarketCapHigh: 100.2, MarketCapLow: 99.5, MarketCapClose: 100.0, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 100.0, MarketCapHigh: 100.1, MarketCapLow: 99.4, MarketCapClose: 99.7, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 99.7, MarketCapHigh: 100.25, MarketCapLow: 99.5, MarketCapClose: 100.05, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 100.05, MarketCapHigh: 100.15, MarketCapLow: 99.45, MarketCapClose: 99.8, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 99.8, MarketCapHigh: 100.3, MarketCapLow: 99.55, MarketCapClose: 100.08, Volume: 280},
	}
	current := model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(6 * time.Minute),
		CloseTime:      base.Add(7 * time.Minute),
		MarketCapOpen:  100.08,
		MarketCapHigh:  101.7,
		MarketCapLow:   100.0,
		MarketCapClose: 101.5,
		Volume:         340,
	}
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 100.1,
		Lower: 99.9,
		Upper: 100.3,
		Calculation: model.LevelCalculation{
			ResistanceVotes: 3,
		},
	}
	signal := detectRealtimeBreakoutSignal(level, window, current, LevelOptions{
		BreakTolerance:    0.01,
		VolumeWindow:      3,
		VolumeMultiplier:  1.0,
		MinTouches:        3,
		MinWindowRange:    0.08,
		MinLevelSpace:     0.06,
		MinRetestPullback: 0.03,
		MinRetestSpanBars: 4,
	})
	if signal != nil {
		t.Fatalf("expected narrow-range fake pressure to be skipped, got %#v", signal)
	}
}

func TestCalculateRealtimeScenarioSignalsByWindowsSkipsStaleOldWindowBreakout(t *testing.T) {
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	history := make([]model.Kline, 0, 14)
	history = append(history,
		model.Kline{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		model.Kline{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		model.Kline{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		model.Kline{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		model.Kline{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		model.Kline{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	)
	for minute := 6; minute < 14; minute++ {
		history = append(history, model.Kline{
			Interval:       "1m",
			OpenTime:       base.Add(time.Duration(minute) * time.Minute),
			CloseTime:      base.Add(time.Duration(minute+1) * time.Minute),
			MarketCapOpen:  8.6,
			MarketCapHigh:  8.9,
			MarketCapLow:   8.2,
			MarketCapClose: 8.5,
			Volume:         90,
		})
	}
	current := model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(14 * time.Minute),
		CloseTime:      base.Add(15 * time.Minute),
		MarketCapOpen:  8.5,
		MarketCapHigh:  11.2,
		MarketCapLow:   8.4,
		MarketCapClose: 10.95,
		Volume:         320,
	}
	result := CalculateRealtimeScenarioSignalsByWindows(history, current, LevelOptions{
		PivotWindow:      1,
		PriceTolerance:   0.02,
		BreakTolerance:   0.01,
		ConfirmBars:      1,
		VolumeWindow:     3,
		VolumeMultiplier: 1.2,
		MaxLevels:        4,
		WindowSize:       6,
		WindowStep:       1,
		MinTouches:       3,
	}, 6, 1, pressureBreakoutDetector())
	if len(result.Signals) != 0 {
		t.Fatalf("expected stale old-window breakout to be skipped, got %#v", result.Signals)
	}
}

func TestDetectRealtimeBreakoutSignalUsesFirstBreakoutAfterThirdTouch(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.2, MarketCapLow: 8.8, MarketCapClose: 9.0, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 10.4, MarketCapLow: 8.9, MarketCapClose: 9.8, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
		// 第 4 次试压先出现，但当前 bar 仍然应该归属于“试压1/2/3 之后的第一次有效突破”。
		{Interval: "1m", OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 9.9, MarketCapHigh: 10.48, MarketCapLow: 9.8, MarketCapClose: 9.96, Volume: 300},
	}
	current := model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(7 * time.Minute),
		CloseTime:      base.Add(8 * time.Minute),
		MarketCapOpen:  9.96,
		MarketCapHigh:  11.2,
		MarketCapLow:   9.9,
		MarketCapClose: 10.95,
		Volume:         320,
	}
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.3,
		Lower: 10.25,
		Upper: 10.5,
		Calculation: model.LevelCalculation{
			ResistanceVotes: 3,
		},
	}
	signal := detectRealtimeBreakoutSignal(level, window, current, LevelOptions{
		PriceTolerance:   0.02,
		BreakTolerance:   0.01,
		ConfirmBars:      1,
		VolumeWindow:     3,
		VolumeMultiplier: 1.0,
		MinTouches:       3,
	})
	if signal == nil || signal.Breakout == nil {
		t.Fatalf("expected realtime breakout signal, got %#v", signal)
	}
	failedTouches := signal.Breakout.FailedTouches
	if len(failedTouches) != 3 {
		t.Fatalf("expected 3 failed touches, got %#v", failedTouches)
	}
	expected := []time.Time{
		base.Add(1 * time.Minute),
		base.Add(3 * time.Minute),
		base.Add(5 * time.Minute),
	}
	for index, expectTime := range expected {
		if !failedTouches[index].Time.Equal(expectTime) {
			t.Fatalf("expected touch %d at %s, got %#v", index+1, expectTime, failedTouches)
		}
	}
	if signal.Breakout.BuyPoint == nil || !signal.Breakout.BuyPoint.Time.Equal(current.OpenTime) {
		t.Fatalf("expected current bar to be buy point, got %#v", signal.Breakout.BuyPoint)
	}
}

func TestPressureBreakoutScenarioOutputsResistanceLevel(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	window := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.2, MarketCapLow: 8.8, MarketCapClose: 9.0, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 10.4, MarketCapLow: 8.9, MarketCapClose: 9.8, Volume: 220},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	}
	current := model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(6 * time.Minute),
		CloseTime:      base.Add(7 * time.Minute),
		MarketCapOpen:  9.9,
		MarketCapHigh:  11.2,
		MarketCapLow:   9.8,
		MarketCapClose: 10.95,
		Volume:         320,
	}
	levels := []model.PriceLevel{{
		Type:  model.LevelTypeSupport,
		Price: 10.3,
		Lower: 10.25,
		Upper: 10.5,
		Calculation: model.LevelCalculation{
			SupportVotes:    4,
			ResistanceVotes: 3,
		},
	}}

	signals := pressureBreakoutDetector().DetectRealtimeSignals(levels, window, current, LevelOptions{
		PriceTolerance:   0.02,
		BreakTolerance:   0.01,
		ConfirmBars:      1,
		VolumeWindow:     3,
		VolumeMultiplier: 1.0,
		MinTouches:       3,
	})
	if len(signals) != 1 {
		t.Fatalf("expected one pressure breakout signal, got %#v", signals)
	}
	if signals[0].LevelType != model.LevelTypeResistance {
		t.Fatalf("expected signal level type resistance, got %s", signals[0].LevelType)
	}
	if levels[0].Type != model.LevelTypeResistance {
		t.Fatalf("expected annotated level type resistance, got %s", levels[0].Type)
	}
}

func TestDedupeBreakoutsByKlineSignatureKeepsHighestScore(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	duplicateBreakout := &model.BreakoutSetup{
		Triggered: true,
		FailedTouches: []model.LevelAnchorPoint{
			{Time: base.Add(1 * time.Minute), Price: 10},
			{Time: base.Add(2 * time.Minute), Price: 10.1},
			{Time: base.Add(3 * time.Minute), Price: 10.2},
		},
		BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(4 * time.Minute), Price: 11},
	}
	windows := []WindowLevelResult{
		{WindowIndex: 1, Levels: []model.PriceLevel{{Type: model.LevelTypeResistance, Price: 10, Score: 4, Breakout: duplicateBreakout}}},
		{WindowIndex: 2, Levels: []model.PriceLevel{{Type: model.LevelTypeResistance, Price: 10.2, Score: 8, Breakout: duplicateBreakout}}},
	}
	dedupeBreakoutsByKlineSignature(windows)
	if windows[0].Levels[0].Breakout != nil {
		t.Fatalf("expected lower-score duplicate breakout to be removed, got %#v", windows[0].Levels[0].Breakout)
	}
	if windows[1].Levels[0].Breakout == nil {
		t.Fatal("expected highest-score breakout to be kept")
	}
}

func TestSelectTopLevelsKeepingBreakoutPrefersBreakoutVersionForSameBand(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	plain := model.PriceLevel{
		Type:  model.LevelTypeSupport,
		Price: 100,
		Lower: 98,
		Upper: 102,
		Score: 9,
	}
	withBreakout := plain
	withBreakout.Score = 7
	withBreakout.Breakout = &model.BreakoutSetup{
		Consolidation: &model.ConsolidationZone{
			StartTime: base.Add(-3 * time.Minute),
			EndTime:   base.Add(-1 * time.Minute),
		},
		BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 102},
	}
	selected := selectTopLevelsKeepingBreakout([]model.PriceLevel{plain, withBreakout}, 8)
	if len(selected) != 1 {
		t.Fatalf("expected duplicate band to collapse into one level, got %#v", selected)
	}
	if selected[0].Breakout == nil || selected[0].Breakout.BreakoutPoint == nil {
		t.Fatalf("expected breakout version to be kept, got %#v", selected[0])
	}
}

func makeKlines(base time.Time, step time.Duration, interval string, prices []struct{ high, low, close float64 }) []model.Kline {
	klines := make([]model.Kline, 0, len(prices))
	for i, price := range prices {
		openTime := base.Add(time.Duration(i) * step)
		openValue := price.close - 0.4
		if openValue <= 0 {
			openValue = price.low
		}
		klines = append(klines, model.Kline{
			Interval:       interval,
			OpenTime:       openTime,
			CloseTime:      openTime.Add(step),
			MarketCapOpen:  openValue,
			MarketCapHigh:  price.high,
			MarketCapLow:   price.low,
			MarketCapClose: price.close,
			Volume:         float64(100 + i*10),
		})
	}
	return klines
}

func countByType(levels []model.PriceLevel, levelType model.LevelType) int {
	count := 0
	for _, level := range levels {
		if level.Type == levelType {
			count++
		}
	}
	return count
}

func levelsAlmostEqual(left float64, right float64) bool {
	diff := left - right
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.0001
}
