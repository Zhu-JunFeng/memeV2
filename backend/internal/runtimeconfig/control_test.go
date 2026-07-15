package runtimeconfig

import (
	"context"
	"testing"
)

type fakeStore struct {
	caMonitoring   *bool
	tradeExecution *bool
	saved          State
	saveCalls      int
}

func (s *fakeStore) LoadRuntimeSwitches(context.Context) (*bool, *bool, error) {
	return s.caMonitoring, s.tradeExecution, nil
}

func (s *fakeStore) SaveRuntimeSwitches(_ context.Context, state State) error {
	s.saved = state
	s.saveCalls++
	return nil
}

func TestControlUsesDefaultsAndPersistsMissingSettings(t *testing.T) {
	store := &fakeStore{}
	control, err := New(context.Background(), store, State{CAMonitoringEnabled: true, TradeExecutionEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if state := control.State(); !state.CAMonitoringEnabled || state.TradeExecutionEnabled {
		t.Fatalf("unexpected defaults: %+v", state)
	}
	if !store.saved.CAMonitoringEnabled || store.saved.TradeExecutionEnabled {
		t.Fatalf("defaults were not persisted: %+v", store.saved)
	}
}

func TestControlUpdatesBothSwitches(t *testing.T) {
	control, err := New(context.Background(), nil, State{})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	state, err := control.Update(context.Background(), &enabled, &enabled)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CAMonitoringEnabled || !state.TradeExecutionEnabled {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestControlDoesNotPersistWhenNoSwitchWasProvided(t *testing.T) {
	caMonitoring := true
	tradeExecution := false
	store := &fakeStore{caMonitoring: &caMonitoring, tradeExecution: &tradeExecution}
	control, err := New(context.Background(), store, State{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Update(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("expected no persistence for an empty switch update, got %d calls", store.saveCalls)
	}
}
