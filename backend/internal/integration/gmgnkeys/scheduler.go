package gmgnkeys

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNoAvailableKey = errors.New("GMGN 可用 API Key 不存在")
var ErrAllKeysCoolingDown = errors.New("GMGN API Key 均处于限流冷却中")
var ErrKeyCoolingDown = errors.New("GMGN API Key 处于限流冷却中")

type Store interface {
	ListAvailableGMGNKeys(ctx context.Context) ([]string, error)
	MarkGMGNKeySuccessful(ctx context.Context, apiKey string) error
}

type Scheduler struct {
	store    Store
	fallback []string
	cursor   uint32

	mu       sync.Mutex
	limiter  *requestLimiter
	cooldown map[string]time.Time
}

type requestLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func NewScheduler(store Store, fallback []string, maxQPS float64) *Scheduler {
	return &Scheduler{
		store:    store,
		fallback: normalizeKeys(fallback),
		limiter:  newRequestLimiter(maxQPS),
		cooldown: make(map[string]time.Time),
	}
}

func newRequestLimiter(maxQPS float64) *requestLimiter {
	if maxQPS <= 0 {
		return nil
	}
	return &requestLimiter{interval: time.Duration(float64(time.Second) / maxQPS)}
}

func (s *Scheduler) AvailableKeys(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, ErrNoAvailableKey
	}
	var keys []string
	if s.store == nil {
		if len(s.fallback) == 0 {
			return nil, ErrNoAvailableKey
		}
		keys = append([]string(nil), s.fallback...)
	} else {
		var err error
		keys, err = s.store.ListAvailableGMGNKeys(ctx)
		if err != nil {
			return nil, err
		}
		keys = normalizeKeys(keys)
		if len(keys) == 0 {
			return nil, ErrNoAvailableKey
		}
	}
	available := s.withoutCoolingDown(keys, time.Now())
	if len(available) == 0 {
		return nil, ErrAllKeysCoolingDown
	}
	return available, nil
}

func (s *Scheduler) NextKey(keys []string) string {
	if s == nil || len(keys) == 0 {
		return ""
	}
	index := int(atomic.AddUint32(&s.cursor, 1)-1) % len(keys)
	return keys[index]
}

func (s *Scheduler) Wait(ctx context.Context, apiKey string) error {
	if s.isCoolingDown(apiKey, time.Now()) {
		return ErrKeyCoolingDown
	}
	if s == nil || s.limiter == nil || strings.TrimSpace(apiKey) == "" {
		return nil
	}
	limiter := s.limiter
	limiter.mu.Lock()
	now := time.Now()
	wait := time.Duration(0)
	if now.Before(limiter.next) {
		wait = limiter.next.Sub(now)
		limiter.next = limiter.next.Add(limiter.interval)
	} else {
		limiter.next = now.Add(limiter.interval)
	}
	limiter.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		if s.isCoolingDown(apiKey, time.Now()) {
			return ErrKeyCoolingDown
		}
		return nil
	}
}

func (s *Scheduler) isCoolingDown(apiKey string, now time.Time) bool {
	if s == nil || strings.TrimSpace(apiKey) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until := s.cooldown[apiKey]
	return !until.IsZero() && now.Before(until)
}

func (s *Scheduler) MarkSuccessful(ctx context.Context, apiKey string) {
	if s == nil || s.store == nil || strings.TrimSpace(apiKey) == "" {
		return
	}
	_ = s.store.MarkGMGNKeySuccessful(ctx, apiKey)
}

func (s *Scheduler) MarkRateLimited(apiKey string, duration time.Duration) {
	if s == nil || strings.TrimSpace(apiKey) == "" || duration <= 0 {
		return
	}
	s.mu.Lock()
	until := time.Now().Add(duration)
	if until.After(s.cooldown[apiKey]) {
		s.cooldown[apiKey] = until
	}
	s.mu.Unlock()
}

func (s *Scheduler) withoutCoolingDown(keys []string, now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		until := s.cooldown[key]
		if until.IsZero() || !now.Before(until) {
			delete(s.cooldown, key)
			result = append(result, key)
		}
	}
	return result
}

func normalizeKeys(keys []string) []string {
	result := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, item := range keys {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}
