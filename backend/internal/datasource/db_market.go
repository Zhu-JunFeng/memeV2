package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/model"
)

type DBBarDataSource struct {
	db *sql.DB
}

type DBTradePointDataSource struct {
	db *sql.DB
}

type barPriceData struct {
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

func NewDBBarDataSource(db *sql.DB) *DBBarDataSource {
	return &DBBarDataSource{db: db}
}

func NewDBTradePointDataSource(db *sql.DB) *DBTradePointDataSource {
	return &DBTradePointDataSource{db: db}
}

func (s *DBBarDataSource) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	start, end, err := s.resolveRange(ctx, req)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT start_ts, usd_data
		FROM bar_data
		WHERE pair_id = $1
		  AND "interval" = $2
		  AND start_ts >= $3
		  AND start_ts <= $4
		ORDER BY start_ts ASC`, req.TokenAddress, req.Interval, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Kline, 0)
	for rows.Next() {
		var startTS int64
		var raw string
		if err := rows.Scan(&startTS, &raw); err != nil {
			return nil, err
		}
		var price barPriceData
		if err := json.Unmarshal([]byte(raw), &price); err != nil {
			return nil, err
		}
		openTime := apptime.InBeijing(time.UnixMilli(startTS))
		items = append(items, model.Kline{
			TokenAddress: req.TokenAddress,
			Interval:     req.Interval,
			OpenTime:     openTime,
			CloseTime:    closeTime(openTime, req.Interval),
			Open:         price.Open,
			High:         price.High,
			Low:          price.Low,
			Close:        price.Close,
			Volume:       price.Volume,
		})
	}
	return items, rows.Err()
}

func (s *DBBarDataSource) resolveRange(ctx context.Context, req KlineQuery) (int64, int64, error) {
	if !req.StartTime.IsZero() && !req.EndTime.IsZero() {
		return req.StartTime.UnixMilli(), req.EndTime.UnixMilli(), nil
	}
	var start sql.NullInt64
	var end sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(start_ts), MAX(start_ts) FROM bar_data WHERE pair_id = $1 AND "interval" = $2`, req.TokenAddress, req.Interval).Scan(&start, &end)
	if err != nil {
		return 0, 0, err
	}
	return start.Int64, end.Int64, nil
}

func (s *DBTradePointDataSource) GetTradePoints(ctx context.Context, req TradePointQuery) ([]model.TradePoint, error) {
	start := req.StartTime.UnixMilli()
	end := req.EndTime.UnixMilli()
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		var minTS sql.NullInt64
		var maxTS sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT MIN(timestamp), MAX(timestamp) FROM trade_data WHERE pair_id = $1 AND "user" = $2`, req.TokenAddress, req.WalletAddress).Scan(&minTS, &maxTS); err != nil {
			return nil, err
		}
		start = minTS.Int64
		end = maxTS.Int64
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp, is_buy, price_usd, signature
		FROM trade_data
		WHERE pair_id = $1
		  AND "user" = $2
		  AND timestamp >= $3
		  AND timestamp <= $4
		ORDER BY timestamp ASC`, req.TokenAddress, req.WalletAddress, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]model.TradePoint, 0)
	for rows.Next() {
		var timestamp int64
		var isBuy int
		var price float64
		var signature string
		if err := rows.Scan(&timestamp, &isBuy, &price, &signature); err != nil {
			return nil, err
		}
		side := model.TradeSideSell
		if isBuy == 1 {
			side = model.TradeSideBuy
		}
		points = append(points, model.TradePoint{Side: side, Time: apptime.InBeijing(time.UnixMilli(timestamp)), Price: &price, Note: signature})
	}
	return points, rows.Err()
}

func closeTime(openTime time.Time, interval string) time.Time {
	switch interval {
	case "1m":
		return openTime.Add(time.Minute)
	case "5m":
		return openTime.Add(5 * time.Minute)
	case "15m":
		return openTime.Add(15 * time.Minute)
	case "1h":
		return openTime.Add(time.Hour)
	default:
		return openTime
	}
}
