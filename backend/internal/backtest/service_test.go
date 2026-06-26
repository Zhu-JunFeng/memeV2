package backtest

import (
	"context"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/model"
)

type fakeKlines struct{ items []model.Kline }

func (f fakeKlines) GetKlines(context.Context, datasource.KlineQuery) ([]model.Kline, error) {
	return f.items, nil
}
func (f fakeKlines) SearchTokens(context.Context, string, int) ([]model.Token, error) {
	return nil, nil
}

type fakeTradePoints struct{}

func (fakeTradePoints) GetTradePoints(context.Context, datasource.TradePointQuery) ([]model.TradePoint, error) {
	return nil, nil
}

type fakeRepo struct{}

func (fakeRepo) SaveAnalysis(context.Context, SaveAnalysisInput) error { return nil }
func (fakeRepo) GetAnalysis(context.Context, string) (SavedAnalysis, error) {
	return SavedAnalysis{}, nil
}
func (fakeRepo) ListAnalyses(context.Context, int) ([]model.BacktestSession, error) { return nil, nil }

func TestAnalyzeCalculatesMetrics(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	svc := NewService(fakeKlines{items: []model.Kline{
		{OpenTime: base, CloseTime: base.Add(time.Minute), Close: 1, MarketCapClose: 1},
		{OpenTime: base.Add(time.Minute), CloseTime: base.Add(2 * time.Minute), Close: 2, MarketCapClose: 2},
		{OpenTime: base.Add(2 * time.Minute), CloseTime: base.Add(3 * time.Minute), Close: 1, MarketCapClose: 1},
		{OpenTime: base.Add(3 * time.Minute), CloseTime: base.Add(4 * time.Minute), Close: 0.5, MarketCapClose: 0.5},
	}}, fakeKlines{}, fakeKlines{}, fakeTradePoints{}, fakeTradePoints{}, fakeTradePoints{}, fakeKlines{}, fakeRepo{})
	result, err := svc.Analyze(context.Background(), AnalyzeRequest{SessionID: "s1", TokenAddress: "token", Interval: "1m", StartTime: base, EndTime: base.Add(4 * time.Minute), TradePoints: []model.TradePoint{
		{Side: model.TradeSideBuy, Time: base},
		{Side: model.TradeSideSell, Time: base.Add(time.Minute)},
		{Side: model.TradeSideBuy, Time: base.Add(2 * time.Minute)},
		{Side: model.TradeSideSell, Time: base.Add(3 * time.Minute)},
	}})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if result.Metrics.TradeCount != 2 {
		t.Fatalf("TradeCount = %d", result.Metrics.TradeCount)
	}
	if result.Metrics.WinRate != 0.5 {
		t.Fatalf("WinRate = %f", result.Metrics.WinRate)
	}
	if result.Metrics.TotalProfitRate != 0 {
		t.Fatalf("TotalProfitRate = %f", result.Metrics.TotalProfitRate)
	}
	if result.Metrics.MaxDrawdownRate != 0.5 {
		t.Fatalf("MaxDrawdownRate = %f", result.Metrics.MaxDrawdownRate)
	}
}

func TestAnalyzeRejectsInvalidTradeFlow(t *testing.T) {
	base := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	svc := NewService(fakeKlines{items: []model.Kline{{OpenTime: base, Close: 1, MarketCapClose: 1}, {OpenTime: base.Add(time.Minute), Close: 2, MarketCapClose: 2}}}, fakeKlines{}, fakeKlines{}, fakeTradePoints{}, fakeTradePoints{}, fakeTradePoints{}, fakeKlines{}, fakeRepo{})
	_, err := svc.Analyze(context.Background(), AnalyzeRequest{SessionID: "s1", TokenAddress: "token", Interval: "1m", StartTime: base, EndTime: base.Add(time.Minute), TradePoints: []model.TradePoint{{Side: model.TradeSideSell, Time: base}, {Side: model.TradeSideBuy, Time: base.Add(time.Minute)}}})
	if err != ErrInvalidTradeFlow {
		t.Fatalf("expected ErrInvalidTradeFlow, got %v", err)
	}
}
