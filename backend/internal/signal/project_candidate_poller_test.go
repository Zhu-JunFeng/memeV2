package signal

import (
	"context"
	"testing"

	"solana-meme-backtest/backend/internal/integration/project"
)

type fakeCompletedProjectFeed struct {
	items []project.Item
}

func (f fakeCompletedProjectFeed) FetchCompletedProjects(context.Context) ([]project.Item, error) {
	return f.items, nil
}

func TestProjectCandidatePollerOnlyAddsUniqueKOLAtLeastThree(t *testing.T) {
	store := &fakeCandidateStore{states: map[string]candidateMonitorState{}}
	monitor := &CandidateMonitor{cfg: CandidateMonitorConfig{Enabled: true}, store: store}
	feed := fakeCompletedProjectFeed{items: []project.Item{
		{TokenAddress: "ca-six", KOL: 6},
		{TokenAddress: "ca-three", KOL: 3},
		{TokenAddress: "ca-low", KOL: 2},
		{TokenAddress: "ca-six", KOL: 8},
		{TokenAddress: "", KOL: 9},
	}}
	poller := NewProjectCandidatePoller(feed, feed, monitor, 0)

	poller.pollOnce(context.Background(), poller.feeds[0])

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

func TestProjectCandidatePollerKeepsSourceOrder(t *testing.T) {
	poller := NewProjectCandidatePoller(fakeCompletedProjectFeed{}, fakeCompletedProjectFeed{}, &CandidateMonitor{}, 0)
	if len(poller.feeds) != 2 || poller.feeds[0].name != "XXYY" || poller.feeds[1].name != "GMGN" {
		t.Fatalf("unexpected source order: %#v", poller.feeds)
	}
}
