package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

var ErrStrategyMethodNotFound = errors.New("回测方法不存在")

type StrategyParamDefinition struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	Required     bool    `json:"required"`
	DefaultValue float64 `json:"defaultValue"`
	MinValue     float64 `json:"minValue,omitempty"`
	MaxValue     float64 `json:"maxValue,omitempty"`
	Step         float64 `json:"step,omitempty"`
}

type StrategyMethodMetadata struct {
	Code        string                    `json:"code"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Params      []StrategyParamDefinition `json:"params"`
}

type StrategyBacktestRequest struct {
	MethodCode   string
	MethodConfig json.RawMessage
	TokenAddress string
	Interval     string
	StartTime    time.Time
	EndTime      time.Time
	LevelOptions LevelOptions
}

type StrategyBacktestContext struct {
	MethodCode   string
	TokenAddress string
	Interval     string
	StartTime    time.Time
	EndTime      time.Time
	Klines       []model.Kline
	Windows      []WindowLevelResult
	LevelOptions LevelOptions
}

type StrategyBacktestResult struct {
	MethodCode string                   `json:"methodCode"`
	MethodName string                   `json:"methodName"`
	Summary    StrategyBacktestSummary  `json:"summary"`
	Trades     []StrategyBacktestTrade  `json:"trades"`
	Groups     []StrategyBacktestGroup  `json:"groups,omitempty"`
	Methods    []StrategyMethodMetadata `json:"methods,omitempty"`
	Klines     []model.Kline            `json:"klines,omitempty"`
	Windows    []WindowLevelResult      `json:"windows,omitempty"`
}

type StrategyBacktestGroup struct {
	Label          string                  `json:"label"`
	TakeProfitRate float64                 `json:"takeProfitRate"`
	FeeRate        float64                 `json:"feeRate"`
	Summary        StrategyBacktestSummary `json:"summary"`
	Trades         []StrategyBacktestTrade `json:"trades"`
}

type StrategyBacktestSummary struct {
	TradeCount           int     `json:"tradeCount"`
	WinCount             int     `json:"winCount"`
	LossCount            int     `json:"lossCount"`
	WinRate              float64 `json:"winRate"`
	TotalProfitUSD       float64 `json:"totalProfitUsd"`
	TotalProfitRate      float64 `json:"totalProfitRate"`
	AverageProfitRate    float64 `json:"averageProfitRate"`
	MaxDrawdownUSD       float64 `json:"maxDrawdownUsd"`
	MaxDrawdownRate      float64 `json:"maxDrawdownRate"`
	PositionSizeUSD      float64 `json:"positionSizeUsd"`
	CommittedCapitalUSD  float64 `json:"committedCapitalUsd"`
	BestTradeProfitRate  float64 `json:"bestTradeProfitRate"`
	WorstTradeProfitRate float64 `json:"worstTradeProfitRate"`
}

type StrategyBacktestTrade struct {
	WindowIndex           int                    `json:"windowIndex"`
	LevelIndex            int                    `json:"levelIndex"`
	LevelType             model.LevelType        `json:"levelType"`
	LevelMarketCap        float64                `json:"levelMarketCap"`
	LevelLowerMarketCap   float64                `json:"levelLowerMarketCap"`
	LevelUpperMarketCap   float64                `json:"levelUpperMarketCap"`
	BuyPoint              model.LevelAnchorPoint `json:"buyPoint"`
	SellPoint             model.LevelAnchorPoint `json:"sellPoint"`
	ProfitRate            float64                `json:"profitRate"`
	ProfitUSD             float64                `json:"profitUsd"`
	GrossProfitRate       float64                `json:"grossProfitRate"`
	GrossProfitUSD        float64                `json:"grossProfitUsd"`
	FeeRate               float64                `json:"feeRate"`
	FeeUSD                float64                `json:"feeUsd"`
	PositionSizeUSD       float64                `json:"positionSizeUsd"`
	Outcome               model.BreakoutOutcome  `json:"outcome"`
	ExitReason            string                 `json:"exitReason"`
	TakeProfitRate        float64                `json:"takeProfitRate"`
	StopLossMarketCap     float64                `json:"stopLossMarketCap,omitempty"`
	TakeProfitMarketCap   float64                `json:"takeProfitMarketCap,omitempty"`
	TrailingArmed         bool                   `json:"trailingArmed"`
	TrailingStopMarketCap float64                `json:"trailingStopMarketCap,omitempty"`
	HoldingBars           int                    `json:"holdingBars"`
	Calculation           model.LevelCalculation `json:"calculation"`
	Breakout              *model.BreakoutSetup   `json:"breakout,omitempty"`
}

type StrategyMethod interface {
	Metadata() StrategyMethodMetadata
	Run(ctx StrategyBacktestContext, raw json.RawMessage) (StrategyBacktestResult, error)
}

func (s *Service) StrategyMethods() []StrategyMethodMetadata {
	items := make([]StrategyMethodMetadata, 0, len(s.strategyMethods))
	for _, method := range s.strategyMethods {
		items = append(items, method.Metadata())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

func (s *Service) RunStrategyBacktest(ctx context.Context, source string, req StrategyBacktestRequest) (StrategyBacktestResult, error) {
	method, ok := s.strategyMethods[req.MethodCode]
	if !ok {
		return StrategyBacktestResult{}, ErrStrategyMethodNotFound
	}
	levelsResult, err := s.GetKlineLevels(ctx, source, datasource.KlineQuery{
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
	}, req.LevelOptions)
	if err != nil {
		return StrategyBacktestResult{}, err
	}
	result, err := method.Run(StrategyBacktestContext{
		MethodCode:   req.MethodCode,
		TokenAddress: req.TokenAddress,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Klines:       levelsResult.Klines,
		Windows:      levelsResult.Windows,
		LevelOptions: req.LevelOptions,
	}, req.MethodConfig)
	if err != nil {
		return StrategyBacktestResult{}, err
	}
	result.MethodCode = method.Metadata().Code
	result.MethodName = method.Metadata().Name
	if len(result.Klines) == 0 {
		result.Klines = levelsResult.Klines
	}
	if len(result.Windows) == 0 {
		result.Windows = levelsResult.Windows
	}
	return result, nil
}

type BreakoutBandFollowConfig struct {
	TakeProfitRate       float64 `json:"takeProfitRate"`
	TakeProfitRateStart  float64 `json:"takeProfitRateStart"`
	TakeProfitRateEnd    float64 `json:"takeProfitRateEnd"`
	TakeProfitRateStep   float64 `json:"takeProfitRateStep"`
	PositionSizeUSD      float64 `json:"positionSizeUsd"`
	HardStopLossRate     float64 `json:"hardStopLossRate"`
	ActivationProfitRate float64 `json:"activationProfitRate"`
	LockedProfitRate     float64 `json:"lockedProfitRate"`
	FeeRate              float64 `json:"feeRate"`
}

type breakoutBandFollowMethod struct{}

type BandFollowExitDecision struct {
	Triggered       bool
	Outcome         model.BreakoutOutcome
	ExitPoint       *model.LevelAnchorPoint
	HoldingBars     int
	ProfitRate      float64
	Reason          string
	InitialStopLoss float64
	TrailingStop    float64
	TrailingArmed   bool
}

func newBreakoutBandFollowMethod() StrategyMethod {
	return breakoutBandFollowMethod{}
}

func DefaultBreakoutBandFollowConfig() BreakoutBandFollowConfig {
	return BreakoutBandFollowConfig{
		TakeProfitRate:       0.08,
		PositionSizeUSD:      10,
		HardStopLossRate:     0.05,
		ActivationProfitRate: 0.05,
		LockedProfitRate:     0.03,
		FeeRate:              0.015,
	}
}

func (breakoutBandFollowMethod) Metadata() StrategyMethodMetadata {
	return StrategyMethodMetadata{
		Code:        "breakout_band_follow",
		Name:        "突破压力带买入",
		Description: "在突破压力带的 K 线收盘买入；下一根 K 线跌回压力带上沿止损；同时支持可配置硬止损；盈利到 5% 后把止损抬到 +3%，之后每多盈利 1% 锁盈同步上移 1%；止盈比例可配置。",
		Params: []StrategyParamDefinition{
			{Key: "takeProfitRate", Label: "止盈比例", Description: "达到该收益率后止盈卖出，例如 0.08 表示 8%。", Type: "number", Required: true, DefaultValue: 0.08, MinValue: 0.01, MaxValue: 0.5, Step: 0.01},
			{Key: "takeProfitRateStart", Label: "止盈起点", Description: "启用区间回测时的起始止盈比例。", Type: "number", Required: false, DefaultValue: 0.08, MinValue: 0.01, MaxValue: 0.5, Step: 0.01},
			{Key: "takeProfitRateEnd", Label: "止盈终点", Description: "启用区间回测时的结束止盈比例。", Type: "number", Required: false, DefaultValue: 0.15, MinValue: 0.01, MaxValue: 0.5, Step: 0.01},
			{Key: "takeProfitRateStep", Label: "止盈步长", Description: "启用区间回测时的止盈递增步长。", Type: "number", Required: false, DefaultValue: 0.01, MinValue: 0.001, MaxValue: 0.1, Step: 0.001},
			{Key: "positionSizeUsd", Label: "单笔投入(U)", Description: "每个买点固定投入金额。", Type: "number", Required: true, DefaultValue: 10, MinValue: 1, MaxValue: 100000, Step: 1},
			{Key: "hardStopLossRate", Label: "硬止损比例", Description: "买入后任意时刻亏损达到该比例立即止损，例如 0.05 表示 -5%。", Type: "number", Required: true, DefaultValue: 0.05, MinValue: 0.001, MaxValue: 0.5, Step: 0.001},
			{Key: "activationProfitRate", Label: "动态止损触发收益率", Description: "盈利达到该比例后开始保护利润；之后每多盈利 1%，锁定收益率同步上移 1%。", Type: "number", Required: true, DefaultValue: 0.05, MinValue: 0.01, MaxValue: 0.5, Step: 0.01},
			{Key: "lockedProfitRate", Label: "动态止损锁定收益率", Description: "动态止损触发后，回撤到该收益率卖出；例如 5%/3%、6%/4%。", Type: "number", Required: true, DefaultValue: 0.03, MinValue: 0.001, MaxValue: 0.5, Step: 0.01},
			{Key: "feeRate", Label: "总手续费比例", Description: "单笔买入加卖出的总手续费比例，默认 1.5%。", Type: "number", Required: false, DefaultValue: 0.015, MinValue: 0, MaxValue: 0.3, Step: 0.001},
		},
	}
}

func (breakoutBandFollowMethod) Run(ctx StrategyBacktestContext, raw json.RawMessage) (StrategyBacktestResult, error) {
	config := DefaultBreakoutBandFollowConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return StrategyBacktestResult{}, fmt.Errorf("回测方法参数格式错误: %w", err)
		}
	}
	if config.TakeProfitRate <= 0 {
		if config.TakeProfitRateStart <= 0 || config.TakeProfitRateEnd <= 0 || config.TakeProfitRateStep <= 0 {
			return StrategyBacktestResult{}, errors.New("止盈比例或止盈区间参数必须大于 0")
		}
	}
	if config.PositionSizeUSD <= 0 {
		return StrategyBacktestResult{}, errors.New("单笔投入金额必须大于 0")
	}
	if config.HardStopLossRate <= 0 || config.HardStopLossRate >= 1 {
		return StrategyBacktestResult{}, errors.New("硬止损比例必须大于 0 且小于 1")
	}
	if config.ActivationProfitRate <= 0 {
		return StrategyBacktestResult{}, errors.New("动态止损触发收益率必须大于 0")
	}
	if config.LockedProfitRate <= 0 || config.LockedProfitRate >= config.ActivationProfitRate {
		return StrategyBacktestResult{}, errors.New("动态止损锁定收益率必须大于 0 且小于触发收益率")
	}
	if config.FeeRate < 0 || config.FeeRate >= 1 {
		return StrategyBacktestResult{}, errors.New("手续费比例必须大于等于 0 且小于 1")
	}

	takeProfitRates, err := resolveTakeProfitRates(config)
	if err != nil {
		return StrategyBacktestResult{}, err
	}
	replayEntries, replayWindows := CollectBandFollowReplayEntries(ctx.Klines, ctx.LevelOptions)
	if len(replayWindows) == 0 && len(ctx.Windows) > 0 {
		replayWindows = cloneWindowLevelResults(ctx.Windows)
	}
	groups := make([]StrategyBacktestGroup, 0, len(takeProfitRates))
	for _, takeProfitRate := range takeProfitRates {
		groupConfig := config
		groupConfig.TakeProfitRate = takeProfitRate
		trades := runBandFollowTrades(replayEntries, ctx.Windows, ctx.Klines, groupConfig)
		groups = append(groups, StrategyBacktestGroup{
			Label:          fmt.Sprintf("止盈 %s", formatPercentValue(takeProfitRate)),
			TakeProfitRate: takeProfitRate,
			FeeRate:        config.FeeRate,
			Summary:        summarizeStrategyTrades(trades, config.PositionSizeUSD),
			Trades:         trades,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].TakeProfitRate == groups[j].TakeProfitRate {
			return groups[i].Summary.TotalProfitUSD > groups[j].Summary.TotalProfitUSD
		}
		return groups[i].TakeProfitRate < groups[j].TakeProfitRate
	})
	result := StrategyBacktestResult{Groups: groups}
	if len(groups) > 0 {
		best := groups[0]
		for _, group := range groups[1:] {
			if group.Summary.TotalProfitUSD > best.Summary.TotalProfitUSD {
				best = group
			}
		}
		result.Summary = best.Summary
		result.Trades = best.Trades
	}
	result.Klines = append([]model.Kline(nil), ctx.Klines...)
	result.Windows = replayWindows
	return result, nil
}

func resolveTakeProfitRates(config BreakoutBandFollowConfig) ([]float64, error) {
	if config.TakeProfitRateStart > 0 || config.TakeProfitRateEnd > 0 || config.TakeProfitRateStep > 0 {
		if config.TakeProfitRateStart <= 0 || config.TakeProfitRateEnd <= 0 || config.TakeProfitRateStep <= 0 {
			return nil, errors.New("止盈区间起点、终点、步长必须同时大于 0")
		}
		if config.TakeProfitRateEnd < config.TakeProfitRateStart {
			return nil, errors.New("止盈区间终点不能小于起点")
		}
		values := make([]float64, 0)
		for value := config.TakeProfitRateStart; value <= config.TakeProfitRateEnd+1e-9; value += config.TakeProfitRateStep {
			values = append(values, roundTo(value, 6))
		}
		return values, nil
	}
	return []float64{config.TakeProfitRate}, nil
}

func runBandFollowTrades(entries []BandFollowReplayEntry, windows []WindowLevelResult, klines []model.Kline, config BreakoutBandFollowConfig) []StrategyBacktestTrade {
	if len(klines) == 0 {
		return nil
	}
	if len(entries) == 0 {
		return runBandFollowTradesLegacy(windows, klines, config)
	}
	timeIndex := make(map[string]int, len(klines))
	for index, item := range klines {
		timeIndex[item.OpenTime.Format(time.RFC3339Nano)] = index
	}
	trades := make([]StrategyBacktestTrade, 0, len(entries))
	activeExitIndex := -1
	for _, entry := range entries {
		if entry.EntryIndex <= activeExitIndex {
			continue
		}
		outcome, exitPoint, holdingBars, grossProfitRate, exitReason, initialStopLoss, trailingStop, trailingArmed := simulateBandFollowExit(klines, entry.EntryIndex, entry.Level, config)
		if exitPoint == nil {
			continue
		}
		exitIndex, ok := timeIndex[exitPoint.Time.Format(time.RFC3339Nano)]
		if !ok {
			continue
		}
		feeUSD := config.PositionSizeUSD * config.FeeRate
		netProfitRate := grossProfitRate - config.FeeRate
		trades = append(trades, StrategyBacktestTrade{
			WindowIndex:           entry.GlobalWindow,
			LevelIndex:            entry.Signal.LevelIndex,
			LevelType:             entry.Level.Type,
			LevelMarketCap:        entry.Level.Price,
			LevelLowerMarketCap:   entry.Level.Lower,
			LevelUpperMarketCap:   entry.Level.Upper,
			BuyPoint:              *entry.Level.Breakout.BuyPoint,
			SellPoint:             *exitPoint,
			ProfitRate:            netProfitRate,
			ProfitUSD:             config.PositionSizeUSD*grossProfitRate - feeUSD,
			GrossProfitRate:       grossProfitRate,
			GrossProfitUSD:        config.PositionSizeUSD * grossProfitRate,
			FeeRate:               config.FeeRate,
			FeeUSD:                feeUSD,
			PositionSizeUSD:       config.PositionSizeUSD,
			Outcome:               outcome,
			ExitReason:            exitReason,
			TakeProfitRate:        config.TakeProfitRate,
			StopLossMarketCap:     initialStopLoss,
			TakeProfitMarketCap:   entry.Level.Breakout.BuyPoint.Price * (1 + config.TakeProfitRate),
			TrailingArmed:         trailingArmed,
			TrailingStopMarketCap: trailingStop,
			HoldingBars:           holdingBars,
			Calculation:           entry.Level.Calculation,
			Breakout:              entry.Level.Breakout,
		})
		activeExitIndex = exitIndex
	}
	return trades
}

func runBandFollowTradesLegacy(windows []WindowLevelResult, klines []model.Kline, config BreakoutBandFollowConfig) []StrategyBacktestTrade {
	timeIndex := make(map[string]int, len(klines))
	for index, item := range klines {
		timeIndex[item.OpenTime.Format(time.RFC3339Nano)] = index
	}
	type candidateTrade struct {
		trade      StrategyBacktestTrade
		entryIndex int
		exitIndex  int
	}
	candidates := make([]candidateTrade, 0)
	for windowIndex, window := range windows {
		for levelIndex, level := range window.Levels {
			if level.Breakout == nil || level.Breakout.BuyPoint == nil || level.Breakout.BreakoutPoint == nil {
				continue
			}
			entryIndex, ok := timeIndex[level.Breakout.BuyPoint.Time.Format(time.RFC3339Nano)]
			if !ok {
				continue
			}
			outcome, exitPoint, holdingBars, grossProfitRate, exitReason, initialStopLoss, trailingStop, trailingArmed := simulateBandFollowExit(klines, entryIndex, level, config)
			if exitPoint == nil {
				continue
			}
			exitIndex, ok := timeIndex[exitPoint.Time.Format(time.RFC3339Nano)]
			if !ok {
				continue
			}
			feeUSD := config.PositionSizeUSD * config.FeeRate
			netProfitRate := grossProfitRate - config.FeeRate
			candidates = append(candidates, candidateTrade{
				entryIndex: entryIndex,
				exitIndex:  exitIndex,
				trade: StrategyBacktestTrade{
					WindowIndex:           windowIndex + 1,
					LevelIndex:            levelIndex + 1,
					LevelType:             level.Type,
					LevelMarketCap:        level.Price,
					LevelLowerMarketCap:   level.Lower,
					LevelUpperMarketCap:   level.Upper,
					BuyPoint:              *level.Breakout.BuyPoint,
					SellPoint:             *exitPoint,
					ProfitRate:            netProfitRate,
					ProfitUSD:             config.PositionSizeUSD*grossProfitRate - feeUSD,
					GrossProfitRate:       grossProfitRate,
					GrossProfitUSD:        config.PositionSizeUSD * grossProfitRate,
					FeeRate:               config.FeeRate,
					FeeUSD:                feeUSD,
					PositionSizeUSD:       config.PositionSizeUSD,
					Outcome:               outcome,
					ExitReason:            exitReason,
					TakeProfitRate:        config.TakeProfitRate,
					StopLossMarketCap:     initialStopLoss,
					TakeProfitMarketCap:   level.Breakout.BuyPoint.Price * (1 + config.TakeProfitRate),
					TrailingArmed:         trailingArmed,
					TrailingStopMarketCap: trailingStop,
					HoldingBars:           holdingBars,
					Calculation:           level.Calculation,
					Breakout:              level.Breakout,
				},
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].entryIndex == candidates[j].entryIndex {
			if candidates[i].exitIndex == candidates[j].exitIndex {
				return candidates[i].trade.LevelIndex < candidates[j].trade.LevelIndex
			}
			return candidates[i].exitIndex < candidates[j].exitIndex
		}
		return candidates[i].entryIndex < candidates[j].entryIndex
	})
	trades := make([]StrategyBacktestTrade, 0, len(candidates))
	activeExitIndex := -1
	for _, candidate := range candidates {
		if candidate.entryIndex <= activeExitIndex {
			continue
		}
		trades = append(trades, candidate.trade)
		activeExitIndex = candidate.exitIndex
	}
	return trades
}

func simulateBandFollowExit(klines []model.Kline, entryIndex int, level model.PriceLevel, config BreakoutBandFollowConfig) (model.BreakoutOutcome, *model.LevelAnchorPoint, int, float64, string, float64, float64, bool) {
	decision := EvaluateRealtimeBandFollowExit(klines, entryIndex, level, config)
	if decision.Triggered {
		return decision.Outcome, decision.ExitPoint, decision.HoldingBars, decision.ProfitRate, decision.Reason, decision.InitialStopLoss, decision.TrailingStop, decision.TrailingArmed
	}
	if entryIndex < 0 || entryIndex >= len(klines) {
		return model.BreakoutOutcomePending, nil, 0, 0, "", 0, 0, false
	}
	entryPrice := 0.0
	if level.Breakout != nil && level.Breakout.BuyPoint != nil {
		entryPrice = level.Breakout.BuyPoint.Price
	}
	if entryPrice <= 0 {
		entryPrice = marketClose(klines[entryIndex])
	}
	if entryPrice <= 0 {
		entryPrice = marketOpen(klines[entryIndex])
	}
	if entryPrice <= 0 {
		return model.BreakoutOutcomePending, nil, 0, 0, "", 0, 0, false
	}
	initialStopLoss := level.Upper
	if initialStopLoss <= 0 {
		initialStopLoss = level.Price
	}
	takeProfitPrice := entryPrice * (1 + config.TakeProfitRate)
	hardStopLossPrice := entryPrice * (1 - config.HardStopLossRate)
	trailingStopPrice := entryPrice * (1 + config.LockedProfitRate)
	trailingArmed := false

	for i := entryIndex + 1; i < len(klines); i++ {
		item := klines[i]
		holdingBars := i - entryIndex
		if i == entryIndex+1 && marketLow(item) <= initialStopLoss {
			exit := anchorFromKline(item, initialStopLoss)
			return model.BreakoutOutcomeStopLoss, &exit, holdingBars, profitRate(entryPrice, initialStopLoss), "买入后下一根 K 线跌破压力带上沿，按上沿止损卖出", initialStopLoss, trailingStopPrice, trailingArmed
		}
		if marketLow(item) <= hardStopLossPrice {
			exit := anchorFromKline(item, hardStopLossPrice)
			return model.BreakoutOutcomeStopLoss, &exit, holdingBars, profitRate(entryPrice, hardStopLossPrice), fmt.Sprintf("买入后触发硬止损 %s，按硬止损价卖出", formatPercentValue(config.HardStopLossRate)), initialStopLoss, trailingStopPrice, trailingArmed
		}
		if trailingArmed && marketLow(item) <= trailingStopPrice {
			exit := anchorFromKline(item, trailingStopPrice)
			return model.BreakoutOutcomeStopLoss, &exit, holdingBars, profitRate(entryPrice, trailingStopPrice), "盈利达到触发阈值后回撤到锁定收益率，执行动态止损", initialStopLoss, trailingStopPrice, trailingArmed
		}
		if marketHigh(item) >= takeProfitPrice {
			exit := anchorFromKline(item, takeProfitPrice)
			return model.BreakoutOutcomeTakeProfit, &exit, holdingBars, profitRate(entryPrice, takeProfitPrice), "达到止盈比例，按止盈价卖出", initialStopLoss, trailingStopPrice, trailingArmed
		}
		trailingStopPrice, trailingArmed = steppedTrailingStopPrice(entryPrice, marketHigh(item), config, trailingStopPrice, trailingArmed)
	}
	last := klines[len(klines)-1]
	exitPrice := marketClose(last)
	if exitPrice <= 0 {
		exitPrice = marketOpen(last)
	}
	exit := anchorFromKline(last, exitPrice)
	return model.BreakoutOutcomeTimeout, &exit, len(klines) - entryIndex - 1, profitRate(entryPrice, exitPrice), "直到样本结束仍未触发止盈或止损，按最后一根 K 线收盘卖出", initialStopLoss, trailingStopPrice, trailingArmed
}

func EvaluateRealtimeBandFollowExit(klines []model.Kline, entryIndex int, level model.PriceLevel, config BreakoutBandFollowConfig) BandFollowExitDecision {
	if entryIndex < 0 || entryIndex >= len(klines) {
		return BandFollowExitDecision{}
	}
	entryPrice := 0.0
	if level.Breakout != nil && level.Breakout.BuyPoint != nil {
		entryPrice = level.Breakout.BuyPoint.Price
	}
	if entryPrice <= 0 {
		entryPrice = marketClose(klines[entryIndex])
	}
	if entryPrice <= 0 {
		entryPrice = marketOpen(klines[entryIndex])
	}
	if entryPrice <= 0 {
		return BandFollowExitDecision{}
	}
	initialStopLoss := level.Upper
	if initialStopLoss <= 0 {
		initialStopLoss = level.Price
	}
	takeProfitPrice := entryPrice * (1 + config.TakeProfitRate)
	hardStopLossPrice := entryPrice * (1 - config.HardStopLossRate)
	trailingStopPrice := entryPrice * (1 + config.LockedProfitRate)
	trailingArmed := false

	for i := entryIndex + 1; i < len(klines); i++ {
		item := klines[i]
		holdingBars := i - entryIndex
		if i == entryIndex+1 && marketLow(item) <= initialStopLoss {
			exit := anchorFromKline(item, initialStopLoss)
			return BandFollowExitDecision{Triggered: true, Outcome: model.BreakoutOutcomeStopLoss, ExitPoint: &exit, HoldingBars: holdingBars, ProfitRate: profitRate(entryPrice, initialStopLoss), Reason: "买入后下一根 K 线跌破压力带上沿，按上沿止损卖出", InitialStopLoss: initialStopLoss, TrailingStop: trailingStopPrice, TrailingArmed: trailingArmed}
		}
		if marketLow(item) <= hardStopLossPrice {
			exit := anchorFromKline(item, hardStopLossPrice)
			return BandFollowExitDecision{Triggered: true, Outcome: model.BreakoutOutcomeStopLoss, ExitPoint: &exit, HoldingBars: holdingBars, ProfitRate: profitRate(entryPrice, hardStopLossPrice), Reason: fmt.Sprintf("买入后触发硬止损 %s，按硬止损价卖出", formatPercentValue(config.HardStopLossRate)), InitialStopLoss: initialStopLoss, TrailingStop: trailingStopPrice, TrailingArmed: trailingArmed}
		}
		if trailingArmed && marketLow(item) <= trailingStopPrice {
			exit := anchorFromKline(item, trailingStopPrice)
			return BandFollowExitDecision{Triggered: true, Outcome: model.BreakoutOutcomeStopLoss, ExitPoint: &exit, HoldingBars: holdingBars, ProfitRate: profitRate(entryPrice, trailingStopPrice), Reason: "盈利达到触发阈值后回撤到锁定收益率，执行动态止损", InitialStopLoss: initialStopLoss, TrailingStop: trailingStopPrice, TrailingArmed: trailingArmed}
		}
		if marketHigh(item) >= takeProfitPrice {
			exit := anchorFromKline(item, takeProfitPrice)
			return BandFollowExitDecision{Triggered: true, Outcome: model.BreakoutOutcomeTakeProfit, ExitPoint: &exit, HoldingBars: holdingBars, ProfitRate: profitRate(entryPrice, takeProfitPrice), Reason: "达到止盈比例，按止盈价卖出", InitialStopLoss: initialStopLoss, TrailingStop: trailingStopPrice, TrailingArmed: trailingArmed}
		}
		trailingStopPrice, trailingArmed = steppedTrailingStopPrice(entryPrice, marketHigh(item), config, trailingStopPrice, trailingArmed)
	}
	return BandFollowExitDecision{InitialStopLoss: initialStopLoss, TrailingStop: trailingStopPrice, TrailingArmed: trailingArmed}
}

func steppedTrailingStopPrice(entryPrice float64, highPrice float64, config BreakoutBandFollowConfig, currentStop float64, armed bool) (float64, bool) {
	if entryPrice <= 0 || highPrice <= 0 || highPrice < entryPrice*(1+config.ActivationProfitRate) {
		return currentStop, armed
	}
	const lockStepRate = 0.01
	highestProfitRate := profitRate(entryPrice, highPrice)
	if config.TakeProfitRate > 0 && highestProfitRate > config.TakeProfitRate {
		highestProfitRate = config.TakeProfitRate
	}
	steps := math.Floor((highestProfitRate - config.ActivationProfitRate + 1e-9) / lockStepRate)
	if steps < 0 {
		steps = 0
	}
	lockRate := config.LockedProfitRate + steps*lockStepRate
	if config.TakeProfitRate > config.ActivationProfitRate {
		maxLockRate := config.LockedProfitRate + config.TakeProfitRate - config.ActivationProfitRate
		if lockRate > maxLockRate {
			lockRate = maxLockRate
		}
	}
	nextStop := entryPrice * (1 + lockRate)
	if !armed || nextStop > currentStop {
		return nextStop, true
	}
	return currentStop, true
}

func summarizeStrategyTrades(trades []StrategyBacktestTrade, positionSizeUSD float64) StrategyBacktestSummary {
	summary := StrategyBacktestSummary{
		TradeCount:          len(trades),
		PositionSizeUSD:     positionSizeUSD,
		CommittedCapitalUSD: positionSizeUSD * float64(len(trades)),
	}
	if len(trades) == 0 {
		return summary
	}
	bestProfitRate := -math.MaxFloat64
	worstProfitRate := math.MaxFloat64
	equity := 0.0
	peak := 0.0
	maxDrawdown := 0.0
	for _, trade := range trades {
		summary.TotalProfitUSD += trade.ProfitUSD
		summary.TotalProfitRate += trade.ProfitRate
		if trade.ProfitRate > 0 {
			summary.WinCount++
		} else {
			summary.LossCount++
		}
		if trade.ProfitRate > bestProfitRate {
			bestProfitRate = trade.ProfitRate
		}
		if trade.ProfitRate < worstProfitRate {
			worstProfitRate = trade.ProfitRate
		}
		equity += trade.ProfitUSD
		if equity > peak {
			peak = equity
		}
		drawdown := peak - equity
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}
	summary.AverageProfitRate = summary.TotalProfitRate / float64(len(trades))
	summary.WinRate = float64(summary.WinCount) / float64(len(trades))
	summary.MaxDrawdownUSD = maxDrawdown
	if summary.CommittedCapitalUSD > 0 {
		summary.MaxDrawdownRate = maxDrawdown / summary.CommittedCapitalUSD
	}
	summary.BestTradeProfitRate = bestProfitRate
	summary.WorstTradeProfitRate = worstProfitRate
	return summary
}

func roundTo(value float64, precision int) float64 {
	factor := math.Pow(10, float64(precision))
	return math.Round(value*factor) / factor
}

func formatPercentValue(value float64) string {
	return fmt.Sprintf("%.2f%%", value*100)
}
