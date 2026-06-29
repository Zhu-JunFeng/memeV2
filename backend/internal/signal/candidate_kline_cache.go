package signal

import (
	"sort"
	"sync"
	"time"

	"solana-meme-backtest/backend/internal/apptime"
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

func (c *candidateKlineCache) ApplyPriceSample(tokenAddress string, interval string, sampleAt time.Time, marketCap float64) ([]model.Kline, model.Kline) {
	if c == nil {
		bar := buildCandidateBar(sampleAt, tokenAddress, interval, marketCap)
		return []model.Kline{bar}, bar
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := c.key(tokenAddress, interval)
	items := append([]model.Kline(nil), c.series[key]...)
	bar := buildCandidateBar(sampleAt, tokenAddress, interval, marketCap)
	if len(items) == 0 {
		items = []model.Kline{bar}
		c.series[key] = items
		return append([]model.Kline(nil), items...), bar
	}
	last := items[len(items)-1]
	switch {
	case last.OpenTime.Equal(bar.OpenTime):
		if last.MarketCapOpen <= 0 {
			last.MarketCapOpen = marketCap
		}
		if last.Open <= 0 {
			last.Open = marketCap
		}
		if last.MarketCapHigh <= 0 || marketCap > last.MarketCapHigh {
			last.MarketCapHigh = marketCap
			last.High = marketCap
		}
		if last.MarketCapLow <= 0 || marketCap < last.MarketCapLow {
			last.MarketCapLow = marketCap
			last.Low = marketCap
		}
		last.MarketCapClose = marketCap
		last.Close = marketCap
		last.CloseTime = bar.CloseTime
		last.Volume++
		items[len(items)-1] = last
		bar = last
	case last.OpenTime.Before(bar.OpenTime):
		bar.Volume = 1
		items = append(items, bar)
	default:
		items = mergeKlinesByOpenTime(items, []model.Kline{bar}, c.limit)
		bar = items[len(items)-1]
	}
	if c.limit > 0 && len(items) > c.limit {
		items = items[len(items)-c.limit:]
	}
	c.series[key] = items
	return append([]model.Kline(nil), items...), bar
}

func (c *candidateKlineCache) key(tokenAddress string, interval string) string {
	return tokenAddress + "|" + interval
}

func buildCandidateBar(sampleAt time.Time, tokenAddress string, interval string, marketCap float64) model.Kline {
	openTime := candidateBarOpenTime(sampleAt, interval)
	closeAt := candidateBarCloseTime(openTime, interval)
	return model.Kline{
		TokenAddress:   tokenAddress,
		Interval:       interval,
		OpenTime:       openTime,
		CloseTime:      closeAt,
		Open:           marketCap,
		High:           marketCap,
		Low:            marketCap,
		Close:          marketCap,
		MarketCapOpen:  marketCap,
		MarketCapHigh:  marketCap,
		MarketCapLow:   marketCap,
		MarketCapClose: marketCap,
		Volume:         1,
	}
}

func candidateBarOpenTime(sampleAt time.Time, interval string) time.Time {
	if sampleAt.IsZero() {
		sampleAt = time.Now()
	}
	local := apptime.InBeijing(sampleAt)
	switch interval {
	case "30s":
		return local.Truncate(30 * time.Second)
	case "1m":
		return local.Truncate(time.Minute)
	case "3m":
		return local.Truncate(3 * time.Minute)
	case "5m":
		return local.Truncate(5 * time.Minute)
	case "15m":
		return local.Truncate(15 * time.Minute)
	case "30m":
		return local.Truncate(30 * time.Minute)
	case "1h":
		return local.Truncate(time.Hour)
	case "2h":
		return local.Truncate(2 * time.Hour)
	case "4h":
		return local.Truncate(4 * time.Hour)
	default:
		return local.Truncate(time.Minute)
	}
}

func candidateBarCloseTime(openTime time.Time, interval string) time.Time {
	switch interval {
	case "30s":
		return openTime.Add(30 * time.Second)
	case "1m":
		return openTime.Add(time.Minute)
	case "3m":
		return openTime.Add(3 * time.Minute)
	case "5m":
		return openTime.Add(5 * time.Minute)
	case "15m":
		return openTime.Add(15 * time.Minute)
	case "30m":
		return openTime.Add(30 * time.Minute)
	case "1h":
		return openTime.Add(time.Hour)
	case "2h":
		return openTime.Add(2 * time.Hour)
	case "4h":
		return openTime.Add(4 * time.Hour)
	case "1d":
		return openTime.AddDate(0, 0, 1)
	case "1w":
		return openTime.AddDate(0, 0, 7)
	default:
		return openTime.Add(time.Minute)
	}
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
