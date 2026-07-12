package signal

import (
	"context"
	"log"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/integration/project"
)

type CompletedProjectFeed interface {
	FetchCompletedProjects(ctx context.Context) ([]project.Item, error)
}

type alternatingFeed struct {
	name string
	feed CompletedProjectFeed
}

type ProjectCandidatePoller struct {
	feeds    []alternatingFeed
	monitor  *CandidateMonitor
	interval time.Duration
}

func NewProjectCandidatePoller(xxyyFeed, gmgnFeed CompletedProjectFeed, monitor *CandidateMonitor, interval time.Duration) *ProjectCandidatePoller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ProjectCandidatePoller{
		feeds:    []alternatingFeed{{name: "XXYY", feed: xxyyFeed}, {name: "GMGN", feed: gmgnFeed}},
		monitor:  monitor,
		interval: interval,
	}
}

func (p *ProjectCandidatePoller) Start(ctx context.Context) {
	if p == nil || len(p.feeds) == 0 || p.monitor == nil {
		return
	}
	for _, item := range p.feeds {
		if item.feed == nil {
			return
		}
	}
	go func() {
		index := 0
		p.pollOnce(ctx, p.feeds[index])
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				index = (index + 1) % len(p.feeds)
				p.pollOnce(ctx, p.feeds[index])
			}
		}
	}()
}

func (p *ProjectCandidatePoller) pollOnce(ctx context.Context, source alternatingFeed) {
	items, err := source.feed.FetchCompletedProjects(ctx)
	if err != nil {
		log.Printf("%s completed candidate poll failed: %v", source.name, err)
		return
	}
	qualified, accepted, failed := 0, 0, 0
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		address := strings.TrimSpace(item.TokenAddress)
		if item.KOL < 3 || address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		qualified++
		if _, err := p.monitor.AddManualCandidate(ctx, address); err != nil {
			failed++
			log.Printf("%s completed candidate add failed: ca=%s kol=%v err=%v", source.name, address, item.KOL, err)
			continue
		}
		accepted++
	}
	log.Printf("%s completed candidate poll completed: fetched=%d qualified=%d accepted=%d failed=%d", source.name, len(items), qualified, accepted, failed)
}
