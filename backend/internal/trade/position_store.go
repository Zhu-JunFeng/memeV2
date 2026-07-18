package trade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/model"
)

var ErrRuntimePositionNotFound = errors.New("Redis 实时持仓不存在")

const defaultRedisPositionKeyPrefix = "solana_meme_v2:trade:open_positions"

type PositionStore interface {
	Save(ctx context.Context, position model.TradePosition) error
	Get(ctx context.Context, accountID string, tokenAddress string) (model.TradePosition, error)
	List(ctx context.Context, accountID string) ([]model.TradePosition, error)
	Delete(ctx context.Context, accountID string, tokenAddress string) error
}

type redisHashClient interface {
	HSet(ctx context.Context, key string, values ...any) *redis.IntCmd
	HGet(ctx context.Context, key string, field string) *redis.StringCmd
	HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd
	HDel(ctx context.Context, key string, fields ...string) *redis.IntCmd
}

type RedisPositionStore struct {
	client redisHashClient
	prefix string
}

func NewRedisPositionStore(client redisHashClient, prefix string) *RedisPositionStore {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultRedisPositionKeyPrefix
	}
	return &RedisPositionStore{client: client, prefix: strings.TrimSuffix(prefix, ":")}
}

func (s *RedisPositionStore) Save(ctx context.Context, position model.TradePosition) error {
	if s == nil || s.client == nil {
		return errors.New("Redis 持仓存储未配置")
	}
	payload, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("序列化 Redis 持仓失败: %w", err)
	}
	return s.client.HSet(ctx, s.key(position.AccountID), position.TokenAddress, string(payload)).Err()
}

func (s *RedisPositionStore) Get(ctx context.Context, accountID string, tokenAddress string) (model.TradePosition, error) {
	if s == nil || s.client == nil {
		return model.TradePosition{}, errors.New("Redis 持仓存储未配置")
	}
	payload, err := s.client.HGet(ctx, s.key(accountID), tokenAddress).Result()
	if errors.Is(err, redis.Nil) {
		return model.TradePosition{}, ErrRuntimePositionNotFound
	}
	if err != nil {
		return model.TradePosition{}, err
	}
	var position model.TradePosition
	if err := json.Unmarshal([]byte(payload), &position); err != nil {
		return model.TradePosition{}, fmt.Errorf("解析 Redis 持仓失败: %w", err)
	}
	return position, nil
}

func (s *RedisPositionStore) List(ctx context.Context, accountID string) ([]model.TradePosition, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Redis 持仓存储未配置")
	}
	values, err := s.client.HGetAll(ctx, s.key(accountID)).Result()
	if err != nil {
		return nil, err
	}
	positions := make([]model.TradePosition, 0, len(values))
	for tokenAddress, payload := range values {
		var position model.TradePosition
		if err := json.Unmarshal([]byte(payload), &position); err != nil {
			return nil, fmt.Errorf("解析 Redis 持仓失败: ca=%s: %w", tokenAddress, err)
		}
		if position.Status == model.TradePositionStatusOpen {
			positions = append(positions, position)
		}
	}
	return positions, nil
}

func (s *RedisPositionStore) Delete(ctx context.Context, accountID string, tokenAddress string) error {
	if s == nil || s.client == nil {
		return errors.New("Redis 持仓存储未配置")
	}
	return s.client.HDel(ctx, s.key(accountID), tokenAddress).Err()
}

func (s *RedisPositionStore) key(accountID string) string {
	return s.prefix + ":" + accountID
}
