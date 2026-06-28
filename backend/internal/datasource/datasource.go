package datasource

import (
	"context"
	"errors"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

var ErrUnsupportedKlineSource = errors.New("不支持的 K 线数据源")
var ErrUnsupportedPriceSource = errors.New("不支持的价格数据源")

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
