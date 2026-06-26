package backtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

var (
	ErrNoKlines         = errors.New("指定时间范围内没有 K 线数据")
	ErrInvalidTimeRange = errors.New("开始时间必须早于结束时间")
	ErrInvalidTradeFlow = errors.New("买卖点必须按买入后卖出的顺序成对出现")
)

type Repository interface {
	SaveAnalysis(ctx context.Context, input SaveAnalysisInput) error
	GetAnalysis(ctx context.Context, id string) (SavedAnalysis, error)
	ListAnalyses(ctx context.Context, limit int) ([]model.BacktestSession, error)
}

type Service struct {
	klines              datasource.KlineDataSource
	dbBars              datasource.KlineDataSource
	birdeye             datasource.KlineDataSource
	tradePoints         datasource.TradePointDataSource
	bitqueryTradePoints datasource.TradePointDataSource
	dbTradePoints       datasource.TradePointDataSource
	tokens              datasource.TokenDataSource
	repo                Repository
	strategyMethods     map[string]StrategyMethod
}

type AnalyzeRequest struct {
	SessionID    string
	DataSource   string
	TokenAddress string
	TokenSymbol  string
	Interval     string
	StartTime    time.Time
	EndTime      time.Time
	TradePoints  []model.TradePoint
}

type MarkedKlinesResult struct {
	Klines      []model.Kline      `json:"klines"`
	TradePoints []model.TradePoint `json:"tradePoints"`
	Levels      []model.PriceLevel `json:"levels"`
}

type KlineLevelsResult struct {
	Klines     []model.Kline       `json:"klines"`
	Windows    []WindowLevelResult `json:"windows"`
	WindowSize int                 `json:"windowSize"`
	WindowStep int                 `json:"windowStep"`
}

type WindowLevelResult struct {
	WindowIndex int                `json:"windowIndex"`
	StartTime   time.Time          `json:"startTime"`
	EndTime     time.Time          `json:"endTime"`
	KlineCount  int                `json:"klineCount"`
	Levels      []model.PriceLevel `json:"levels"`
}

type AnalyzeResult struct {
	SessionID    string                    `json:"sessionId"`
	TokenAddress string                    `json:"tokenAddress"`
	TokenSymbol  string                    `json:"tokenSymbol"`
	Interval     string                    `json:"interval"`
	StartTime    time.Time                 `json:"startTime"`
	EndTime      time.Time                 `json:"endTime"`
	Klines       []model.Kline             `json:"klines"`
	TradePoints  []model.MatchedTradePoint `json:"tradePoints"`
	Trades       []model.TradeResult       `json:"trades"`
	Metrics      model.Metrics             `json:"metrics"`
}

type SaveAnalysisInput struct {
	Session model.BacktestSession
	Points  []model.MatchedTradePoint
	Trades  []model.TradeResult
	Metrics model.Metrics
}

type SavedAnalysis struct {
	Session model.BacktestSession     `json:"session"`
	Points  []model.MatchedTradePoint `json:"tradePoints"`
	Trades  []model.TradeResult       `json:"trades"`
	Metrics model.Metrics             `json:"metrics"`
}

func NewService(klines datasource.KlineDataSource, dbBars datasource.KlineDataSource, birdeye datasource.KlineDataSource, tradePoints datasource.TradePointDataSource, bitqueryTradePoints datasource.TradePointDataSource, dbTradePoints datasource.TradePointDataSource, tokens datasource.TokenDataSource, repo Repository) *Service {
	methods := []StrategyMethod{
		newBreakoutBandFollowMethod(),
	}
	methodMap := make(map[string]StrategyMethod, len(methods))
	for _, method := range methods {
		methodMap[method.Metadata().Code] = method
	}
	return &Service{
		klines:              klines,
		dbBars:              dbBars,
		birdeye:             birdeye,
		tradePoints:         tradePoints,
		bitqueryTradePoints: bitqueryTradePoints,
		dbTradePoints:       dbTradePoints,
		tokens:              tokens,
		repo:                repo,
		strategyMethods:     methodMap,
	}
}

