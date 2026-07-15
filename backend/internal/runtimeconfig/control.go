package runtimeconfig

import (
	"context"
	"sync"
	"sync/atomic"
)

type State struct {
	CAMonitoringEnabled   bool `json:"caMonitoringEnabled"`
	TradeExecutionEnabled bool `json:"tradeExecutionEnabled"`
}

type Store interface {
	LoadRuntimeSwitches(ctx context.Context) (caMonitoring *bool, tradeExecution *bool, err error)
	SaveRuntimeSwitches(ctx context.Context, state State) error
}

type Control struct {
	store          Store
	mu             sync.Mutex
	caMonitoring   atomic.Bool
	tradeExecution atomic.Bool
}

func New(ctx context.Context, store Store, defaults State) (*Control, error) {
	control := &Control{store: store}
	state := defaults
	if store != nil {
		caMonitoring, tradeExecution, err := store.LoadRuntimeSwitches(ctx)
		if err != nil {
			return nil, err
		}
		if caMonitoring != nil {
			state.CAMonitoringEnabled = *caMonitoring
		}
		if tradeExecution != nil {
			state.TradeExecutionEnabled = *tradeExecution
		}
		if caMonitoring == nil || tradeExecution == nil {
			if err := store.SaveRuntimeSwitches(ctx, state); err != nil {
				return nil, err
			}
		}
	}
	control.caMonitoring.Store(state.CAMonitoringEnabled)
	control.tradeExecution.Store(state.TradeExecutionEnabled)
	return control, nil
}

func (c *Control) State() State {
	if c == nil {
		return State{}
	}
	return State{
		CAMonitoringEnabled:   c.caMonitoring.Load(),
		TradeExecutionEnabled: c.tradeExecution.Load(),
	}
}

func (c *Control) CAMonitoringEnabled() bool {
	return c != nil && c.caMonitoring.Load()
}

func (c *Control) TradeExecutionEnabled() bool {
	return c != nil && c.tradeExecution.Load()
}

func (c *Control) Update(ctx context.Context, caMonitoring *bool, tradeExecution *bool) (State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.State()
	if caMonitoring == nil && tradeExecution == nil {
		return state, nil
	}
	if caMonitoring != nil {
		state.CAMonitoringEnabled = *caMonitoring
	}
	if tradeExecution != nil {
		state.TradeExecutionEnabled = *tradeExecution
	}
	if c.store != nil {
		if err := c.store.SaveRuntimeSwitches(ctx, state); err != nil {
			return c.State(), err
		}
	}
	c.caMonitoring.Store(state.CAMonitoringEnabled)
	c.tradeExecution.Store(state.TradeExecutionEnabled)
	return state, nil
}
