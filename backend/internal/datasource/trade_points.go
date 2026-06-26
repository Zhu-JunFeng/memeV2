package datasource

import (
	"context"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

type TradePointDataSource interface {
	GetTradePoints(ctx context.Context, req TradePointQuery) ([]model.TradePoint, error)
}

type TradePointQuery struct {
	TokenAddress  string
	WalletAddress string
	StartTime     time.Time
	EndTime       time.Time
	MaxPages      int
}
