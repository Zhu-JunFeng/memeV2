package trade

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/model"
)

type Worker struct {
	service *Service
	redis   *redis.Client
	channel string
}

func NewWorker(service *Service, redisClient *redis.Client, channel string) *Worker {
	return &Worker{service: service, redis: redisClient, channel: channel}
}

func (w *Worker) StartSignalConsumer(ctx context.Context) {
	if w == nil || w.service == nil || !w.service.Enabled() || w.redis == nil || w.channel == "" {
		return
	}
	go func() {
		pubsub := w.redis.Subscribe(ctx, w.channel)
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var signal model.TradeSignalMessage
				if err := json.Unmarshal([]byte(msg.Payload), &signal); err != nil {
					log.Printf("trade worker unmarshal signal failed: %v", err)
					continue
				}
				if _, err := w.service.ProcessSignal(ctx, signal); err != nil {
					log.Printf("trade worker process signal failed: %v", err)
				}
			}
		}
	}()
}

func (w *Worker) StartPriceSync(ctx context.Context, interval time.Duration) {
	if w == nil || w.service == nil || !w.service.Enabled() || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.service.RefreshOpenPositions(ctx); err != nil {
					log.Printf("trade worker refresh positions failed: %v", err)
				}
			}
		}
	}()
}
