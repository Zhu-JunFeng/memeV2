package trade

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/model"
)

type fakeRepo struct {
	account       model.TradeAccount
	signals       []model.TradeSignal
	orders        []model.TradeOrder
	positions     map[string]model.TradePosition
	updatedSignal map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		account:       model.TradeAccount{ID: "acc-1", Name: "default", BuyAmountUSD: 10, SlippageBPS: 500},
		positions:     map[string]model.TradePosition{},
		updatedSignal: map[string]string{},
	}
}

func (r *fakeRepo) EnsureAccount(context.Context, model.TradeAccount) (model.TradeAccount, error) {
	return r.account, nil
}
func (r *fakeRepo) GetAccountByName(context.Context, string) (model.TradeAccount, error) {
	return r.account, nil
}
func (r *fakeRepo) ListAccounts(context.Context) ([]model.TradeAccount, error) {
	return []model.TradeAccount{r.account}, nil
}
func (r *fakeRepo) InsertSignalIfAbsent(_ context.Context, signal model.TradeSignal) (model.TradeSignal, bool, error) {
	r.signals = append(r.signals, signal)
	return signal, true, nil
}
func (r *fakeRepo) UpdateSignalStatus(_ context.Context, signalID string, status string) error {
	r.updatedSignal[signalID] = status
	return nil
}
func (r *fakeRepo) GetSignalByID(_ context.Context, id string) (model.TradeSignal, error) {
	for _, item := range r.signals {
		if item.ID == id {
			return item, nil
		}
	}
	return model.TradeSignal{}, sql.ErrNoRows
}
func (r *fakeRepo) ListSignals(context.Context, int) ([]model.TradeSignal, error) {
	return r.signals, nil
}
func (r *fakeRepo) GetOpenPosition(_ context.Context, accountID string, tokenAddress string) (model.TradePosition, error) {
	item, ok := r.positions[accountID+":"+tokenAddress]
	if !ok {
		return model.TradePosition{}, sql.ErrNoRows
	}
	return item, nil
}
func (r *fakeRepo) CreateOrder(_ context.Context, order model.TradeOrder) (model.TradeOrder, error) {
	order.ID = "order-1"
	r.orders = append(r.orders, order)
	return order, nil
}
func (r *fakeRepo) UpdateOrderExecution(context.Context, string, model.TradeOrderStatus, string, json.RawMessage, json.RawMessage, string, *time.Time) error {
	return nil
}
func (r *fakeRepo) AddOrderEvent(context.Context, string, string, any) error { return nil }
func (r *fakeRepo) SaveFilledBuy(_ context.Context, order model.TradeOrder, fill model.TradeFill) error {
	r.positions[r.account.ID+":"+order.TokenAddress] = model.TradePosition{ID: "pos-1", AccountID: r.account.ID, TokenAddress: order.TokenAddress, Status: model.TradePositionStatusOpen, Quantity: fill.FilledTokenAmount, CostAmount: fill.FilledQuoteAmount, AvgCostPrice: fill.AvgPrice}
	return nil
}
func (r *fakeRepo) SaveFilledSell(_ context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error {
	delete(r.positions, position.AccountID+":"+position.TokenAddress)
	return nil
}
func (r *fakeRepo) ListOrders(context.Context, int) ([]model.TradeOrder, error) { return r.orders, nil }
func (r *fakeRepo) GetOrder(context.Context, string) (model.TradeOrder, error) {
	return model.TradeOrder{}, errors.New("not implemented")
}
func (r *fakeRepo) ListPositions(context.Context, string, int) ([]model.TradePosition, error) {
	items := make([]model.TradePosition, 0, len(r.positions))
	for _, item := range r.positions {
		items = append(items, item)
	}
	return items, nil
}
func (r *fakeRepo) GetPosition(context.Context, string) (model.TradePosition, error) {
	return model.TradePosition{}, errors.New("not implemented")
}
func (r *fakeRepo) UpdatePositionMark(context.Context, string, float64, float64, float64, float64, float64) error {
	return nil
}

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, ExecutionRequest) (ExecutionResult, error) {
	return ExecutionResult{TxHash: "tx-1", FilledToken: 100, FilledQuote: 10, AvgPrice: 0.1, FeeAmount: 0.15, FeeAsset: "USD", ExecutedAt: time.Now().UTC()}, nil
}

func TestProcessSignalCreatesSinglePosition(t *testing.T) {
	repo := newFakeRepo()
	svc, err := NewService(context.Background(), config.TradeConfig{Enabled: true, AccountName: "default", BuyAmountUSD: 10, SlippageBPS: 500}, repo, fakeExecutor{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	signalTime := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-1", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", Interval: "1m", SignalTime: signalTime, TriggerMarketCap: 123, Reason: "buy"}); err != nil {
		t.Fatalf("process signal: %v", err)
	}
	if len(repo.orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(repo.orders))
	}
	if _, ok := repo.positions[repo.account.ID+":token-a"]; !ok {
		t.Fatalf("expected open position after buy")
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-2", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", Interval: "1m", SignalTime: signalTime.Add(time.Minute), TriggerMarketCap: 124, Reason: "buy again"}); err != nil {
		t.Fatalf("second process signal: %v", err)
	}
	if len(repo.orders) != 1 {
		t.Fatalf("expected duplicate open-position buy to be skipped, orders=%d", len(repo.orders))
	}
}

func TestDisabledServiceRejectsSignal(t *testing.T) {
	repo := newFakeRepo()
	svc, err := NewService(context.Background(), config.TradeConfig{}, repo, fakeExecutor{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-1", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", Interval: "1m", SignalTime: time.Now().UTC(), Reason: "buy"})
	if !errors.Is(err, ErrTradeDisabled) {
		t.Fatalf("expected ErrTradeDisabled, got %v", err)
	}
}
