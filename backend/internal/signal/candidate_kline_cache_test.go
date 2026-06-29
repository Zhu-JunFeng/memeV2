package signal

import (
	"testing"
	"time"
)

func TestCandidateKlineCacheApplyPriceSampleCountsSamplesAsVolume(t *testing.T) {
	cache := newCandidateKlineCache(10)
	base := time.Date(2026, 6, 29, 10, 0, 10, 0, time.UTC)

	series, bar := cache.ApplyPriceSample("token-a", "1m", base, 100)
	if len(series) != 1 || bar.Volume != 1 {
		t.Fatalf("expected first sample to create volume=1 bar, got series=%#v bar=%#v", series, bar)
	}

	series, bar = cache.ApplyPriceSample("token-a", "1m", base.Add(20*time.Second), 120)
	if len(series) != 1 || bar.Volume != 2 {
		t.Fatalf("expected second sample in same bar to increment volume, got series=%#v bar=%#v", series, bar)
	}

	series, bar = cache.ApplyPriceSample("token-a", "1m", base.Add(70*time.Second), 90)
	if len(series) != 2 || bar.Volume != 1 {
		t.Fatalf("expected next minute to start a new volume counter, got series=%#v bar=%#v", series, bar)
	}
}
