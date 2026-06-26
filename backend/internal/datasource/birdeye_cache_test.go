package datasource

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

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
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()

	start := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	klines := []model.Kline{
		{TokenAddress: "token-a", Interval: "1m", OpenTime: start, CloseTime: start.Add(time.Minute), MarketCapOpen: 100, MarketCapHigh: 110, MarketCapLow: 95, MarketCapClose: 108, Volume: 10},
		{TokenAddress: "token-a", Interval: "1m", OpenTime: start.Add(time.Minute), CloseTime: start.Add(2 * time.Minute), MarketCapOpen: 108, MarketCapHigh: 114, MarketCapLow: 104, MarketCapClose: 111, Volume: 12},
	}
	upstream := &stubKlineDataSource{klines: klines}
	source := NewBirdeyeCachedDataSource(db, upstream)
	req := KlineQuery{TokenAddress: "token-a", Interval: "1m", StartTime: start, EndTime: start.Add(2 * time.Minute)}

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT open_time, close_time, market_cap_open, market_cap_high, market_cap_low, market_cap_close, volume
		FROM birdeye_kline_cache
		WHERE token_address = $1
		  AND "interval" = $2
		ORDER BY open_time ASC`)).WithArgs("token-a", "1m").WillReturnRows(sqlmock.NewRows([]string{"open_time", "close_time", "market_cap_open", "market_cap_high", "market_cap_low", "market_cap_close", "volume"}))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO birdeye_kline_cache").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO birdeye_kline_cache").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO birdeye_kline_cache_ranges").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

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

	cachedRows := sqlmock.NewRows([]string{"open_time", "close_time", "market_cap_open", "market_cap_high", "market_cap_low", "market_cap_close", "volume"}).
		AddRow(start, start.Add(time.Minute), 100, 110, 95, 108, 10).
		AddRow(start.Add(time.Minute), start.Add(2*time.Minute), 108, 114, 104, 111, 12)
	mock.ExpectQuery("SELECT open_time, close_time").WithArgs("token-a", "1m").WillReturnRows(cachedRows)

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

	cachedSubrangeRows := sqlmock.NewRows([]string{"open_time", "close_time", "market_cap_open", "market_cap_high", "market_cap_low", "market_cap_close", "volume"}).
		AddRow(start, start.Add(time.Minute), 100, 110, 95, 108, 10).
		AddRow(start.Add(time.Minute), start.Add(2*time.Minute), 108, 114, 104, 111, 12)
	mock.ExpectQuery("SELECT open_time, close_time").WithArgs("token-a", "1m").WillReturnRows(cachedSubrangeRows)

	subrange, err := source.GetKlines(context.Background(), KlineQuery{TokenAddress: "token-a", Interval: "1m", StartTime: start.Add(time.Minute), EndTime: start.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("subrange get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected subrange request to use cache, upstream calls=%d", upstream.calls)
	}
	if len(subrange) != 1 {
		t.Fatalf("expected 1 cached kline for subrange, got %d", len(subrange))
	}

	outsideRows := sqlmock.NewRows([]string{"open_time", "close_time", "market_cap_open", "market_cap_high", "market_cap_low", "market_cap_close", "volume"}).
		AddRow(start, start.Add(time.Minute), 100, 110, 95, 108, 10).
		AddRow(start.Add(time.Minute), start.Add(2*time.Minute), 108, 114, 104, 111, 12)
	mock.ExpectQuery("SELECT open_time, close_time").WithArgs("token-a", "1m").WillReturnRows(outsideRows)

	outsideRange, err := source.GetKlines(context.Background(), KlineQuery{TokenAddress: "token-a", Interval: "1m", StartTime: start.Add(24 * time.Hour), EndTime: start.Add(24*time.Hour + time.Minute)})
	if err != nil {
		t.Fatalf("outside range get klines: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected outside range request to keep using existing project cache, upstream calls=%d", upstream.calls)
	}
	if len(outsideRange) != len(first) {
		t.Fatalf("expected outside range request to return existing cached project klines, got %d", len(outsideRange))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
