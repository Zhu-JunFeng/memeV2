package trade

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	solana "github.com/gagliardetto/solana-go"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/model"
)

type fakeRepo struct {
	account           model.TradeAccount
	tradeMode         model.TradeMode
	modeStartedAt     time.Time
	periodSummary     model.TradeModePeriodSummary
	summaries         []model.TradeSummaryItem
	dailyStats        []model.TradeDailyStatsItem
	dailyStatsMode    model.TradeMode
	dailyStatsStart   time.Time
	dailyStatsEnd     time.Time
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
func (r *fakeRepo) UpdateAccountBuyAmountUSD(_ context.Context, _ string, buyAmountUSD float64) (model.TradeAccount, error) {
	r.account.BuyAmountUSD = buyAmountUSD
	r.account.UpdatedAt = time.Now().UTC()
	return r.account, nil
}
func (r *fakeRepo) GetTradeModeState(context.Context) (model.TradeMode, time.Time, error) {
	return r.tradeMode, r.modeStartedAt, nil
}
func (r *fakeRepo) SetTradeModeState(_ context.Context, mode model.TradeMode, startedAt time.Time) error {
	r.tradeMode = mode
	r.modeStartedAt = startedAt
	r.setTradeModeCalls++
	return nil
}
func (r *fakeRepo) GetTradeModePeriodSummary(_ context.Context, _ string, mode model.TradeMode, startedAt time.Time, endedAt time.Time) (model.TradeModePeriodSummary, error) {
	summary := r.periodSummary
	summary.TradeMode = mode
	summary.StartedAt = startedAt
	summary.EndedAt = endedAt
	return summary, nil
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
func (r *fakeRepo) UpdateSignalStatusAndReason(_ context.Context, signalID string, status string, reason string) error {
	r.updatedSignal[signalID] = status
	for index := range r.signals {
		if r.signals[index].ID == signalID {
			r.signals[index].ConsumeStatus = status
			r.signals[index].Reason = reason
			return nil
		}
	}
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
func (r *fakeRepo) GetSignalBySignalID(_ context.Context, signalID string) (model.TradeSignal, error) {
	for _, item := range r.signals {
		if item.SignalID == signalID {
			return item, nil
		}
	}
	return model.TradeSignal{}, sql.ErrNoRows
}
func (r *fakeRepo) ListTradeSummaries(context.Context) ([]model.TradeSummaryItem, error) {
	return append([]model.TradeSummaryItem(nil), r.summaries...), nil
}
func (r *fakeRepo) ListDailyStats(_ context.Context, tradeMode model.TradeMode, startTime time.Time, endTime time.Time) ([]model.TradeDailyStatsItem, error) {
	r.dailyStatsMode = tradeMode
	r.dailyStatsStart = startTime
	r.dailyStatsEnd = endTime
	return append([]model.TradeDailyStatsItem(nil), r.dailyStats...), nil
}
func (r *fakeRepo) ListSignals(context.Context, model.TradeMode, int) ([]model.TradeSignal, error) {
	return r.signals, nil
}
func (r *fakeRepo) CreateOrder(_ context.Context, order model.TradeOrder) (model.TradeOrder, error) {
	if order.ID == "" {
		order.ID = "order-" + string(rune('0'+r.nextOrderID))
	}
	r.nextOrderID++
	r.orders = append(r.orders, order)
	return order, nil
}
func (r *fakeRepo) UpdateOrderExecution(context.Context, string, model.TradeOrderStatus, string, json.RawMessage, json.RawMessage, string, *time.Time) error {
	return nil
}
func (r *fakeRepo) AddOrderEvent(context.Context, string, string, any) error { return nil }
func (r *fakeRepo) SaveFilledBuy(_ context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error {
	storedFill := fill
	r.lastBuyFill = &storedFill
	position.AccountID = r.account.ID
	position.TradeMode = order.TradeMode
	position.TokenAddress = order.TokenAddress
	position.Status = model.TradePositionStatusOpen
	position.Quantity = fill.FilledTokenAmount
	position.CostAmount = fill.FilledQuoteAmount + fill.FeeAmount
	position.AvgCostPrice = fill.AvgPrice
	position.LastPrice = fill.AvgPrice
	position.MarketValue = fill.FilledQuoteAmount
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
	lastRequest  ExecutionRequest
	quoteResult  QuoteResult
	quoteResults []QuoteResult
	quoteCalls   int
	executeCalls int
}

func (f *fakeExecutor) Quote(_ context.Context, req ExecutionRequest) (QuoteResult, error) {
	f.lastRequest = req
	f.quoteCalls++
	if len(f.quoteResults) > 0 {
		index := f.quoteCalls - 1
		if index >= len(f.quoteResults) {
			index = len(f.quoteResults) - 1
		}
		return f.quoteResults[index], nil
	}
	return f.quoteResult, nil
}

func (f *fakeExecutor) Execute(_ context.Context, req ExecutionRequest) (ExecutionResult, error) {
	f.lastRequest = req
	f.executeCalls++
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

type fakeSupplyProvider struct {
	supply float64
}

type fakeNotifier struct {
	signals     []model.TradeSignal
	fills       []model.TradeFill
	modeChanges []model.TradeModeChange
}

func (n *fakeNotifier) NotifySignal(_ context.Context, signal model.TradeSignal) error {
	n.signals = append(n.signals, signal)
	return nil
}

func (n *fakeNotifier) NotifyTrade(_ context.Context, fill model.TradeFill) error {
	n.fills = append(n.fills, fill)
	return nil
}

func (n *fakeNotifier) NotifyTradeModeChange(_ context.Context, change model.TradeModeChange) error {
	n.modeChanges = append(n.modeChanges, change)
	return nil
}

func (p fakeSupplyProvider) GetTokenSupply(context.Context, string) (float64, error) {
	return p.supply, nil
}

type fakeWalletBalanceProvider struct {
	balance float64
	err     error
}

type fakeCARiskStore struct {
	states map[string]model.CABlacklistState
}

func (s *fakeCARiskStore) GetCABlacklistState(_ context.Context, tokenAddress string) (model.CABlacklistState, error) {
	return s.states[tokenAddress], nil
}

type fakePositionStore struct {
	positions      map[string]model.TradePosition
	deleteCalls    int
	deleteFailures int
}

func newFakePositionStore() *fakePositionStore {
	return &fakePositionStore{positions: map[string]model.TradePosition{}}
}

func (s *fakePositionStore) Save(_ context.Context, position model.TradePosition) error {
	s.positions[position.AccountID+":"+position.TokenAddress] = position
	return nil
}

func (s *fakePositionStore) Get(_ context.Context, accountID string, tokenAddress string) (model.TradePosition, error) {
	position, ok := s.positions[accountID+":"+tokenAddress]
	if !ok {
		return model.TradePosition{}, ErrRuntimePositionNotFound
	}
	return position, nil
}

func (s *fakePositionStore) List(_ context.Context, accountID string) ([]model.TradePosition, error) {
	items := make([]model.TradePosition, 0)
	for _, position := range s.positions {
		if position.AccountID == accountID && position.Status == model.TradePositionStatusOpen {
			items = append(items, position)
		}
	}
	return items, nil
}

func (s *fakePositionStore) Delete(_ context.Context, accountID string, tokenAddress string) error {
	s.deleteCalls++
	if s.deleteFailures > 0 {
		s.deleteFailures--
		return errors.New("temporary Redis delete failure")
	}
	delete(s.positions, accountID+":"+tokenAddress)
	return nil
}

func (p fakeWalletBalanceProvider) GetSOLBalance(context.Context, string) (float64, error) {
	return p.balance, p.err
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

func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout")
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

func TestUpdateBuyAmountUSDUpdatesRuntimeAccountForBothModes(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	amount, err := svc.UpdateBuyAmountUSD(context.Background(), 12.5)
	if err != nil {
		t.Fatalf("update buy amount: %v", err)
	}
	if amount != 12.5 || svc.GetBuyAmountUSD() != 12.5 || repo.account.BuyAmountUSD != 12.5 {
		t.Fatalf("buy amount was not synchronized: return=%.2f runtime=%.2f repo=%.2f", amount, svc.GetBuyAmountUSD(), repo.account.BuyAmountUSD)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "configured-amount-buy", SignalType: model.TradeSignalTypeBuy,
		StrategyCode: "test", TokenAddress: "token-configured", Interval: "1m", SignalTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process configured amount buy: %v", err)
	}
	waitFor(t, func() bool { return len(repo.orders) == 1 })
	if repo.orders[0].IntentAmountUSD != 12.5 || executor.lastRequest.Account.BuyAmountUSD != 12.5 {
		t.Fatalf("configured amount was not used consistently: order=%.2f executor=%.2f", repo.orders[0].IntentAmountUSD, executor.lastRequest.Account.BuyAmountUSD)
	}
	if _, err := svc.UpdateBuyAmountUSD(context.Background(), 0); !errors.Is(err, ErrInvalidBuyAmountUSD) {
		t.Fatalf("expected invalid amount error, got %v", err)
	}
}

func TestUpdateTradeModePersistsState(t *testing.T) {
	repo := newFakeRepo()
	repo.periodSummary = model.TradeModePeriodSummary{BuyCount: 3, SellCount: 2, RealizedPNL: 1.25}
	executor := &fakeExecutor{}
	notifier := &fakeNotifier{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithNotifier(notifier), WithWalletBalanceProvider(fakeWalletBalanceProvider{balance: 0.998}))
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
	if len(notifier.modeChanges) != 1 {
		t.Fatalf("expected one mode change notification, got %d", len(notifier.modeChanges))
	}
	change := notifier.modeChanges[0]
	if change.PreviousMode != model.TradeModePaper || change.CurrentMode != model.TradeModeLive || change.WalletAddress != repo.account.WalletAddress {
		t.Fatalf("unexpected mode change notification: %+v", change)
	}
	if change.Summary.BuyCount != 3 || change.Summary.SellCount != 2 || change.Summary.RealizedPNL != 1.25 {
		t.Fatalf("unexpected period summary: %+v", change.Summary)
	}
	if change.WalletBalance == nil || *change.WalletBalance != 0.998 {
		t.Fatalf("unexpected wallet balance: %v", change.WalletBalance)
	}
	if _, err := svc.UpdateTradeMode(context.Background(), model.TradeModePaper); err != nil {
		t.Fatalf("switch back to paper: %v", err)
	}
	if len(notifier.modeChanges) != 2 || notifier.modeChanges[1].PreviousMode != model.TradeModeLive || notifier.modeChanges[1].CurrentMode != model.TradeModePaper {
		t.Fatalf("expected live-to-paper notification, got %+v", notifier.modeChanges)
	}
	if notifier.modeChanges[1].WalletBalance != nil {
		t.Fatalf("paper switch must not include wallet balance: %v", notifier.modeChanges[1].WalletBalance)
	}
}

func TestUpdateTradeModeDoesNotEnableLiveWhenBalanceLookupFails(t *testing.T) {
	repo := newFakeRepo()
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil, WithWalletBalanceProvider(fakeWalletBalanceProvider{err: errors.New("rpc unavailable")}))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	setCallsBefore := repo.setTradeModeCalls
	if _, err := svc.UpdateTradeMode(context.Background(), model.TradeModeLive); err == nil || !strings.Contains(err.Error(), "rpc unavailable") {
		t.Fatalf("expected balance lookup error, got %v", err)
	}
	if svc.GetTradeMode() != model.TradeModePaper || repo.tradeMode != model.TradeModePaper {
		t.Fatalf("live mode must not be enabled after balance lookup failure: service=%s repo=%s", svc.GetTradeMode(), repo.tradeMode)
	}
	if repo.setTradeModeCalls != setCallsBefore {
		t.Fatalf("mode state must not be persisted after balance lookup failure")
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
	waitFor(t, func() bool { return len(repo.orders) == 1 && repo.lastBuyFill != nil })
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
	time.Sleep(50 * time.Millisecond)
	if len(repo.orders) != 1 {
		t.Fatalf("expected duplicate open-position buy to be skipped, orders=%d", len(repo.orders))
	}
}

func TestProcessSignalNotifiesSignalAndFilledTrade(t *testing.T) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil, WithNotifier(notifier), WithSupplyProvider(fakeSupplyProvider{supply: 1000}))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sig-notify", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout", TokenAddress: "token-notify", SignalTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process signal: %v", err)
	}
	waitFor(t, func() bool { return len(notifier.signals) == 1 && len(notifier.fills) == 1 })
	if notifier.signals[0].TradeMode != model.TradeModePaper || notifier.fills[0].TradeMode != model.TradeModePaper {
		t.Fatalf("expected paper notifications, got signal=%s fill=%s", notifier.signals[0].TradeMode, notifier.fills[0].TradeMode)
	}
	if notifier.fills[0].ExecutedMarketCap != 100 {
		t.Fatalf("expected executed market cap from avg price 0.1 * supply 1000, got %f", notifier.fills[0].ExecutedMarketCap)
	}
}

func TestProcessBuySignalRejectsWhenJupiterQuoteSlippageTooLarge(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{quoteResult: QuoteResult{AvgPrice: 0.104}}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithSupplyProvider(fakeSupplyProvider{supply: 1000}))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID:         "sig-slippage",
		SignalType:       model.TradeSignalTypeBuy,
		StrategyCode:     "pressure_breakout",
		TokenAddress:     "token-a",
		Interval:         "1m",
		SignalTime:       time.Now().UTC(),
		TriggerMarketCap: 100,
		Reason:           "buy",
	})
	if err != nil {
		t.Fatalf("process signal: %v", err)
	}
	waitFor(t, func() bool {
		return len(repo.signals) == 1 && repo.signals[0].ConsumeStatus == "rejected"
	})
	if len(repo.orders) != 0 {
		t.Fatalf("expected no order when quote slippage is too large, got %d", len(repo.orders))
	}
	if executor.executeCalls != 0 {
		t.Fatalf("expected executor Execute not to be called, got %d", executor.executeCalls)
	}
	if executor.quoteCalls != 4 {
		t.Fatalf("expected initial quote plus 3 retries, got %d", executor.quoteCalls)
	}
	if !strings.Contains(repo.signals[0].Reason, "连续 4 次报价滑点均大于 3.00%") {
		t.Fatalf("expected slippage rejection reason, got %q", repo.signals[0].Reason)
	}
}

