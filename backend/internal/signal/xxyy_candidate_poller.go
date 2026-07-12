package signal

import (
	"context"
	"log"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/integration/xxyy"
)

type XXYYFeed interface {
	FetchNewProjects(ctx context.Context) ([]xxyy.FeedItem, error)
}

type XXYYCandidatePoller struct {
	feed     XXYYFeed
	monitor  *CandidateMonitor
	interval time.Duration
}

func NewXXYYCandidatePoller(feed XXYYFeed, monitor *CandidateMonitor, interval time.Duration) *XXYYCandidatePoller {
	if interval <= 0 {
		interval = time.Minute
	}
	return &XXYYCandidatePoller{feed: feed, monitor: monitor, interval: interval}
}

func (p *XXYYCandidatePoller) Start(ctx context.Context) {
	if p == nil || p.feed == nil || p.monitor == nil {
		return
	}
	go func() {
		p.pollOnce(ctx)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.pollOnce(ctx)
			}
		}
	}()
}

func (p *XXYYCandidatePoller) pollOnce(ctx context.Context) {
	items, err := p.feed.FetchNewProjects(ctx)
	if err != nil {
		log.Printf("XXYY candidate poll failed: %v", err)
		return
	}
	qualified, accepted, failed := 0, 0, 0
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		address := strings.TrimSpace(item.TokenAddress)
		if item.KOL < 5 || address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		qualified++
		if _, err := p.monitor.AddManualCandidate(ctx, address); err != nil {
			failed++
			log.Printf("XXYY candidate add failed: ca=%s kol=%v err=%v", address, item.KOL, err)
			continue
		}
		accepted++
	}
	log.Printf("XXYY candidate poll completed: fetched=%d qualified=%d accepted=%d failed=%d", len(items), qualified, accepted, failed)
}
