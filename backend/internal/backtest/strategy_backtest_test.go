package backtest

import (
	"encoding/json"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestBreakoutBandFollowMethodStopsOnNextBarUpperBandBreak(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.5,
		Upper: 10.6,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(2 * time.Minute), Price: 10.9},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(2 * time.Minute), Price: 10.9},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 9.8, MarketCapHigh: 10.3, MarketCapLow: 9.7, MarketCapClose: 10.0, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.5, MarketCapLow: 9.9, MarketCapClose: 10.2, Volume: 110},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.2, MarketCapHigh: 11.1, MarketCapLow: 10.2, MarketCapClose: 10.9, Volume: 150},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 10.9, MarketCapHigh: 11.0, MarketCapLow: 10.5, MarketCapClose: 10.7, Volume: 120},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines: klines,
		Windows: []WindowLevelResult{
			{WindowIndex: 1, Levels: []model.PriceLevel{level}},
		},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{TakeProfitRate: 0.08, PositionSizeUSD: 10, HardStopLossRate: 0.05, ActivationProfitRate: 0.05, LockedProfitRate: 0.03}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected one trade, got %#v", result.Trades)
	}
	trade := result.Trades[0]
	if trade.Outcome != model.BreakoutOutcomeStopLoss {
		t.Fatalf("expected stop loss, got %#v", trade)
	}
	if trade.SellPoint.Price != 10.6 {
		t.Fatalf("expected sell at upper band 10.6, got %#v", trade.SellPoint)
	}
}

func TestBreakoutBandFollowMethodArmsTrailingStopAfterFivePercent(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.4,
		Upper: 10.5,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.4, MarketCapLow: 9.9, MarketCapClose: 10.1, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10.1, MarketCapHigh: 11.2, MarketCapLow: 10.1, MarketCapClose: 10.8, Volume: 150},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.8, MarketCapHigh: 11.5, MarketCapLow: 10.8, MarketCapClose: 11.3, Volume: 140},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 11.3, MarketCapHigh: 11.35, MarketCapLow: 11.1, MarketCapClose: 11.15, Volume: 130},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines: klines,
		Windows: []WindowLevelResult{
			{WindowIndex: 1, Levels: []model.PriceLevel{level}},
		},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{TakeProfitRate: 0.15, PositionSizeUSD: 10, HardStopLossRate: 0.05, ActivationProfitRate: 0.05, LockedProfitRate: 0.03}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	trade := result.Trades[0]
	if trade.Outcome != model.BreakoutOutcomeStopLoss {
		t.Fatalf("expected trailing stop loss, got %#v", trade)
	}
	expectedStop := 10.8 * 1.03
	if !almostEqual(trade.SellPoint.Price, expectedStop) {
		t.Fatalf("expected trailing stop %.4f, got %#v", expectedStop, trade.SellPoint)
	}
	if !trade.TrailingArmed {
		t.Fatalf("expected trailing stop to be armed, got %#v", trade)
	}
}

func TestBreakoutBandFollowMethodSupportsTakeProfitRangeAndFees(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.4,
		Upper: 10.5,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.4, MarketCapLow: 9.9, MarketCapClose: 10.1, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10.1, MarketCapHigh: 11.1, MarketCapLow: 10.1, MarketCapClose: 10.8, Volume: 150},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.8, MarketCapHigh: 12.7, MarketCapLow: 10.8, MarketCapClose: 12.2, Volume: 180},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines:  klines,
		Windows: []WindowLevelResult{{WindowIndex: 1, Levels: []model.PriceLevel{level}}},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{
		TakeProfitRateStart:  0.08,
		TakeProfitRateEnd:    0.10,
		TakeProfitRateStep:   0.01,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.05,
		LockedProfitRate:     0.03,
		FeeRate:              0.015,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(result.Groups) != 3 {
		t.Fatalf("expected 3 groups, got %#v", result.Groups)
	}
	firstTrade := result.Groups[0].Trades[0]
	if !almostEqual(firstTrade.GrossProfitRate, 0.08) {
		t.Fatalf("expected gross profit rate 0.08, got %#v", firstTrade)
	}
	if !almostEqual(firstTrade.ProfitRate, 0.065) {
		t.Fatalf("expected net profit rate 0.065 after fee, got %#v", firstTrade)
	}
	if !almostEqual(firstTrade.FeeUSD, 0.15) {
		t.Fatalf("expected fee usd 0.15, got %#v", firstTrade)
	}
}

