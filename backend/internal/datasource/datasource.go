package datasource

import (
	"context"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

type KlineDataSource interface {
	GetKlines(ctx context.Context, req KlineQuery) ([]model.Kline, error)
}

type TokenDataSource interface {
	SearchTokens(ctx context.Context, keyword string, limit int) ([]model.Token, error)
}

type KlineQuery struct {
	TokenAddress string
	Interval     string
	StartTime    time.Time
	EndTime      time.Time
}
