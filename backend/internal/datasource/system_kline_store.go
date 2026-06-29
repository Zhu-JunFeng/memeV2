package datasource

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"sync"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

type SystemKlineStore struct {
	db            *sql.DB
	flushSize     int
	flushInterval time.Duration
	queue         chan []model.Kline
	once          sync.Once
}

func NewSystemKlineStore(db *sql.DB) *SystemKlineStore {
	return &SystemKlineStore{
		db:            db,
		flushSize:     200,
		flushInterval: 2 * time.Second,
		queue:         make(chan []model.Kline, 256),
	}
}

func (s *SystemKlineStore) Start(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	s.once.Do(func() {
		go s.run(ctx)
	})
}

func (s *SystemKlineStore) EnqueueUpsert(klines []model.Kline) {
	if s == nil || s.db == nil || len(klines) == 0 {
		return
	}
	copied := append([]model.Kline(nil), klines...)
	select {
	case s.queue <- copied:
	default:
		go s.SaveKlines(context.Background(), copied)
	}
}

func (s *SystemKlineStore) SaveKlines(ctx context.Context, klines []model.Kline) error {
	if s == nil || s.db == nil || len(klines) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ranges := map[string]struct {
		token, interval string
		start, end      time.Time
	}{}
	for _, item := range klines {
		if strings.TrimSpace(item.TokenAddress) == "" || strings.TrimSpace(item.Interval) == "" || item.OpenTime.IsZero() {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO system_kline_cache (
                token_address, "interval", open_time, close_time,
                open_price, high_price, low_price, close_price,
                market_cap_open, market_cap_high, market_cap_low, market_cap_close,
                volume, created_at, updated_at
            ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),NOW())
            ON CONFLICT (token_address, "interval", open_time) DO UPDATE SET
                close_time = EXCLUDED.close_time,
                open_price = EXCLUDED.open_price,
                high_price = EXCLUDED.high_price,
                low_price = EXCLUDED.low_price,
                close_price = EXCLUDED.close_price,
                market_cap_open = EXCLUDED.market_cap_open,
                market_cap_high = EXCLUDED.market_cap_high,
                market_cap_low = EXCLUDED.market_cap_low,
                market_cap_close = EXCLUDED.market_cap_close,
                volume = EXCLUDED.volume,
                updated_at = NOW()`,
			item.TokenAddress, item.Interval, item.OpenTime, item.CloseTime,
			item.Open, item.High, item.Low, item.Close,
			item.MarketCapOpen, item.MarketCapHigh, item.MarketCapLow, item.MarketCapClose,
			item.Volume,
		); err != nil {
			return err
		}
		key := item.TokenAddress + "|" + item.Interval
		current, ok := ranges[key]
		if !ok {
			ranges[key] = struct {
				token, interval string
				start, end      time.Time
			}{item.TokenAddress, item.Interval, item.OpenTime, item.CloseTime}
			continue
		}
		if item.OpenTime.Before(current.start) {
			current.start = item.OpenTime
		}
		if item.CloseTime.After(current.end) {
			current.end = item.CloseTime
		}
		ranges[key] = current
	}

	for _, item := range ranges {
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO system_kline_cache_ranges (token_address, "interval", range_start, range_end, created_at, updated_at)
            VALUES ($1,$2,$3,$4,NOW(),NOW())
            ON CONFLICT (token_address, "interval") DO UPDATE SET
                range_start = LEAST(system_kline_cache_ranges.range_start, EXCLUDED.range_start),
                range_end = GREATEST(system_kline_cache_ranges.range_end, EXCLUDED.range_end),
                updated_at = NOW()`,
			item.token, item.interval, item.start, item.end,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SystemKlineStore) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	query := `
        SELECT open_time, close_time, open_price, high_price, low_price, close_price,
               market_cap_open, market_cap_high, market_cap_low, market_cap_close, volume
        FROM system_kline_cache
        WHERE token_address = $1 AND "interval" = $2`
	args := []any{req.TokenAddress, req.Interval}
	if !req.StartTime.IsZero() {
		query += ` AND open_time >= $3`
		args = append(args, req.StartTime)
		if !req.EndTime.IsZero() {
			query += ` AND open_time <= $4`
			args = append(args, req.EndTime)
		}
	} else if !req.EndTime.IsZero() {
		query += ` AND open_time <= $3`
		args = append(args, req.EndTime)
	}
	query += ` ORDER BY open_time ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Kline, 0)
	for rows.Next() {
		var item model.Kline
		item.TokenAddress = req.TokenAddress
		item.Interval = req.Interval
		if err := rows.Scan(&item.OpenTime, &item.CloseTime, &item.Open, &item.High, &item.Low, &item.Close, &item.MarketCapOpen, &item.MarketCapHigh, &item.MarketCapLow, &item.MarketCapClose, &item.Volume); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SystemKlineStore) GetRecentKlines(ctx context.Context, tokenAddress string, interval string, limit int) ([]model.Kline, error) {
	if s == nil || s.db == nil || limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT open_time, close_time, open_price, high_price, low_price, close_price,
               market_cap_open, market_cap_high, market_cap_low, market_cap_close, volume
        FROM system_kline_cache
        WHERE token_address = $1 AND "interval" = $2
        ORDER BY open_time DESC
        LIMIT $3`, tokenAddress, interval, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Kline, 0, limit)
	for rows.Next() {
		item := model.Kline{TokenAddress: tokenAddress, Interval: interval}
		if err := rows.Scan(&item.OpenTime, &item.CloseTime, &item.Open, &item.High, &item.Low, &item.Close, &item.MarketCapOpen, &item.MarketCapHigh, &item.MarketCapLow, &item.MarketCapClose, &item.Volume); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OpenTime.Before(items[j].OpenTime) })
	return items, nil
}

func (s *SystemKlineStore) run(ctx context.Context) {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	batch := make([]model.Kline, 0, s.flushSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = s.SaveKlines(context.Background(), batch)
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case items := <-s.queue:
			batch = append(batch, items...)
			if len(batch) >= s.flushSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
