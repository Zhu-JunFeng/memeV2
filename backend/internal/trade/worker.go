package trade

import (
	"context"
	"encoding/json"
	"errors"
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

type candidateScorePassedMessage struct {
	Event          string          `json:"event"`
	RunID          string          `json:"runId"`
	StrategyName   string          `json:"strategyName"`
	ScanNo         int64           `json:"scanNo"`
	Token          string          `json:"token"`
	TokenAddress   string          `json:"tokenAddress"`
	PairAddress    string          `json:"pairAddress"`
	Score          float64         `json:"score"`
	Liquidity      float64         `json:"liquidity"`
	MarketCap      float64         `json:"marketCap"`
	SignalPrice    float64         `json:"signalPrice"`
	SignalVolumeM5 float64         `json:"signalVolumeM5"`
	BuyRatio       float64         `json:"buyRatio"`
	PriceChange5m  float64         `json:"priceChange5m"`
	Volume24h      float64         `json:"volume24h"`
	ObservedAt     int64           `json:"observedAt"`
	ExpiresAt      int64           `json:"expiresAt"`
	PublishedAt    int64           `json:"publishedAt"`
	Pullback       json.RawMessage `json:"pullback,omitempty"`
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
	case "candidate_score_passed":
		return decodeCandidateScorePassed(payload)
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

func decodeCandidateScorePassed(payload []byte) (model.TradeSignalMessage, error) {
	var candidate candidateScorePassedMessage
	if err := json.Unmarshal(payload, &candidate); err != nil {
		return model.TradeSignalMessage{}, err
	}
	if candidate.RunID == "" {
		return model.TradeSignalMessage{}, errors.New("candidate_score_passed missing runId")
	}
	if candidate.TokenAddress == "" {
		return model.TradeSignalMessage{}, errors.New("candidate_score_passed missing tokenAddress")
	}
	if candidate.PublishedAt <= 0 {
		return model.TradeSignalMessage{}, errors.New("candidate_score_passed missing publishedAt")
	}
	signalID := fmt.Sprintf("candidate_score_passed:%s:%d:%s:%d", candidate.RunID, candidate.ScanNo, candidate.TokenAddress, candidate.PublishedAt)
	reason := fmt.Sprintf("候选池评分合格: strategy=%s score=%.2f scanNo=%d", candidate.StrategyName, candidate.Score, candidate.ScanNo)
	return model.TradeSignalMessage{
		SignalID:         signalID,
		SignalType:       model.TradeSignalTypeBuy,
		StrategyCode:     "candidate_score_passed",
		TokenAddress:     candidate.TokenAddress,
		Interval:         "candidate_pool",
		SignalTime:       time.UnixMilli(candidate.PublishedAt).UTC(),
		TriggerPrice:     candidate.SignalPrice,
		TriggerMarketCap: candidate.MarketCap,
		Reason:           reason,
		Metadata:         json.RawMessage(payload),
	}, nil
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
