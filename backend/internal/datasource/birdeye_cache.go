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
		WHERE token_address = $1
		  AND "interval" = $2
		ORDER BY open_time ASC`,
		req.TokenAddress, req.Interval)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	allItems := make([]model.Kline, 0)
	for rows.Next() {
		var item model.Kline
		item.TokenAddress = req.TokenAddress
		item.Interval = req.Interval
		if err := rows.Scan(&item.OpenTime, &item.CloseTime, &item.MarketCapOpen, &item.MarketCapHigh, &item.MarketCapLow, &item.MarketCapClose, &item.Volume); err != nil {
			return nil, false, err
		}
		item.OpenTime = apptime.InBeijing(item.OpenTime)
		item.CloseTime = apptime.InBeijing(item.CloseTime)
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
	// 只要这个项目已经缓存过，就继续复用该项目缓存，不主动向上游追最新 K 线。
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
	now := time.Now().UTC()
	for _, item := range klines {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO birdeye_kline_cache (
				token_address, "interval", open_time, close_time,
				market_cap_open, market_cap_high, market_cap_low, market_cap_close,
				volume, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT(token_address, "interval", open_time) DO UPDATE SET
				close_time = excluded.close_time,
				market_cap_open = excluded.market_cap_open,
				market_cap_high = excluded.market_cap_high,
				market_cap_low = excluded.market_cap_low,
				market_cap_close = excluded.market_cap_close,
				volume = excluded.volume,
				updated_at = excluded.updated_at`,
			req.TokenAddress,
			req.Interval,
			item.OpenTime.UTC(),
			item.CloseTime.UTC(),
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
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO birdeye_kline_cache_ranges (
			token_address, "interval", range_start, range_end, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(token_address, "interval", range_start, range_end) DO UPDATE SET
			updated_at = excluded.updated_at`,
		req.TokenAddress,
		req.Interval,
		req.StartTime.UTC(),
		req.EndTime.UTC(),
		now,
		now,
	); err != nil {
		return err
	}
	return tx.Commit()
}