func TestBreakoutBandFollowMethodStopsOnHardStopLoss(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.4,
		Upper: 10.5,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.4, MarketCapLow: 9.9, MarketCapClose: 10.1, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10.1, MarketCapHigh: 11.0, MarketCapLow: 10.1, MarketCapClose: 10.8, Volume: 150},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.8, MarketCapHigh: 10.95, MarketCapLow: 10.7, MarketCapClose: 10.9, Volume: 140},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 10.9, MarketCapHigh: 11.0, MarketCapLow: 10.2, MarketCapClose: 10.3, Volume: 130},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines:  klines,
		Windows: []WindowLevelResult{{WindowIndex: 1, Levels: []model.PriceLevel{level}}},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{
		TakeProfitRate:       0.15,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.08,
		LockedProfitRate:     0.03,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	trade := result.Trades[0]
	expectedStop := 10.8 * 0.95
	if trade.Outcome != model.BreakoutOutcomeStopLoss {
		t.Fatalf("expected hard stop loss, got %#v", trade)
	}
	if !almostEqual(trade.SellPoint.Price, expectedStop) {
		t.Fatalf("expected hard stop %.4f, got %#v", expectedStop, trade.SellPoint)
	}
}

func TestBreakoutBandFollowMethodUsesBreakoutBuyPriceForPnL(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	level := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 175.39,
		Upper: 180.0,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 183.54},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 183.54},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 170, MarketCapHigh: 176, MarketCapLow: 169, MarketCapClose: 174, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 174, MarketCapHigh: 200, MarketCapLow: 173, MarketCapClose: 199.70, Volume: 150},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 199.70, MarketCapHigh: 201, MarketCapLow: 189.72, MarketCapClose: 193, Volume: 140},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines:  klines,
		Windows: []WindowLevelResult{{WindowIndex: 1, Levels: []model.PriceLevel{level}}},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{
		TakeProfitRate:       0.15,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.05,
		LockedProfitRate:     0.03,
		FeeRate:              0.015,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	trade := result.Trades[0]
	if trade.Outcome != model.BreakoutOutcomeTimeout {
		t.Fatalf("expected timeout exit, got %#v", trade)
	}
	expectedGross := (193.0 - 183.54) / 183.54
	if !almostEqual(trade.GrossProfitRate, expectedGross) {
		t.Fatalf("expected gross profit %.6f, got %#v", expectedGross, trade)
	}
	expectedNet := expectedGross - 0.015
	if !almostEqual(trade.ProfitRate, expectedNet) {
		t.Fatalf("expected net profit %.6f, got %#v", expectedNet, trade)
	}
}

