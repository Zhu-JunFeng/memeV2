package trade

import (
	"context"
	"encoding/json"
	"fmt"
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

type redisSignalEnvelope struct {
	Event string `json:"event"`
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
		if _, err := pubsub.Receive(ctx); err != nil {
			log.Printf("trade worker subscribe redis channel failed: channel=%s err=%v", w.channel, err)
			return
		}
		log.Printf("trade worker subscribed redis channel: %s", w.channel)
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				signal, err := decodeTradeSignalPayload([]byte(msg.Payload))
				if err != nil {
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

func decodeTradeSignalPayload(payload []byte) (model.TradeSignalMessage, error) {
	var envelope redisSignalEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return model.TradeSignalMessage{}, err
	}
	switch envelope.Event {
	case "":
		var signal model.TradeSignalMessage
		if err := json.Unmarshal(payload, &signal); err != nil {
			return model.TradeSignalMessage{}, err
		}
		return signal, nil
	default:
		return model.TradeSignalMessage{}, fmt.Errorf("unsupported redis signal event: %s", envelope.Event)
	}
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
