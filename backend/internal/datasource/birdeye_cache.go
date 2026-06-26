package datasource

import (
	"context"
	"database/sql"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/model"
)

type BirdeyeCachedDataSource struct {
	cache    *sql.DB
	upstream KlineDataSource
}

func NewBirdeyeCachedDataSource(cache *sql.DB, upstream KlineDataSource) *BirdeyeCachedDataSource {
	return &BirdeyeCachedDataSource{cache: cache, upstream: upstream}
}

func (s *BirdeyeCachedDataSource) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	if klines, ok, err := s.loadCachedKlines(ctx, req); err != nil {
		return nil, err
	} else if ok {
		return klines, nil
	}
	klines, err := s.upstream.GetKlines(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.saveKlines(ctx, req, klines); err != nil {
		return nil, err
	}
	return klines, nil
}

func (s *BirdeyeCachedDataSource) loadCachedKlines(ctx context.Context, req KlineQuery) ([]model.Kline, bool, error) {
	rows, err := s.cache.QueryContext(ctx, `
		SELECT open_time, close_time, market_cap_open, market_cap_high, market_cap_low, market_cap_close, volume
		FROM birdeye_kline_cache
		WHERE token_address = ?
		  AND interval = ?
		ORDER BY open_time ASC`,
		req.TokenAddress, req.Interval)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	allItems := make([]model.Kline, 0)
	for rows.Next() {
		var openTimeRaw string
		var closeTimeRaw string
		var item model.Kline
		item.TokenAddress = req.TokenAddress
		item.Interval = req.Interval
		if err := rows.Scan(&openTimeRaw, &closeTimeRaw, &item.MarketCapOpen, &item.MarketCapHigh, &item.MarketCapLow, &item.MarketCapClose, &item.Volume); err != nil {
			return nil, false, err
		}
		openTime, err := parseCacheTime(openTimeRaw)
		if err != nil {
			return nil, false, err
		}
		closeTime, err := parseCacheTime(closeTimeRaw)
		if err != nil {
			return nil, false, err
		}
		item.OpenTime = openTime
		item.CloseTime = closeTime
		allItems = append(allItems, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(allItems) == 0 {
		return nil, false, nil
	}
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return allItems, true, nil
	}
	filtered := make([]model.Kline, 0, len(allItems))
	for _, item := range allItems {
		if item.OpenTime.Before(req.StartTime) || item.OpenTime.After(req.EndTime) {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) > 0 {
		return filtered, true, nil
	}
	// A token that has been cached once should keep using that project cache instead of refetching latest bars.
	return allItems, true, nil
}

func (s *BirdeyeCachedDataSource) saveKlines(ctx context.Context, req KlineQuery, klines []model.Kline) error {
	tx, err := s.cache.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := cacheTime(time.Now())
	for _, item := range klines {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO birdeye_kline_cache (
				token_address, interval, open_time, close_time,
				market_cap_open, market_cap_high, market_cap_low, market_cap_close,
				volume, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(token_address, interval, open_time) DO UPDATE SET
				close_time = excluded.close_time,
				market_cap_open = excluded.market_cap_open,
				market_cap_high = excluded.market_cap_high,
				market_cap_low = excluded.market_cap_low,
				market_cap_close = excluded.market_cap_close,
				volume = excluded.volume,
				updated_at = excluded.updated_at`,
			req.TokenAddress,
			req.Interval,
			cacheTime(item.OpenTime),
			cacheTime(item.CloseTime),
			item.MarketCapOpen,
			item.MarketCapHigh,
			item.MarketCapLow,
			item.MarketCapClose,
			item.Volume,
			now,
			now,
		); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO birdeye_kline_cache_ranges (
			token_address, interval, range_start, range_end, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(token_address, interval, range_start, range_end) DO UPDATE SET
			updated_at = excluded.updated_at`,
		req.TokenAddress,
		req.Interval,
		cacheTime(req.StartTime),
		cacheTime(req.EndTime),
		now,
		now,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func cacheTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseCacheTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return apptime.InBeijing(parsed), nil
}
