package signal

import (
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/model"
)

func TestCandidateKlineCacheApplyPriceSampleKeepsGMGNVolumeAndSyntheticBarZeroVolume(t *testing.T) {
	cache := newCandidateKlineCache(10)
	base := time.Date(2026, 6, 29, 10, 0, 10, 0, time.UTC)

	series, bar := cache.ApplyPriceSample("token-a", "1m", base, 100)
	if len(series) != 1 || bar.Volume != 0 {
		t.Fatalf("expected synthetic first sample to keep volume=0, got series=%#v bar=%#v", series, bar)
	}

	series, bar = cache.ApplyPriceSample("token-a", "1m", base.Add(20*time.Second), 120)
	if len(series) != 1 || bar.Volume != 0 {
		t.Fatalf("expected same-minute synthetic sample to keep volume=0, got series=%#v bar=%#v", series, bar)
	}

	series, bar = cache.ApplyPriceSample("token-a", "1m", base.Add(70*time.Second), 90)
	if len(series) != 2 || bar.Volume != 0 {
		t.Fatalf("expected next minute synthetic bar to keep volume=0, got series=%#v bar=%#v", series, bar)
	}
}

func TestCandidateKlineCacheApplyPriceSamplePreservesExistingGMGNVolume(t *testing.T) {
	cache := newCandidateKlineCache(10)
	base := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	cache.Set("token-a", "1m", []model.Kline{{
		TokenAddress:   "token-a",
		Interval:       "1m",
		OpenTime:       base,
		CloseTime:      base.Add(time.Minute),
		Open:           100,
		High:           120,
		Low:            95,
		Close:          110,
		MarketCapOpen:  100,
		MarketCapHigh:  120,
		MarketCapLow:   95,
		MarketCapClose: 110,
		Volume:         1607.03,
	}})

	series, bar := cache.ApplyPriceSample("token-a", "1m", base.Add(20*time.Second), 130)
	if len(series) != 1 {
		t.Fatalf("expected one merged bar, got %#v", series)
	}
	if bar.Volume != 1607.03 {
		t.Fatalf("expected GMGN volume to remain unchanged, got %#v", bar)
	}
}
