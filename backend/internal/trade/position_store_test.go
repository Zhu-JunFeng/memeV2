package trade

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/redis/go-redis/v9"

	"solana-meme-backtest/backend/internal/model"
)

type fakeRedisHashClient struct {
	hsetKey    string
	hsetValues []any
	values     map[string]string
}

func (c *fakeRedisHashClient) HSet(_ context.Context, key string, values ...any) *redis.IntCmd {
	c.hsetKey = key
	c.hsetValues = append([]any(nil), values...)
	if c.values == nil {
		c.values = map[string]string{}
	}
	if len(values) == 2 {
		c.values[values[0].(string)] = values[1].(string)
	}
	return redis.NewIntResult(1, nil)
}

func (c *fakeRedisHashClient) HGet(_ context.Context, _ string, field string) *redis.StringCmd {
	value, ok := c.values[field]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (c *fakeRedisHashClient) HDel(_ context.Context, _ string, fields ...string) *redis.IntCmd {
	for _, field := range fields {
		delete(c.values, field)
	}
	return redis.NewIntResult(int64(len(fields)), nil)
}

func TestRedisPositionStoreSavesPositionInPersistentAccountHash(t *testing.T) {
	client := &fakeRedisHashClient{}
	store := NewRedisPositionStore(client, "solana_meme_v2:trade:open_positions")
	position := model.TradePosition{ID: "position-1", AccountID: "account-1", TokenAddress: "token-1", Quantity: 321.5, Status: model.TradePositionStatusOpen}

	if err := store.Save(context.Background(), position); err != nil {
		t.Fatal(err)
	}
	if client.hsetKey != "solana_meme_v2:trade:open_positions:account-1" {
		t.Fatalf("unexpected Redis key: %s", client.hsetKey)
	}
	if len(client.hsetValues) != 2 || client.hsetValues[0] != "token-1" {
		t.Fatalf("unexpected HSET values: %#v", client.hsetValues)
	}
	var stored model.TradePosition
	if err := json.Unmarshal([]byte(client.hsetValues[1].(string)), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Quantity != 321.5 {
		t.Fatalf("unexpected stored position: %#v", stored)
	}
}
