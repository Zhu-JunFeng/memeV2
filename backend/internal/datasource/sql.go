package datasource

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"solana-meme-backtest/backend/internal/model"
)

var ErrQueryNotConfigured = errors.New("数据源查询 SQL 未配置")

type SQLDataSource struct {
	db               *sql.DB
	klineQuery       string
	tokenSearchQuery string
}

func NewSQLDataSource(db *sql.DB, klineQuery, tokenSearchQuery string) *SQLDataSource {
	return &SQLDataSource{db: db, klineQuery: strings.TrimSpace(klineQuery), tokenSearchQuery: strings.TrimSpace(tokenSearchQuery)}
}

func (s *SQLDataSource) GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error) {
	if s.klineQuery == "" {
		return nil, ErrQueryNotConfigured
	}
	rows, err := s.db.QueryContext(ctx, s.klineQuery, req.TokenAddress, req.Interval, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Kline, 0)
	for rows.Next() {
		var item model.Kline
		if err := rows.Scan(&item.TokenAddress, &item.Interval, &item.OpenTime, &item.CloseTime, &item.Open, &item.High, &item.Low, &item.Close, &item.Volume); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLDataSource) SearchTokens(ctx context.Context, keyword string, limit int) ([]model.Token, error) {
	if s.tokenSearchQuery == "" {
		return nil, ErrQueryNotConfigured
	}
	rows, err := s.db.QueryContext(ctx, s.tokenSearchQuery, keyword, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Token, 0)
	for rows.Next() {
		var item model.Token
		if err := rows.Scan(&item.Address, &item.Symbol, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
