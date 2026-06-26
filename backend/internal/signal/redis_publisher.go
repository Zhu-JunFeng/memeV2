package signal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/backtest"
)

type RedisPublisher struct {
	client  *redis.Client
	channel string
}

type redisSignalEnvelope struct {
	Type       string                            `json:"type"`
	OccurredAt time.Time                         `json:"occurredAt"`
	Signals    []backtest.RealtimeScenarioSignal `json:"signals"`
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

// PublishRealtimeSignals 当前采用 Redis Pub/Sub 发送实时信号，
// Web 侧或独立消费程序可以直接订阅 channel 获取 JSON 消息。
func (p *RedisPublisher) PublishRealtimeSignals(ctx context.Context, signals []backtest.RealtimeScenarioSignal) error {
	if len(signals) == 0 {
		return nil
	}
	payload, err := json.Marshal(redisSignalEnvelope{
		Type:       "pressure_breakout_signals",
		OccurredAt: time.Now(),
		Signals:    signals,
	})
	if err != nil {
		return err
	}
	return p.client.Publish(ctx, p.channel, payload).Err()
}