func (s *Service) SearchTokens(ctx context.Context, keyword string, limit int) ([]model.Token, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.tokens.SearchTokens(ctx, keyword, limit)
}

func (s *Service) GetKlines(ctx context.Context, source string, req datasource.KlineQuery) ([]model.Kline, error) {
	if !req.StartTime.IsZero() && !req.EndTime.IsZero() && !req.StartTime.Before(req.EndTime) {
		return nil, ErrInvalidTimeRange
	}
	return s.source(source).GetKlines(ctx, req)
}

func (s *Service) source(name string) datasource.KlineDataSource {
	if name == "birdeye" {
		return s.birdeye
	}
	if name == "db" {
		return s.dbBars
	}
	return s.klines
}

func (s *Service) GetMarkedKlines(ctx context.Context, source string, tradeSource string, klineReq datasource.KlineQuery, tradeReq datasource.TradePointQuery) (MarkedKlinesResult, error) {
	klines, err := s.GetKlines(ctx, source, klineReq)
	if err != nil {
		return MarkedKlinesResult{}, err
	}
	points, err := s.tradePointSource(tradeSource).GetTradePoints(ctx, tradeReq)
	if err != nil {
		return MarkedKlinesResult{}, err
	}
	levels := CalculateSupportResistance(klines, DefaultLevelOptions())
	return MarkedKlinesResult{Klines: klines, TradePoints: points, Levels: levels}, nil
}

func (s *Service) GetKlineLevels(ctx context.Context, source string, req datasource.KlineQuery, options LevelOptions) (KlineLevelsResult, error) {
	klines, err := s.GetKlines(ctx, source, req)
	if err != nil {
		return KlineLevelsResult{}, err
	}
	if len(klines) == 0 {
		return KlineLevelsResult{}, ErrNoKlines
	}
	windowSize := options.WindowSize
	if windowSize <= 0 || windowSize > len(klines) {
		windowSize = len(klines)
	}
	windowStep := options.WindowStep
	if windowStep <= 0 {
		windowStep = 1
	}
	return KlineLevelsResult{
		Klines:     klines,
		Windows:    CalculateSupportResistanceByWindows(klines, options, windowSize, windowStep),
		WindowSize: windowSize,
		WindowStep: windowStep,
	}, nil
}

func (s *Service) tradePointSource(name string) datasource.TradePointDataSource {
	if name == "bitquery" {
		return s.bitqueryTradePoints
	}
	if name == "db" {
		return s.dbTradePoints
	}
	return s.tradePoints
}

func (s *Service) GetSupportResistance(ctx context.Context, source string, req datasource.KlineQuery, options LevelOptions) ([]model.PriceLevel, error) {
	klines, err := s.GetKlines(ctx, source, req)
	if err != nil {
		return nil, err
	}
	if len(klines) == 0 {
		return nil, ErrNoKlines
	}
	return CalculateSupportResistance(klines, options), nil
}

func (s *Service) AnalyzeAndSave(ctx context.Context, req AnalyzeRequest) (AnalyzeResult, error) {
	result, err := s.Analyze(ctx, req)
	if err != nil {
		return AnalyzeResult{}, err
	}
	session := model.BacktestSession{
		ID:           result.SessionID,
		TokenAddress: result.TokenAddress,
		TokenSymbol:  result.TokenSymbol,
		Interval:     result.Interval,
		StartTime:    result.StartTime,
		EndTime:      result.EndTime,
	}
	if err := s.repo.SaveAnalysis(ctx, SaveAnalysisInput{Session: session, Points: result.TradePoints, Trades: result.Trades, Metrics: result.Metrics}); err != nil {
		return AnalyzeResult{}, err
	}
	return result, nil
}

