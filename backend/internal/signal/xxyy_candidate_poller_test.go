package signal

import (
	"context"
	"testing"

	"solana-meme-backtest/backend/internal/integration/xxyy"
)

type fakeXXYYFeed struct {
	items []xxyy.FeedItem
}

func (f fakeXXYYFeed) FetchNewProjects(context.Context) ([]xxyy.FeedItem, error) {
	return f.items, nil
}

func TestXXYYCandidatePollerOnlyAddsUniqueKOLAtLeastThree(t *testing.T) {
	store := &fakeCandidateStore{states: map[string]candidateMonitorState{}}
	monitor := &CandidateMonitor{cfg: CandidateMonitorConfig{Enabled: true}, store: store}
	poller := NewXXYYCandidatePoller(fakeXXYYFeed{items: []xxyy.FeedItem{
		{TokenAddress: "ca-six", KOL: 6},
		{TokenAddress: "ca-three", KOL: 3},
		{TokenAddress: "ca-low", KOL: 2},
		{TokenAddress: "ca-six", KOL: 8},
		{TokenAddress: "", KOL: 9},
	}}, monitor, 0)

	poller.pollOnce(context.Background())

	if len(store.states) != 2 {
		t.Fatalf("expected two candidates, got %#v", store.states)
	}
	if _, ok := store.states["ca-six"]; !ok {
		t.Fatalf("expected ca-six, got %#v", store.states)
	}
	if _, ok := store.states["ca-three"]; !ok {
		t.Fatalf("expected ca-three, got %#v", store.states)
	}
}