func TestProcessBuySignalRejectsBlacklistedCAWithoutCallingExecutor(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	riskStore := &fakeCARiskStore{states: map[string]model.CABlacklistState{
		"token-blacklisted": {TokenAddress: "token-blacklisted", IsBlacklisted: true},
	}}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithCARiskStore(riskStore))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sig-blacklisted", SignalType: model.TradeSignalTypeBuy,
		TokenAddress: "token-blacklisted", SignalTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process signal: %v", err)
	}
	waitFor(t, func() bool { return len(repo.signals) == 1 && repo.signals[0].ConsumeStatus == "rejected" })
	if executor.quoteCalls != 0 || executor.executeCalls != 0 || len(repo.orders) != 0 {
		t.Fatalf("blacklisted CA must stop before quote/order: quote=%d execute=%d orders=%d", executor.quoteCalls, executor.executeCalls, len(repo.orders))
	}
}

func TestProcessBuySignalRejectsCADuringLossCooldown(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{}
	cooldownUntil := time.Now().UTC().Add(time.Hour)
	riskStore := &fakeCARiskStore{states: map[string]model.CABlacklistState{
		"token-cooling": {TokenAddress: "token-cooling", ConsecutiveLossCount: 1, CooldownUntil: &cooldownUntil},
	}}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithCARiskStore(riskStore))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sig-cooling", SignalType: model.TradeSignalTypeBuy,
		TokenAddress: "token-cooling", SignalTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process signal: %v", err)
	}
	waitFor(t, func() bool { return len(repo.signals) == 1 && repo.signals[0].ConsumeStatus == "rejected" })
	if executor.quoteCalls != 0 || executor.executeCalls != 0 || len(repo.orders) != 0 {
		t.Fatalf("cooling CA must stop before quote/order: quote=%d execute=%d orders=%d", executor.quoteCalls, executor.executeCalls, len(repo.orders))
	}
}

