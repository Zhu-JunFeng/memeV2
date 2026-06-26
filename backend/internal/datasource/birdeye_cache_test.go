package datasource

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/db"
	"solana-meme-backtest/backend/internal/model"
)

type stubKlineDataSource struct {
	calls  int
	klines []model.Kline
}

func (s *stubKlineDataSource) GetKlines(_ context.Context, _ KlineQuery) ([]model.Kline, error) {
	s.calls++
	items := make([]model.Kline, len(s.klines))
	copy(items, s.klines)
	return items, nil
}

func TestBirdeyeCachedDataSourceCachesProjectAndAlwaysReusesIt(t *testing.T) {
	t.Parallel()
	cacheDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "birdeye-cache.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite cache: %v", err)
	}
	start := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{
			TokenAddress:   "token-a",
			Interval:       "1m",
			OpenTime:       start,
			CloseTime:      start.Add(time.Minute),
			MarketCapOpen:  100,
			MarketCapHigh:  110,
			MarketCapLow:   95,
			MarketCapClose: 108,
			Volume:         10,
		},
		{
			TokenAddress:   "token-a",
			Interval:       "1m",
			OpenTime:       start.Add(time.Minute),
			CloseTime:      start.Add(2 * time.Minute),
			MarketCapOpen:  108,
			MarketCapHigh:  114,
			MarketCapLow:   104,
			MarketCapClose: 111,
			Volume:         12,
		},
	}
	upstream := &stubKlineDataSource{klines: klines}
	source := NewBirdeyeCachedDataSource(cacheDB, upstream)
	req := KlineQuery{
		TokenAddress: "token-a",
		Interval:     "1m",
		StartTime:    start,
		EndTime:      start.Add(2 * time.Minute),
	}

	first, err := source.GetKlines(context.Background(), req)
	if err != nil {
		t.Fatalf("first get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected first call to hit upstream once, got %d", upstream.calls)
	}
	if len(first) != 2 {
		t.Fatalf("expected 2 klines on first call, got %d", len(first))
	}

	second, err := source.GetKlines(context.Background(), req)
	if err != nil {
		t.Fatalf("second get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected second call to use cache, upstream calls=%d", upstream.calls)
	}
	if len(second) != len(first) {
		t.Fatalf("expected cached result length %d, got %d", len(first), len(second))
	}

	subrangeReq := KlineQuery{
		TokenAddress: "token-a",
		Interval:     "1m",
		StartTime:    start.Add(time.Minute),
		EndTime:      start.Add(2 * time.Minute),
	}
	subrange, err := source.GetKlines(context.Background(), subrangeReq)
	if err != nil {
		t.Fatalf("subrange get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected subrange request to use covering cache, upstream calls=%d", upstream.calls)
	}
	if len(subrange) != 1 {
		t.Fatalf("expected 1 cached kline for subrange, got %d", len(subrange))
	}

	outsideRangeReq := KlineQuery{
		TokenAddress: "token-a",
		Interval:     "1m",
		StartTime:    start.Add(24 * time.Hour),
		EndTime:      start.Add(24*time.Hour + time.Minute),
	}
	outsideRange, err := source.GetKlines(context.Background(), outsideRangeReq)
	if err != nil {
		t.Fatalf("outside range get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected outside range request to keep using existing project cache, upstream calls=%d", upstream.calls)
	}
	if len(outsideRange) != len(first) {
		t.Fatalf("expected outside range request to return existing cached project klines, got %d", len(outsideRange))
	}
}
