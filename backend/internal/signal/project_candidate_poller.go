package signal

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/integration/project"
)

type CompletedProjectFeed interface {
	FetchCompletedProjects(ctx context.Context) ([]project.Item, error)
}

type TokenSymbolFeed interface {
	FetchTokenSymbol(ctx context.Context, tokenAddress string) (string, error)
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
	sort.SliceStable(items, func(i, j int) bool {
		leftPreferred := items[i].MarketCap >= preferredMarketCapMin && items[i].MarketCap <= preferredMarketCapMax
		rightPreferred := items[j].MarketCap >= preferredMarketCapMin && items[j].MarketCap <= preferredMarketCapMax
		return leftPreferred && !rightPreferred
	})
	qualified, accepted, skipped, failed := 0, 0, 0, 0
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		address := strings.TrimSpace(item.TokenAddress)
		if item.KOL < 3 || address == "" || (item.MarketCap > 0 && item.MarketCap < monitorMinMarketCap) {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		qualified++
		_, added, err := p.monitor.AddProjectCandidate(ctx, address, item.Symbol, item.MarketCap)
		if errors.Is(err, ErrCandidatePoolFull) {
			skipped++
			continue
		}
		if err != nil {
			failed++
			log.Printf("%s completed candidate add failed: ca=%s kol=%v err=%v", source.name, address, item.KOL, err)
			continue
		}
		if added {
			accepted++
		}
	}
	if source.name == "GMGN" {
		if err := p.monitor.TrimCandidatePool(ctx); err != nil {
			log.Printf("candidate pool trim failed: %v", err)
		}
		p.enrichMissingSymbols(ctx, source.feed)
	}
	log.Printf("%s completed candidate poll completed: fetched=%d qualified=%d accepted=%d skipped=%d failed=%d", source.name, len(items), qualified, accepted, skipped, failed)
}

func (p *ProjectCandidatePoller) enrichMissingSymbols(ctx context.Context, feed CompletedProjectFeed) {
	symbolFeed, ok := feed.(TokenSymbolFeed)
	if !ok {
		return
	}
	addresses, err := p.monitor.MissingSymbolCandidates(ctx)
	if err != nil {
		log.Printf("candidate symbol list failed: %v", err)
		return
	}
	for _, address := range addresses {
		symbol, err := symbolFeed.FetchTokenSymbol(ctx, address)
		if err != nil {
			log.Printf("candidate symbol fetch failed: ca=%s err=%v", address, err)
			continue
		}
		if err := p.monitor.UpdateCandidateSymbol(ctx, address, symbol); err != nil {
			log.Printf("candidate symbol update failed: ca=%s err=%v", address, err)
		}
	}
}