func TestProcessBuySignalBuysWhenThirdRetryFallsWithinSlippageLimit(t *testing.T) {
	repo := newFakeRepo()
	executor := &fakeExecutor{quoteResults: []QuoteResult{{AvgPrice: 0.104}, {AvgPrice: 0.105}, {AvgPrice: 0.106}, {AvgPrice: 0.102}}}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithSupplyProvider(fakeSupplyProvider{supply: 1000}))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sig-slippage-recovered", SignalType: model.TradeSignalTypeBuy, StrategyCode: "pressure_breakout",
		TokenAddress: "token-a", Interval: "1m", SignalTime: time.Now().UTC(), TriggerMarketCap: 100, Reason: "buy",
	})
	if err != nil {
		t.Fatalf("process signal: %v", err)
	}
	if executor.quoteCalls != 4 || executor.executeCalls != 1 {
		t.Fatalf("expected buy after third retry, quote=%d execute=%d", executor.quoteCalls, executor.executeCalls)
	}
	waitFor(t, func() bool { return len(repo.orders) == 1 })
	if len(repo.orders) != 1 {
		t.Fatalf("expected one buy order, got %d", len(repo.orders))
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

func TestProcessSellSignalUsesLatestRedisPositionQuantity(t *testing.T) {
	repo := newFakeRepo()
	databasePosition := model.TradePosition{ID: "pos-1", AccountID: repo.account.ID, TradeMode: model.TradeModePaper, TokenAddress: "token-a", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10.15}
	repo.positions[repo.account.ID+":"+databasePosition.TokenAddress] = databasePosition
	store := newFakePositionStore()
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil, WithPositionStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	redisPosition := databasePosition
	redisPosition.Quantity = 321.5
	if err := store.Save(context.Background(), redisPosition); err != nil {
		t.Fatal(err)
	}

	_, err = svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sell-1", SignalType: model.TradeSignalTypeSell, StrategyCode: "breakout_band_follow",
		TokenAddress: "token-a", Interval: "1m", SignalTime: time.Now().UTC(), Reason: "take profit",
	})
	if err != nil {
		t.Fatalf("process sell signal: %v", err)
	}
	if executor.lastRequest.Position == nil || executor.lastRequest.Position.Quantity != 321.5 {
		t.Fatalf("expected Redis quantity 321.5, got request=%#v", executor.lastRequest)
	}
	if _, ok := store.positions[repo.account.ID+":"+databasePosition.TokenAddress]; ok {
		t.Fatal("expected successful sell to delete Redis position")
	}
}

