package backtest

import (
	"strings"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestBandExitWaitsForCloseBelowLowerBand(t *testing.T) {
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
	if beforeClose.Triggered {
		t.Fatalf("expected next-bar lower-band exit to wait for close, got %#v", beforeClose)
	}
	afterClose := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(2*time.Minute))
	if !afterClose.Triggered || afterClose.ExitPoint == nil {
		t.Fatalf("expected closed next bar below lower band to exit, got %#v", afterClose)
	}
	if afterClose.ExitPoint.Price != 89 {
		t.Fatalf("expected exit at next-bar close 89, got %#v", afterClose.ExitPoint)
	}
	if !strings.Contains(afterClose.Reason, "收盘市值低于压力带下沿") {
		t.Fatalf("unexpected reason: %q", afterClose.Reason)
	}
}

func TestBandExitDoesNotTriggerWhenCloseRecoversAboveLowerBand(t *testing.T) {
	base := time.Date(2026, 7, 12, 13, 0, 0, 0, time.UTC)
	level := model.PriceLevel{Lower: 90, Upper: 105, Breakout: &model.BreakoutSetup{BuyPoint: &model.LevelAnchorPoint{Time: base, Price: 110}}}
	klines := []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapClose: 110, MarketCapHigh: 112, MarketCapLow: 107},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 110, MarketCapHigh: 111, MarketCapLow: 85, MarketCapClose: 92},
	}
	config := BreakoutBandFollowConfig{TakeProfitRate: 0.5, ActivationProfitRate: 0.4, LockedProfitRate: 0.2}
	decision := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(2*time.Minute))
	if decision.Triggered {
		t.Fatalf("expected recovered next-bar close above lower band to remain open, got %#v", decision)
	}
}

func TestBandExitChecksEveryClosedBarAfterEntry(t *testing.T) {
	base := time.Date(2026, 7, 15, 6, 4, 0, 0, time.UTC)
	level := model.PriceLevel{Lower: 90, Breakout: &model.BreakoutSetup{BuyPoint: &model.LevelAnchorPoint{Time: base, Price: 110}}}
	klines := []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), MarketCapClose: 110, MarketCapHigh: 112, MarketCapLow: 107},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 110, MarketCapHigh: 111, MarketCapLow: 91, MarketCapClose: 92},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 92, MarketCapHigh: 93, MarketCapLow: 87, MarketCapClose: 89},
	}
	config := BreakoutBandFollowConfig{TakeProfitRate: 0.5, ActivationProfitRate: 0.4, LockedProfitRate: 0.2}

	beforeThirdClose := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(2*time.Minute+30*time.Second))
	if beforeThirdClose.Triggered {
		t.Fatalf("expected later bar to wait for close, got %#v", beforeThirdClose)
	}
	afterThirdClose := evaluateRealtimeBandFollowExit(klines, 0, level, config, base.Add(3*time.Minute))
	if !afterThirdClose.Triggered || afterThirdClose.ExitPoint == nil || afterThirdClose.ExitPoint.Price != 89 {
		t.Fatalf("expected third closed bar below lower band to exit at 89, got %#v", afterThirdClose)
	}
	if afterThirdClose.HoldingBars != 2 {
		t.Fatalf("expected exit after two holding bars, got %d", afterThirdClose.HoldingBars)
	}
}
