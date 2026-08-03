package backtest

import (
	"strings"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestBandExitTriggersFixedFivePercentStopIntrabar(t *testing.T) {
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Lower: 90,
		Upper: 105,
		Breakout: &model.BreakoutSetup{
			BuyPoint: &model.LevelAnchorPoint{Time: base, Price: 110},
		},
	}
	klines := []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapOpen: 108, MarketCapHigh: 112, MarketCapLow: 107, MarketCapClose: 110},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 110, MarketCapHigh: 111, MarketCapLow: 88, MarketCapClose: 89},
	}
	config := BreakoutBandFollowConfig{TakeProfitRate: 0.5, ActivationProfitRate: 0.4, LockedProfitRate: 0.2}

	beforeClose := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(90*time.Second))
	if !beforeClose.Triggered || beforeClose.ExitPoint == nil {
		t.Fatalf("expected intrabar fixed stop to trigger before close, got %#v", beforeClose)
	}
	expectedStop := 110 * (1 - FixedStopLossRate)
	if !almostEqual(beforeClose.ExitPoint.Price, expectedStop) {
		t.Fatalf("expected exit at fixed stop %.4f, got %#v", expectedStop, beforeClose.ExitPoint)
	}
	if !strings.Contains(beforeClose.Reason, "固定 5% 止损") {
		t.Fatalf("unexpected reason: %q", beforeClose.Reason)
	}
}

func TestBandExitDoesNotTriggerWhenLowStaysAboveFixedStop(t *testing.T) {
	base := time.Date(2026, 7, 12, 13, 0, 0, 0, time.UTC)
	level := model.PriceLevel{Lower: 90, Upper: 105, Breakout: &model.BreakoutSetup{BuyPoint: &model.LevelAnchorPoint{Time: base, Price: 110}}}
	klines := []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapClose: 110, MarketCapHigh: 112, MarketCapLow: 107},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 110, MarketCapHigh: 111, MarketCapLow: 105, MarketCapClose: 92},
	}
	config := BreakoutBandFollowConfig{TakeProfitRate: 0.5, ActivationProfitRate: 0.4, LockedProfitRate: 0.2}
	decision := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(2*time.Minute))
	if decision.Triggered {
		t.Fatalf("expected low above fixed stop to remain open, got %#v", decision)
	}
}

func TestBandExitChecksEveryBarAfterEntryForFixedStop(t *testing.T) {
	base := time.Date(2026, 7, 15, 6, 4, 0, 0, time.UTC)
	level := model.PriceLevel{Lower: 90, Breakout: &model.BreakoutSetup{BuyPoint: &model.LevelAnchorPoint{Time: base, Price: 110}}}
	klines := []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapClose: 110, MarketCapHigh: 112, MarketCapLow: 107},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 110, MarketCapHigh: 111, MarketCapLow: 105, MarketCapClose: 106},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 106, MarketCapHigh: 107, MarketCapLow: 104, MarketCapClose: 106},
	}
	config := BreakoutBandFollowConfig{TakeProfitRate: 0.5, ActivationProfitRate: 0.4, LockedProfitRate: 0.2}

	beforeThirdClose := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(2*time.Minute+30*time.Second))
	if !beforeThirdClose.Triggered || beforeThirdClose.ExitPoint == nil {
		t.Fatalf("expected current forming bar fixed stop to trigger, got %#v", beforeThirdClose)
	}
	expectedStop := 110 * (1 - FixedStopLossRate)
	if !almostEqual(beforeThirdClose.ExitPoint.Price, expectedStop) {
		t.Fatalf("expected exit at fixed stop %.4f, got %#v", expectedStop, beforeThirdClose.ExitPoint)
	}
	if beforeThirdClose.HoldingBars != 2 {
		t.Fatalf("expected exit after two holding bars, got %d", beforeThirdClose.HoldingBars)
	}
}