func TestSuccessfulSellRetriesFailedRuntimePositionDelete(t *testing.T) {
	repo := newFakeRepo()
	position := model.TradePosition{
		ID: "pos-1", AccountID: repo.account.ID, TradeMode: model.TradeModePaper,
		TokenAddress: "token-a", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10,
	}
	repo.positions[repo.account.ID+":"+position.TokenAddress] = position
	repo.positionByID[position.ID] = position
	store := newFakePositionStore()
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil, WithPositionStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	store.deleteFailures = 1

	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sell-1", SignalType: model.TradeSignalTypeSell,
		TokenAddress: position.TokenAddress, SignalTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("process sell signal: %v", err)
	}
	waitFor(t, func() bool {
		_, exists := store.positions[repo.account.ID+":"+position.TokenAddress]
		return store.deleteCalls >= 2 && !exists
	})
}

func TestNewServiceRemovesClosedPositionLeftInRuntimeStore(t *testing.T) {
	repo := newFakeRepo()
	position := model.TradePosition{
		ID: "pos-closed", AccountID: repo.account.ID, TradeMode: model.TradeModeLive,
		TokenAddress: "token-stale", Status: model.TradePositionStatusOpen, Quantity: 100,
	}
	closed := position
	closed.Status = model.TradePositionStatusClosed
	repo.positionByID[position.ID] = closed
	store := newFakePositionStore()
	if err := store.Save(context.Background(), position); err != nil {
		t.Fatal(err)
	}

	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil, WithPositionStore(store))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, exists := store.positions[repo.account.ID+":"+position.TokenAddress]; exists {
		t.Fatal("expected stale closed Redis position to be deleted during startup")
	}
	if _, found, err := svc.FindRuntimePosition(context.Background(), position.TokenAddress); err != nil || found {
		t.Fatalf("expected stale closed position to stay out of runtime state, found=%v err=%v", found, err)
	}
}

