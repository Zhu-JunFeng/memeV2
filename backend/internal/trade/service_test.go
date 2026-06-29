package trade

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	solana "github.com/gagliardetto/solana-go"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/model"
)

type fakeRepo struct {
	account           model.TradeAccount
	tradeMode         model.TradeMode
	summaries         []model.TradeSummaryItem
	signals           []model.TradeSignal
	orders            []model.TradeOrder
	positions         map[string]model.TradePosition
	updatedSignal     map[string]string
	positionByID      map[string]model.TradePosition
	nextOrderID       int
	lastBuyFill       *model.TradeFill
	lastSellFill      *model.TradeFill
	manualSignalSeen  bool
	setTradeModeCalls int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		account:       model.TradeAccount{ID: "acc-1", Name: "default", BuyAmountUSD: 10, SlippageBPS: 500},
		positions:     map[string]model.TradePosition{},
		updatedSignal: map[string]string{},
		positionByID:  map[string]model.TradePosition{},
		nextOrderID:   1,
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
func (r *fakeRepo) GetTradeMode(context.Context) (model.TradeMode, error) {
	return r.tradeMode, nil
}
func (r *fakeRepo) SetTradeMode(_ context.Context, mode model.TradeMode) error {
	r.tradeMode = mode
	r.setTradeModeCalls++
	return nil
}
func (r *fakeRepo) InsertSignalIfAbsent(_ context.Context, signal model.TradeSignal) (model.TradeSignal, bool, error) {
	for _, item := range r.signals {
		if item.SignalID == signal.SignalID {
			return item, false, nil
		}
	}
	r.signals = append(r.signals, signal)
	if signal.StrategyCode == "manual_close" {
		r.manualSignalSeen = true
	}
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
func (r *fakeRepo) ListTradeSummaries(context.Context) ([]model.TradeSummaryItem, error) {
	return append([]model.TradeSummaryItem(nil), r.summaries...), nil
}
func (r *fakeRepo) ListSignals(context.Context, model.TradeMode, int) ([]model.TradeSignal, error) {
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
	order.ID = "order-" + string(rune('0'+r.nextOrderID))
	r.nextOrderID++
	r.orders = append(r.orders, order)
	return order, nil
}
func (r *fakeRepo) UpdateOrderExecution(context.Context, string, model.TradeOrderStatus, string, json.RawMessage, json.RawMessage, string, *time.Time) error {
	return nil
}
func (r *fakeRepo) AddOrderEvent(context.Context, string, string, any) error { return nil }
func (r *fakeRepo) SaveFilledBuy(_ context.Context, order model.TradeOrder, fill model.TradeFill) error {
	storedFill := fill
	r.lastBuyFill = &storedFill
	position := model.TradePosition{
		ID:           "pos-1",
		AccountID:    r.account.ID,
		TradeMode:    order.TradeMode,
		TokenAddress: order.TokenAddress,
		Status:       model.TradePositionStatusOpen,
		Quantity:     fill.FilledTokenAmount,
		CostAmount:   fill.FilledQuoteAmount + fill.FeeAmount,
		AvgCostPrice: fill.AvgPrice,
		LastPrice:    fill.AvgPrice,
		MarketValue:  fill.FilledQuoteAmount,
	}
	r.positions[r.account.ID+":"+order.TokenAddress] = position
	r.positionByID[position.ID] = position
	return nil
}
func (r *fakeRepo) SaveFilledSell(_ context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error {
	storedFill := fill
	r.lastSellFill = &storedFill
	delete(r.positions, position.AccountID+":"+position.TokenAddress)
	delete(r.positionByID, position.ID)
	return nil
}
func (r *fakeRepo) ListOrders(context.Context, model.TradeMode, int) ([]model.TradeOrder, error) {
	return r.orders, nil
}
func (r *fakeRepo) GetOrder(_ context.Context, id string) (model.TradeOrder, error) {
	for _, item := range r.orders {
		if item.ID == id {
			return item, nil
		}
	}
	return model.TradeOrder{}, errors.New("not implemented")
}
func (r *fakeRepo) ListPositions(context.Context, string, model.TradeMode, int) ([]model.TradePosition, error) {
	items := make([]model.TradePosition, 0, len(r.positions))
	for _, item := range r.positions {
		items = append(items, item)
	}
	return items, nil
}
func (r *fakeRepo) GetPosition(_ context.Context, id string) (model.TradePosition, error) {
	item, ok := r.positionByID[id]
	if !ok {
		return model.TradePosition{}, errors.New("not implemented")
	}
	return item, nil
}
func (r *fakeRepo) UpdatePositionMark(context.Context, string, float64, float64, float64, float64, float64) error {
	return nil
}

type fakeExecutor struct {
	lastRequest ExecutionRequest
}

func (f *fakeExecutor) Execute(_ context.Context, req ExecutionRequest) (ExecutionResult, error) {
	f.lastRequest = req
	result := ExecutionResult{
		TxHash:           "tx-1",
		FilledToken:      100,
		FilledQuote:      10,
		AvgPrice:         0.1,
		FeeAmount:        0.15,
		FeeAsset:         "USD",
		ExecutedAt:       time.Now().UTC(),
		ExecutionChannel: string(model.TradeExecutionChannelJupiterLive),
	}
	if req.Mode == model.TradeModePaper {
		result.TxHash = "paper_tx-1"
		result.Simulated = true
		result.ExecutionChannel = string(model.TradeExecutionChannelJupiterPaper)
	}
	return result, nil
}

func testTradeConfig(t *testing.T) config.TradeConfig {
	t.Helper()
	privateKey, err := solana.NewRandomPrivateKey()
	if err != nil {
		t.Fatalf("new random private key: %v", err)
	}
	return config.TradeConfig{
		Enabled:          true,
		AccountName:      "default",
		BuyAmountUSD:     10,
		SlippageBPS:      500,
		WalletPrivateKey: privateKey.String(),
	}
}

func TestNewServiceDefaultsTradeModeToPaper(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if svc.GetTradeMode() != model.TradeModePaper {
		t.Fatalf("expected default paper mode, got %s", svc.GetTradeMode())
	}
	if repo.tradeMode != model.TradeModePaper || repo.setTradeModeCalls != 1 {
		t.Fatalf("expected repo trade mode to be persisted once, got mode=%s calls=%d", repo.tradeMode, repo.setTradeModeCalls)
	}
}

func TestUpdateTradeModePersistsState(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	mode, err := svc.UpdateTradeMode(context.Background(), model.TradeModeLive)
	if err != nil {
		t.Fatalf("update trade mode: %v", err)
	}
	if mode != model.TradeModeLive || svc.GetTradeMode() != model.TradeModeLive {
		t.Fatalf("expected live mode, got return=%s current=%s", mode, svc.GetTradeMode())
	}
	if repo.tradeMode != model.TradeModeLive {
		t.Fatalf("expected repo to persist live mode, got %s", repo.tradeMode)
	}
}

func TestProcessSignalCreatesSinglePosition(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
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
	if repo.orders[0].TradeMode != model.TradeModePaper {
		t.Fatalf("expected paper order, got %s", repo.orders[0].TradeMode)
	}
	if repo.lastBuyFill == nil || !repo.lastBuyFill.IsSimulated {
		t.Fatalf("expected paper fill to be simulated")
	}
	position, ok := repo.positions[repo.account.ID+":token-a"]
	if !ok {
		t.Fatalf("expected open position after buy")
	}
	if position.CostAmount != 10.15 {
		t.Fatalf("expected buy fee to be included in cost amount, got %f", position.CostAmount)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-2", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", Interval: "1m", SignalTime: signalTime.Add(time.Minute), TriggerMarketCap: 124, Reason: "buy again"}); err != nil {
		t.Fatalf("second process signal: %v", err)
	}
	if len(repo.orders) != 1 {
		t.Fatalf("expected duplicate open-position buy to be skipped, orders=%d", len(repo.orders))
	}
}

func TestRetryBuyOrderRespectsExistingPosition(t *testing.T) {
	repo := newFakeRepo()
	repo.positions[repo.account.ID+":token-a"] = model.TradePosition{ID: "pos-1", AccountID: repo.account.ID, TokenAddress: "token-a", Status: model.TradePositionStatusOpen}
	repo.orders = append(repo.orders, model.TradeOrder{ID: "order-1", AccountID: repo.account.ID, SignalID: "signal-db-1", TokenAddress: "token-a", Side: model.TradeSignalTypeBuy, TradeMode: model.TradeModePaper})
	repo.signals = append(repo.signals, model.TradeSignal{ID: "signal-db-1", SignalID: "sig-1", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", TradeMode: model.TradeModePaper})
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.RetryOrder(context.Background(), "order-1"); err != nil {
		t.Fatalf("retry order: %v", err)
	}
	if len(repo.orders) != 1 {
		t.Fatalf("expected retry to skip creating a second buy order when position exists, got %d", len(repo.orders))
	}
}

func TestClosePositionPersistsManualSignal(t *testing.T) {
	repo := newFakeRepo()
	position := model.TradePosition{ID: "pos-1", AccountID: repo.account.ID, TokenAddress: "token-a", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10.15, LastPrice: 0.11}
	repo.positions[repo.account.ID+":token-a"] = position
	repo.positionByID[position.ID] = position
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ClosePosition(context.Background(), "pos-1"); err != nil {
		t.Fatalf("close position: %v", err)
	}
	if !repo.manualSignalSeen {
		t.Fatalf("expected manual close to persist a trade signal before creating sell order")
	}
	if len(repo.orders) != 1 || repo.orders[0].SignalID == "" {
		t.Fatalf("expected sell order to be linked to persisted signal, orders=%#v", repo.orders)
	}
}

func TestProcessSignalUsesCurrentTradeMode(t *testing.T) {
	repo := newFakeRepo()
	repo.tradeMode = model.TradeModeLive
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-live", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-live", Interval: "1m", SignalTime: time.Now().UTC(), Reason: "buy"})
	if err != nil {
		t.Fatalf("process signal: %v", err)
	}
	if executor.lastRequest.Mode != model.TradeModeLive {
		t.Fatalf("expected executor to receive live mode, got %s", executor.lastRequest.Mode)
	}
	if repo.orders[0].ExecutionChannel != string(model.TradeExecutionChannelJupiterLive) {
		t.Fatalf("expected live execution channel, got %s", repo.orders[0].ExecutionChannel)
	}
}

func TestDisabledServiceRejectsSignal(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), config.TradeConfig{}, repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{SignalID: "sig-1", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-a", Interval: "1m", SignalTime: time.Now().UTC(), Reason: "buy"})
	if !errors.Is(err, ErrTradeDisabled) {
		t.Fatalf("expected ErrTradeDisabled, got %v", err)
	}
}

func TestListTradeSummaries(t *testing.T) {
	repo := newFakeRepo()
	repo.summaries = []model.TradeSummaryItem{
		{TradeMode: "", TotalPNL: 12.5, TradeCount: 3, WinRate: 2.0 / 3.0},
		{TradeMode: model.TradeModePaper, TotalPNL: 4.2, TradeCount: 2, WinRate: 0.5},
		{TradeMode: model.TradeModeLive, TotalPNL: 8.3, TradeCount: 1, WinRate: 1},
	}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	items, err := svc.ListTradeSummaries(context.Background())
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(items))
	}
	if items[0].TotalPNL != 12.5 || items[2].TradeMode != model.TradeModeLive {
		t.Fatalf("unexpected summaries: %#v", items)
	}
}