func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResult, error) {
	if !req.StartTime.Before(req.EndTime) {
		return AnalyzeResult{}, ErrInvalidTimeRange
	}
	klines, err := s.source(req.DataSource).GetKlines(ctx, datasource.KlineQuery{TokenAddress: req.TokenAddress, Interval: req.Interval, StartTime: req.StartTime, EndTime: req.EndTime})
	if err != nil {
		return AnalyzeResult{}, err
	}
	if len(klines) == 0 {
		return AnalyzeResult{}, ErrNoKlines
	}
	sort.Slice(klines, func(i, j int) bool { return klines[i].OpenTime.Before(klines[j].OpenTime) })
	points := append([]model.TradePoint(nil), req.TradePoints...)
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
	matched, err := matchPoints(klines, points)
	if err != nil {
		return AnalyzeResult{}, err
	}
	trades, err := pairTrades(matched)
	if err != nil {
		return AnalyzeResult{}, err
	}
	metrics := calculateMetrics(trades)
	return AnalyzeResult{
		SessionID:    req.SessionID,
		TokenAddress: req.TokenAddress,
		TokenSymbol:  req.TokenSymbol,
		Interval:     req.Interval,
		StartTime:    apptime.InBeijing(req.StartTime),
		EndTime:      apptime.InBeijing(req.EndTime),
		Klines:       klines,
		TradePoints:  matched,
		Trades:       trades,
		Metrics:      metrics,
	}, nil
}

func (s *Service) GetAnalysis(ctx context.Context, id string) (SavedAnalysis, error) {
	return s.repo.GetAnalysis(ctx, id)
}

func (s *Service) ListAnalyses(ctx context.Context, limit int) ([]model.BacktestSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.ListAnalyses(ctx, limit)
}

func matchPoints(klines []model.Kline, points []model.TradePoint) ([]model.MatchedTradePoint, error) {
	matched := make([]model.MatchedTradePoint, 0, len(points))
	for _, point := range points {
		idx := sort.Search(len(klines), func(i int) bool { return !klines[i].OpenTime.Before(point.Time) })
		if idx >= len(klines) {
			return nil, fmt.Errorf("买卖点 %s 超出 K 线范围", point.Time.Format(time.RFC3339))
		}
		kline := klines[idx]
		matched = append(matched, model.MatchedTradePoint{TradePoint: point, MatchedKlineTime: kline.OpenTime, MatchedPrice: kline.Close})
	}
	return matched, nil
}

func pairTrades(points []model.MatchedTradePoint) ([]model.TradeResult, error) {
	if len(points)%2 != 0 {
		return nil, ErrInvalidTradeFlow
	}
	trades := make([]model.TradeResult, 0, len(points)/2)
	for i := 0; i < len(points); i += 2 {
		buy := points[i]
		sell := points[i+1]
		if buy.Side != model.TradeSideBuy || sell.Side != model.TradeSideSell || !buy.Time.Before(sell.Time) {
			return nil, ErrInvalidTradeFlow
		}
		profit := sell.MatchedPrice - buy.MatchedPrice
		profitRate := 0.0
		if buy.MatchedPrice != 0 {
			profitRate = profit / buy.MatchedPrice
		}
		trades = append(trades, model.TradeResult{Buy: buy, Sell: sell, Profit: profit, ProfitRate: profitRate, HoldingSeconds: int64(sell.Time.Sub(buy.Time).Seconds()), Win: profit > 0})
	}
	return trades, nil
}

func calculateMetrics(trades []model.TradeResult) model.Metrics {
	if len(trades) == 0 {
		return model.Metrics{}
	}
	wins := 0
	totalRate := 0.0
	equity := 1.0
	peak := 1.0
	maxDD := 0.0
	totalHolding := int64(0)
	for _, trade := range trades {
		if trade.Win {
			wins++
		}
		totalRate += trade.ProfitRate
		equity *= 1 + trade.ProfitRate
		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			dd := (peak - equity) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
		totalHolding += trade.HoldingSeconds
	}
	return model.Metrics{TradeCount: len(trades), WinRate: float64(wins) / float64(len(trades)), TotalProfitRate: equity - 1, MaxDrawdownRate: maxDD, AverageHoldingSeconds: totalHolding / int64(len(trades))}
}