func TestProcessSellSignalUsesPositionModeAfterRuntimeModeSwitch(t *testing.T) {
	repo := newFakeRepo()
	repo.tradeMode = model.TradeModeLive
	position := model.TradePosition{
		ID: "pos-paper", AccountID: repo.account.ID, TradeMode: model.TradeModePaper,
		TokenAddress: "token-paper", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10,
	}
	repo.positions[repo.account.ID+":"+position.TokenAddress] = position
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ProcessSignal(context.Background(), model.TradeSignalMessage{
		SignalID: "sell-paper", SignalType: model.TradeSignalTypeSell, StrategyCode: "breakout_band_follow",
		TokenAddress: position.TokenAddress, Interval: "1m", SignalTime: time.Now().UTC(), Reason: "take profit",
	}); err != nil {
		t.Fatalf("process sell signal: %v", err)
	}
	waitFor(t, func() bool { return len(repo.orders) == 1 && len(repo.signals) == 1 })
	if executor.lastRequest.Mode != model.TradeModePaper || repo.orders[0].TradeMode != model.TradeModePaper {
		t.Fatalf("expected position mode paper, request=%s order=%s", executor.lastRequest.Mode, repo.orders[0].TradeMode)
	}
	if repo.signals[0].TradeMode != model.TradeModePaper {
		t.Fatalf("expected persisted sell signal mode paper, got %s", repo.signals[0].TradeMode)
	}
}

