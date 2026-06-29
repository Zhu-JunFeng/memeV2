package backtest

import (
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestDetectBandFollowReplayEntryAtCurrentBarAllowsOlderSlidingWindow(t *testing.T) {
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	klines := make([]model.Kline, 0, 15)
	klines = append(klines,
		model.Kline{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		model.Kline{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		model.Kline{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		model.Kline{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		model.Kline{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		model.Kline{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	)
	for minute := 6; minute < 14; minute++ {
		klines = append(klines, model.Kline{
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
	klines = append(klines, model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(14 * time.Minute),
		CloseTime:      base.Add(15 * time.Minute),
		MarketCapOpen:  8.5,
		MarketCapHigh:  11.2,
		MarketCapLow:   8.4,
		MarketCapClose: 10.95,
		Volume:         320,
	})

	options := LevelOptions{
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
	}

	if _, ok := DetectBandFollowEntryAtCurrentBar(klines, options); ok {
		t.Fatal("expected realtime entry detector to reject stale old-window breakout")
	}
	entry, ok := DetectBandFollowReplayEntryAtCurrentBar(klines, options)
	if !ok {
		t.Fatal("expected replay entry detector to keep older sliding-window breakout")
	}
	if !entry.Signal.SignalTime.Equal(base.Add(14 * time.Minute)) {
		t.Fatalf("expected replay entry on current bar, got %#v", entry)
	}
}

func TestCollectBandFollowReplayEntriesAllowsMultipleSlidingWindows(t *testing.T) {
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 18000, MarketCapHigh: 18800, MarketCapLow: 17600, MarketCapClose: 18200, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 18200, MarketCapHigh: 20800, MarketCapLow: 18000, MarketCapClose: 19600, Volume: 200},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 19600, MarketCapHigh: 19800, MarketCapLow: 18400, MarketCapClose: 18800, Volume: 120},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 18800, MarketCapHigh: 20900, MarketCapLow: 18600, MarketCapClose: 19700, Volume: 240},
		{OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 19700, MarketCapHigh: 19900, MarketCapLow: 18800, MarketCapClose: 19000, Volume: 140},
		{OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 19000, MarketCapHigh: 21000, MarketCapLow: 18900, MarketCapClose: 19800, Volume: 280},
		{OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 19800, MarketCapHigh: 22500, MarketCapLow: 19600, MarketCapClose: 21900, Volume: 320},
	}
	options := LevelOptions{PivotWindow: 1, PriceTolerance: 0.02, BreakTolerance: 0.01, ConfirmBars: 1, VolumeWindow: 3, VolumeMultiplier: 1.2, MaxLevels: 4, WindowSize: 6, WindowStep: 1, MinTouches: 3}

	entries, windows := CollectBandFollowReplayEntries(klines, options)
	if len(entries) == 0 {
		t.Fatal("expected replay collector to produce entries for backtest windows")
	}
	if len(windows) == 0 {
		t.Fatal("expected replay collector to preserve replay windows")
	}
}
