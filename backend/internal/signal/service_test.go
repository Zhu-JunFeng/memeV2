package signal

import (
	"context"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

type fakeKlineSource struct {
	items []model.Kline
}

func (f fakeKlineSource) GetKlines(context.Context, datasource.KlineQuery) ([]model.Kline, error) {
	return append([]model.Kline{}, f.items...), nil
}

type capturePublisher struct {
	signals []backtest.RealtimeScenarioSignal
}

func (c *capturePublisher) PublishRealtimeSignals(_ context.Context, signals []backtest.RealtimeScenarioSignal) error {
	c.signals = append([]backtest.RealtimeScenarioSignal{}, signals...)
	return nil
}

func TestSignalServicePublishesRealtimeSignals(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	history := []model.Kline{
		{Interval: "1m", OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.0, MarketCapHigh: 9.4, MarketCapLow: 8.8, MarketCapClose: 9.1, Volume: 100},
		{Interval: "1m", OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 9.1, MarketCapHigh: 10.4, MarketCapLow: 9.0, MarketCapClose: 9.8, Volume: 200},
		{Interval: "1m", OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 9.9, MarketCapLow: 9.2, MarketCapClose: 9.4, Volume: 120},
		{Interval: "1m", OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 9.4, MarketCapHigh: 10.45, MarketCapLow: 9.3, MarketCapClose: 9.85, Volume: 240},
		{Interval: "1m", OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 9.85, MarketCapHigh: 9.95, MarketCapLow: 9.4, MarketCapClose: 9.5, Volume: 140},
		{Interval: "1m", OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 9.5, MarketCapHigh: 10.5, MarketCapLow: 9.45, MarketCapClose: 9.9, Volume: 280},
	}
	current := &model.Kline{
		Interval:       "1m",
		OpenTime:       base.Add(6 * time.Minute),
		CloseTime:      base.Add(7 * time.Minute),
		MarketCapOpen:  9.9,
		MarketCapHigh:  11.2,
		MarketCapLow:   9.8,
		MarketCapClose: 10.95,
		Volume:         320,
	}
	pub := &capturePublisher{}
	svc := NewService(fakeKlineSource{items: history}, pub)
	result, err := svc.DetectRealtimeSignals(context.Background(), RealtimeRequest{
		TokenAddress: "token",
		Interval:     "1m",
		StartTime:    base,
		EndTime:      base.Add(6 * time.Minute),
		LevelOptions: backtest.LevelOptions{
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
		},
		CurrentKline: current,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(result.Signals) == 0 {
		t.Fatalf("expected realtime signals, got %#v", result)
	}
	if len(pub.signals) == 0 {
		t.Fatalf("expected publisher to receive signals, got %#v", pub.signals)
	}
}