func TestClosePositionPersistsManualSignal(t *testing.T) {
	repo := newFakeRepo()
	repo.tradeMode = model.TradeModePaper
	position := model.TradePosition{ID: "pos-1", AccountID: repo.account.ID, TradeMode: model.TradeModeLive, TokenAddress: "token-a", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10.15, LastPrice: 0.11}
	repo.positions[repo.account.ID+":token-a"] = position
	repo.positionByID[position.ID] = position
	executor := &fakeExecutor{}
	svc, err := NewService(
		context.Background(),
		testTradeConfig(t),
		repo,
		executor,
		nil,
		WithSupplyProvider(fakeSupplyProvider{supply: 1000}),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := svc.ClosePosition(context.Background(), "pos-1"); err != nil {
		t.Fatalf("close position: %v", err)
	}
	waitFor(t, func() bool { return repo.manualSignalSeen && len(repo.orders) == 1 })
	if !repo.manualSignalSeen {
		t.Fatalf("expected manual close to persist a trade signal before creating sell order")
	}
	if len(repo.orders) != 1 || repo.orders[0].SignalID == "" {
		t.Fatalf("expected sell order to be linked to persisted signal, orders=%#v", repo.orders)
	}
	if executor.lastRequest.Mode != model.TradeModeLive || repo.orders[0].TradeMode != model.TradeModeLive {
		t.Fatalf("expected manual close to use position mode live, request=%s order=%s", executor.lastRequest.Mode, repo.orders[0].TradeMode)
	}
	if repo.signals[0].TradeMode != model.TradeModeLive {
		t.Fatalf("expected manual close signal to use position mode live, got %s", repo.signals[0].TradeMode)
	}
	if len(repo.signals[0].RawPayloadJSON) == 0 || repo.signals[0].TriggerMarketCap != 110 {
		t.Fatalf("expected manual close payload and market cap, got payload=%s marketCap=%f", repo.signals[0].RawPayloadJSON, repo.signals[0].TriggerMarketCap)
	}
}

func TestClosePositionRejectsDuplicateSubmission(t *testing.T) {
	repo := newFakeRepo()
	position := model.TradePosition{ID: "pos-1", AccountID: repo.account.ID, TradeMode: model.TradeModePaper, TokenAddress: "token-a", Status: model.TradePositionStatusOpen, Quantity: 100, CostAmount: 10.15, LastPrice: 0.11}
	repo.positions[repo.account.ID+":"+position.TokenAddress] = position
	repo.positionByID[position.ID] = position
	executor := &fakeExecutor{}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, executor, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.inFlight[position.TokenAddress] = model.TradeSignalTypeSell
	if _, err := svc.ClosePosition(context.Background(), position.ID); !errors.Is(err, ErrPositionSellInFlight) {
		t.Fatalf("expected duplicate close rejection, got %v", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("expected no duplicate execution, got %d calls", executor.executeCalls)
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
	waitFor(t, func() bool { return len(repo.orders) == 1 })
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

func TestListDailyStatsNormalizesModeAndUsesBeijingDays(t *testing.T) {
	repo := newFakeRepo()
	repo.dailyStats = []model.TradeDailyStatsItem{{Date: "2026-07-29", TradeMode: model.TradeModePaper, RealizedPNL: 1.5}}
	svc, err := NewService(context.Background(), testTradeConfig(t), repo, &fakeExecutor{}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	items, err := svc.ListDailyStats(context.Background(), model.TradeModePaper, 5)
	if err != nil {
		t.Fatalf("list daily stats: %v", err)
	}
	if len(items) != 1 || items[0].RealizedPNL != 1.5 {
		t.Fatalf("unexpected daily stats: %#v", items)
	}
	if repo.dailyStatsMode != model.TradeModePaper {
		t.Fatalf("expected paper mode, got %s", repo.dailyStatsMode)
	}
	if repo.dailyStatsStart.Location() != repo.dailyStatsEnd.Location() || repo.dailyStatsStart.Location().String() != "Asia/Shanghai" {
		t.Fatalf("expected Beijing day bounds, got %s - %s", repo.dailyStatsStart.Location(), repo.dailyStatsEnd.Location())
	}
	if repo.dailyStatsEnd.Sub(repo.dailyStatsStart) != 5*24*time.Hour {
		t.Fatalf("expected 5 Beijing days, got %s", repo.dailyStatsEnd.Sub(repo.dailyStatsStart))
	}
}