func TestBreakoutBandFollowMethodOnlyKeepsOnePositionOpenAtATime(t *testing.T) {
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	levelA := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.4,
		Upper: 10.5,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(1 * time.Minute), Price: 10.8},
		},
	}
	levelB := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 10.7,
		Upper: 10.8,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(2 * time.Minute), Price: 11.0},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(2 * time.Minute), Price: 11.0},
		},
	}
	levelC := model.PriceLevel{
		Type:  model.LevelTypeResistance,
		Price: 11.0,
		Upper: 11.1,
		Breakout: &model.BreakoutSetup{
			BreakoutPoint: &model.LevelAnchorPoint{Time: base.Add(5 * time.Minute), Price: 11.3},
			BuyPoint:      &model.LevelAnchorPoint{Time: base.Add(5 * time.Minute), Price: 11.3},
		},
	}
	klines := []model.Kline{
		{OpenTime: base.Add(0 * time.Minute), CloseTime: base.Add(1 * time.Minute), MarketCapOpen: 10.0, MarketCapHigh: 10.4, MarketCapLow: 9.9, MarketCapClose: 10.1, Volume: 100},
		{OpenTime: base.Add(1 * time.Minute), CloseTime: base.Add(2 * time.Minute), MarketCapOpen: 10.1, MarketCapHigh: 11.0, MarketCapLow: 10.1, MarketCapClose: 10.8, Volume: 150},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), MarketCapOpen: 10.8, MarketCapHigh: 11.2, MarketCapLow: 10.8, MarketCapClose: 11.0, Volume: 160},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), MarketCapOpen: 11.0, MarketCapHigh: 11.5, MarketCapLow: 11.0, MarketCapClose: 11.4, Volume: 170},
		{OpenTime: base.Add(4 * time.Minute), CloseTime: base.Add(5 * time.Minute), MarketCapOpen: 11.4, MarketCapHigh: 11.45, MarketCapLow: 11.1, MarketCapClose: 11.2, Volume: 180},
		{OpenTime: base.Add(5 * time.Minute), CloseTime: base.Add(6 * time.Minute), MarketCapOpen: 11.2, MarketCapHigh: 11.6, MarketCapLow: 11.2, MarketCapClose: 11.3, Volume: 190},
		{OpenTime: base.Add(6 * time.Minute), CloseTime: base.Add(7 * time.Minute), MarketCapOpen: 11.3, MarketCapHigh: 12.6, MarketCapLow: 11.3, MarketCapClose: 12.2, Volume: 220},
	}
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines: klines,
		Windows: []WindowLevelResult{
			{WindowIndex: 1, Levels: []model.PriceLevel{levelA}},
			{WindowIndex: 2, Levels: []model.PriceLevel{levelB}},
			{WindowIndex: 3, Levels: []model.PriceLevel{levelC}},
		},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{
		TakeProfitRate:       0.08,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.05,
		LockedProfitRate:     0.03,
		FeeRate:              0.015,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("expected only two non-overlapping trades, got %#v", result.Trades)
	}
	if !result.Trades[0].BuyPoint.Time.Equal(base.Add(1 * time.Minute)) {
		t.Fatalf("expected first trade at minute 1, got %#v", result.Trades[0])
	}
	if !result.Trades[1].BuyPoint.Time.Equal(base.Add(5 * time.Minute)) {
		t.Fatalf("expected second trade after first exit at minute 5, got %#v", result.Trades[1])
	}
}

func TestBreakoutBandFollowMethodReplaysRealtimeEntrySignals(t *testing.T) {
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
	result, err := newBreakoutBandFollowMethod().Run(StrategyBacktestContext{
		Klines:       klines,
		LevelOptions: LevelOptions{PivotWindow: 1, PriceTolerance: 0.02, BreakTolerance: 0.01, ConfirmBars: 1, VolumeWindow: 3, VolumeMultiplier: 1.2, MaxLevels: 4, WindowSize: 6, WindowStep: 1, MinTouches: 3},
	}, mustStrategyConfig(t, BreakoutBandFollowConfig{
		TakeProfitRate:       0.08,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.05,
		LockedProfitRate:     0.03,
		FeeRate:              0.015,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected one replayed trade, got %#v", result.Trades)
	}
	if !result.Trades[0].BuyPoint.Time.Equal(base.Add(6 * time.Minute)) {
		t.Fatalf("expected replay buy at realtime breakout bar, got %#v", result.Trades[0])
	}
	if len(result.Windows) == 0 {
		t.Fatalf("expected replay windows for chart explanation")
	}
}

func mustStrategyConfig(t *testing.T, config BreakoutBandFollowConfig) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return raw
}

func almostEqual(left float64, right float64) bool {
	diff := left - right
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.0001
}
