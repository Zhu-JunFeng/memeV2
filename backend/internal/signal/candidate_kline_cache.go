package signal

import (
	"sort"
	"sync"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

// candidateKlineCache 只保留候选池判定需要的最近 N 根真实K线，
// 用于在 GMGN 单次返回不完整时补回历史已经抓到过的分钟数据。
type candidateKlineCache struct {
	mu     sync.RWMutex
	limit  int
	series map[string][]model.Kline
}

func newCandidateKlineCache(limit int) *candidateKlineCache {
	if limit <= 0 {
		limit = 200
	}
	return &candidateKlineCache{limit: limit, series: map[string][]model.Kline{}}
}

func (c *candidateKlineCache) Get(tokenAddress string, interval string) []model.Kline {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := c.series[c.key(tokenAddress, interval)]
	return append([]model.Kline(nil), items...)
}

func (c *candidateKlineCache) Set(tokenAddress string, interval string, klines []model.Kline) []model.Kline {
	if c == nil {
		return append([]model.Kline(nil), klines...)
	}
	merged := mergeKlinesByOpenTime(nil, klines, c.limit)
	c.mu.Lock()
	c.series[c.key(tokenAddress, interval)] = merged
	c.mu.Unlock()
	return append([]model.Kline(nil), merged...)
}

func (c *candidateKlineCache) MergePreferIncoming(tokenAddress string, interval string, incoming []model.Kline) []model.Kline {
	if c == nil {
		return append([]model.Kline(nil), incoming...)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := c.key(tokenAddress, interval)
	merged := mergeKlinesByOpenTime(c.series[key], incoming, c.limit)
	c.series[key] = merged
	return append([]model.Kline(nil), merged...)
}

func (c *candidateKlineCache) key(tokenAddress string, interval string) string {
	return tokenAddress + "|" + interval
}

func mergeKlinesByOpenTime(existing []model.Kline, incoming []model.Kline, limit int) []model.Kline {
	merged := make(map[int64]model.Kline, len(existing)+len(incoming))
	for _, item := range existing {
		if item.OpenTime.IsZero() {
			continue
		}
		merged[item.OpenTime.UnixMilli()] = item
	}
	for _, item := range incoming {
		if item.OpenTime.IsZero() {
			continue
		}
		merged[item.OpenTime.UnixMilli()] = item
	}
	items := make([]model.Kline, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].OpenTime.Before(items[j].OpenTime)
	})
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func filterKlinesAfter(klines []model.Kline, start time.Time) []model.Kline {
	if start.IsZero() {
		return append([]model.Kline(nil), klines...)
	}
	items := make([]model.Kline, 0, len(klines))
	for _, item := range klines {
		if item.OpenTime.Before(start) {
			continue
		}
		items = append(items, item)
	}
	return items
}
