package signal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/backtest"
	"solana-meme-backtest/backend/internal/model"
)

type RedisPublisher struct {
	client  *redis.Client
	channel string
}

func NewRedisPublisher(addr string, password string, db int, channel string) *RedisPublisher {
	if channel == "" {
		channel = "solana:meme:signals:pressure_breakout"
	}
	return &RedisPublisher{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		channel: channel,
	}
}

// PublishRealtimeSignals 把结构突破信号转换成交易模块统一消费的消息，
// 这样后续无论是 Redis 消费端还是手动重放接口，都能复用同一份消息结构。
func (p *RedisPublisher) PublishRealtimeSignals(ctx context.Context, tokenAddress string, interval string, signals []backtest.RealtimeScenarioSignal) error {
	for _, item := range signals {
		message, err := toTradeSignalMessage(tokenAddress, interval, item)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if err := p.client.Publish(ctx, p.channel, payload).Err(); err != nil {
			return err
		}
	}
	return nil
}

func toTradeSignalMessage(tokenAddress string, interval string, item backtest.RealtimeScenarioSignal) (model.TradeSignalMessage, error) {
	metadata, err := json.Marshal(map[string]any{
		"windowIndex":         item.WindowIndex,
		"levelIndex":          item.LevelIndex,
		"levelType":           item.LevelType,
		"levelMarketCap":      item.LevelMarketCap,
		"levelLowerMarketCap": item.LevelLowerMarketCap,
		"levelUpperMarketCap": item.LevelUpperMarketCap,
		"breakoutThreshold":   item.BreakoutThreshold,
		"calculation":         item.Calculation,
		"breakout":            item.Breakout,
	})
	if err != nil {
		return model.TradeSignalMessage{}, err
	}
	return model.TradeSignalMessage{
		SignalID:         uuid.NewString(),
		SignalType:       model.TradeSignalTypeBuy,
		StrategyCode:     item.ScenarioCode,
		TokenAddress:     tokenAddress,
		Interval:         interval,
		SignalTime:       item.SignalTime,
		TriggerPrice:     item.SignalMarketCap,
		TriggerMarketCap: item.SignalMarketCap,
		Reason:           fmt.Sprintf("%s: %s", item.ScenarioName, item.Reason),
		Metadata:         metadata,
	}, nil
}
