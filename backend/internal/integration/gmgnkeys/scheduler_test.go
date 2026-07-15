package gmgnkeys

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerLimitsAllKeysGlobally(t *testing.T) {
	scheduler := NewScheduler(nil, []string{"key-a", "key-b"}, 5)
	ctx := context.Background()
	keys, err := scheduler.AvailableKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	if err := scheduler.Wait(ctx, scheduler.NextKey(keys)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Wait(ctx, scheduler.NextKey(keys)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed < 150*time.Millisecond {
		t.Fatalf("different keys must share the global QPS limit, elapsed=%s", elapsed)
	}
}

func TestSchedulerRoundRobinsAcrossSharedCallers(t *testing.T) {
	scheduler := NewScheduler(nil, []string{"key-a", "key-b"}, 0)
	keys, err := scheduler.AvailableKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first, second, third := scheduler.NextKey(keys), scheduler.NextKey(keys), scheduler.NextKey(keys); first != "key-a" || second != "key-b" || third != "key-a" {
		t.Fatalf("unexpected rotation: %s %s %s", first, second, third)
	}
}

func TestSchedulerTemporarilyExcludesRateLimitedKey(t *testing.T) {
	scheduler := NewScheduler(nil, []string{"key-a", "key-b"}, 0)
	scheduler.MarkRateLimited("key-a", time.Minute)
	keys, err := scheduler.AvailableKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "key-b" {
		t.Fatalf("expected only key-b during cooldown, got %#v", keys)
	}
	scheduler.MarkRateLimited("key-b", time.Minute)
	if _, err := scheduler.AvailableKeys(context.Background()); err != ErrAllKeysCoolingDown {
		t.Fatalf("expected ErrAllKeysCoolingDown, got %v", err)
	}
}
