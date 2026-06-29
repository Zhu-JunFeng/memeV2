package trade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	solana "github.com/gagliardetto/solana-go"
	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/config"
	"solana-meme-backtest/backend/internal/datasource"
	"solana-meme-backtest/backend/internal/eventbus"
	"solana-meme-backtest/backend/internal/model"
)

var ErrTradeDisabled = errors.New("交易模块未启用")
var ErrTradeExecutionNotReady = errors.New("Jupiter 执行器尚未配置完成")
var ErrInvalidTradeMode = errors.New("交易模式不合法")

type Repository interface {
	EnsureAccount(ctx context.Context, account model.TradeAccount) (model.TradeAccount, error)
	GetAccountByName(ctx context.Context, name string) (model.TradeAccount, error)
	ListAccounts(ctx context.Context) ([]model.TradeAccount, error)
	GetTradeMode(ctx context.Context) (model.TradeMode, error)
	SetTradeMode(ctx context.Context, mode model.TradeMode) error
	InsertSignalIfAbsent(ctx context.Context, signal model.TradeSignal) (model.TradeSignal, bool, error)
	UpdateSignalStatus(ctx context.Context, signalID string, status string) error
	GetSignalByID(ctx context.Context, id string) (model.TradeSignal, error)
	ListSignals(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeSignal, error)
	ListTradeSummaries(ctx context.Context) ([]model.TradeSummaryItem, error)
	CreateOrder(ctx context.Context, order model.TradeOrder) (model.TradeOrder, error)
	UpdateOrderExecution(ctx context.Context, orderID string, status model.TradeOrderStatus, txHash string, requestJSON json.RawMessage, responseJSON json.RawMessage, failReason string, confirmedAt *time.Time) error
	AddOrderEvent(ctx context.Context, orderID string, eventType string, detail any) error
	SaveFilledBuy(ctx context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error
	SaveFilledSell(ctx context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error
	ListOrders(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeOrder, error)
	GetOrder(ctx context.Context, id string) (model.TradeOrder, error)
	ListPositions(ctx context.Context, status string, tradeMode model.TradeMode, limit int) ([]model.TradePosition, error)
	GetPosition(ctx context.Context, id string) (model.TradePosition, error)
	UpdatePositionMark(ctx context.Context, positionID string, lastPrice float64, marketValue float64, unrealized float64, maxProfitRate float64, maxDrawdownAmount float64) error
}

type Executor interface {
	Execute(ctx context.Context, req ExecutionRequest) (ExecutionResult, error)
}

type ExecutionRequest struct {
	Account  model.TradeAccount
	Signal   model.TradeSignal
	Order    model.TradeOrder
	Position *model.TradePosition
	Config   config.TradeConfig
	Mode     model.TradeMode
}

type ExecutionResult struct {
	RequestPayload   json.RawMessage
	ResponsePayload  json.RawMessage
	TxHash           string
	FilledToken      float64
	FilledQuote      float64
	AvgPrice         float64
	FeeAmount        float64
	FeeAsset         string
	ExecutedAt       time.Time
	Simulated        bool
	ExecutionChannel string
}

type Service struct {
	cfg            config.TradeConfig
	repo           Repository
	executor       Executor
	priceProvider  datasource.TokenPriceProvider
	supplyProvider datasource.TokenSupplyProvider
	account        model.TradeAccount
	enabled        bool
	modeMu         sync.RWMutex
	tradeMode      model.TradeMode
	eventBus       *eventbus.Broker
	persister      *asyncPersister
	runtimeMu      sync.Mutex
	openPositions  map[string]model.TradePosition
	inFlight       map[string]model.TradeSignalType
	seenSignals    map[string]struct{}
}

type ServiceOption func(*Service)

func WithEventBus(bus *eventbus.Broker) ServiceOption {
	return func(s *Service) {
		s.eventBus = bus
	}
}

func WithSupplyProvider(provider datasource.TokenSupplyProvider) ServiceOption {
	return func(s *Service) {
		s.supplyProvider = provider
	}
}

func NewService(ctx context.Context, cfg config.TradeConfig, repo Repository, executor Executor, priceProvider datasource.TokenPriceProvider, options ...ServiceOption) (*Service, error) {
	svc := &Service{
		cfg:           cfg,
		repo:          repo,
		executor:      executor,
		priceProvider: priceProvider,
		enabled:       cfg.Enabled,
		tradeMode:     model.TradeModePaper,
		openPositions: map[string]model.TradePosition{},
		inFlight:      map[string]model.TradeSignalType{},
		seenSignals:   map[string]struct{}{},
		persister:     newAsyncPersister(ctx, 512),
	}
	for _, option := range options {
		option(svc)
	}
	if !cfg.Enabled {
		return svc, nil
	}
	walletAddress, err := resolveWalletAddress(cfg.WalletAddress, cfg.WalletPrivateKey)
	if err != nil {
		return nil, err
	}
	account, err := repo.EnsureAccount(ctx, model.TradeAccount{
		Name:                defaultString(cfg.AccountName, "default"),
		WalletAddress:       walletAddress,
		Status:              "active",
		BuyAmountSOL:        cfg.BuyAmountSOL,
		SlippageBPS:         cfg.SlippageBPS,
		PriorityFeeLamports: cfg.PriorityFee,
	})
	if err != nil {
		return nil, err
	}
	svc.account = account
	mode, err := repo.GetTradeMode(ctx)
	if err != nil {
		return nil, err
	}
	if mode == "" {
		mode = model.TradeModePaper
		if err := repo.SetTradeMode(ctx, mode); err != nil {
			return nil, err
		}
	}
	if !isValidTradeMode(mode) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidTradeMode, mode)
	}
	svc.tradeMode = mode
	if err := svc.loadRuntimePositions(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) Enabled() bool {
	return s.enabled
}

func (s *Service) ListAccounts(ctx context.Context) ([]model.TradeAccount, error) {
	return s.repo.ListAccounts(ctx)
}

func (s *Service) GetTradeMode() model.TradeMode {
	s.modeMu.RLock()
	defer s.modeMu.RUnlock()
	return s.tradeMode
}

func (s *Service) UpdateTradeMode(ctx context.Context, mode model.TradeMode) (model.TradeMode, error) {
	if !s.enabled {
		return "", ErrTradeDisabled
	}
	if !isValidTradeMode(mode) {
		return "", fmt.Errorf("%w: %s", ErrInvalidTradeMode, mode)
	}
	if err := s.repo.SetTradeMode(ctx, mode); err != nil {
		return "", err
	}
	s.modeMu.Lock()
	s.tradeMode = mode
	s.modeMu.Unlock()
	return mode, nil
}

func (s *Service) ListSignals(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeSignal, error) {
	return s.repo.ListSignals(ctx, normalizeTradeModeFilter(tradeMode), limit)
}

func (s *Service) ListTradeSummaries(ctx context.Context) ([]model.TradeSummaryItem, error) {
	return s.repo.ListTradeSummaries(ctx)
}

func (s *Service) ListOrders(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeOrder, error) {
	return s.repo.ListOrders(ctx, normalizeTradeModeFilter(tradeMode), limit)
}

func (s *Service) GetOrder(ctx context.Context, id string) (model.TradeOrder, error) {
	return s.repo.GetOrder(ctx, id)
}

func (s *Service) ListPositions(ctx context.Context, status string, tradeMode model.TradeMode, limit int) ([]model.TradePosition, error) {
	items, err := s.repo.ListPositions(ctx, status, normalizeTradeModeFilter(tradeMode), limit)
	if err != nil {
		return nil, err
	}
	for index := range items {
		s.enrichExecutedMarketCaps(ctx, &items[index])
	}
	return items, nil
}

func (s *Service) GetPosition(ctx context.Context, id string) (model.TradePosition, error) {
	item, err := s.repo.GetPosition(ctx, id)
	if err != nil {
		return model.TradePosition{}, err
	}
	s.enrichExecutedMarketCaps(ctx, &item)
	return item, nil
}

func (s *Service) ProcessSignal(ctx context.Context, message model.TradeSignalMessage) (model.TradeSignal, error) {
	if !s.enabled {
		return model.TradeSignal{}, ErrTradeDisabled
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return model.TradeSignal{}, err
	}
	signal := model.TradeSignal{
		ID:               uuid.NewString(),
		SignalID:         message.SignalID,
		TradeMode:        s.GetTradeMode(),
		SignalType:       message.SignalType,
		StrategyCode:     message.StrategyCode,
		TokenAddress:     message.TokenAddress,
		Interval:         message.Interval,
		SignalTime:       message.SignalTime,
		TriggerPrice:     message.TriggerPrice,
		TriggerMarketCap: message.TriggerMarketCap,
		Reason:           message.Reason,
		RawPayloadJSON:   raw,
		ConsumeStatus:    "accepted",
	}
	if !s.markSignalSeen(signal.SignalID) {
		return signal, nil
	}
	s.enqueuePersistSignal(signal)
	if err := s.handleSignal(ctx, signal); err != nil {
		s.enqueueSignalStatus(signal.ID, "failed")
		return signal, err
	}
	s.enqueueSignalStatus(signal.ID, "executed")
	return signal, nil
}

// handleSignal 只负责“信号 -> 意图 -> 执行 -> 持仓状态”这条主链路，
// 这样 Redis 消费、HTTP 手动触发、后续定时补单都能复用同一套交易语义。
func (s *Service) handleSignal(ctx context.Context, signal model.TradeSignal) error {
	switch signal.SignalType {
	case model.TradeSignalTypeBuy:
		if !s.tryBeginBuy(signal.TokenAddress) {
			return nil
		}
		return s.executeBuy(ctx, signal)
	case model.TradeSignalTypeSell:
		position, ok := s.tryBeginSell(signal.TokenAddress)
		if !ok {
			return nil
		}
		return s.executeSell(ctx, signal, position)
	default:
		return fmt.Errorf("不支持的信号类型: %s", signal.SignalType)
	}
}

func (s *Service) RetryOrder(ctx context.Context, orderID string) (model.TradeOrder, error) {
	order, err := s.repo.GetOrder(ctx, orderID)
	if err != nil {
		return model.TradeOrder{}, err
	}
	signal, err := s.repo.GetSignalByID(ctx, order.SignalID)
	if err != nil {
		return model.TradeOrder{}, err
	}
	return order, s.handleSignal(ctx, signal)
}

func (s *Service) ClosePosition(ctx context.Context, positionID string) (model.TradePosition, error) {
	position, err := s.repo.GetPosition(ctx, positionID)
	if err != nil {
		return model.TradePosition{}, err
	}
	signal := model.TradeSignal{
		ID:               uuid.NewString(),
		SignalID:         uuid.NewString(),
		TradeMode:        s.GetTradeMode(),
		SignalType:       model.TradeSignalTypeSell,
		StrategyCode:     "manual_close",
		TokenAddress:     position.TokenAddress,
		Interval:         "manual",
		SignalTime:       time.Now().UTC(),
		TriggerPrice:     position.LastPrice,
		TriggerMarketCap: position.LastPrice,
		Reason:           "手动平仓",
		ConsumeStatus:    "manual",
	}
	storedSignal, _, err := s.repo.InsertSignalIfAbsent(ctx, signal)
	if err != nil {
		return model.TradePosition{}, err
	}
	signal = storedSignal
	s.publishSignal(ctx, signal.ID)
	return position, s.executeSell(ctx, signal, position)
}

func (s *Service) RefreshOpenPositions(ctx context.Context) error {
	if !s.enabled || s.priceProvider == nil {
		return nil
	}
	s.runtimeMu.Lock()
	positions := make([]model.TradePosition, 0, len(s.openPositions))
	for _, position := range s.openPositions {
		positions = append(positions, position)
	}
	s.runtimeMu.Unlock()
	for _, position := range positions {
		price, err := s.priceProvider.GetTokenPrice(ctx, position.TokenAddress)
		if err != nil {
			continue
		}
		marketValue := price * position.Quantity
		unrealized := marketValue - position.CostAmount
		profitRate := 0.0
		if position.CostAmount > 0 {
			profitRate = unrealized / position.CostAmount
		}
		drawdown := unrealized
		if err := s.repo.UpdatePositionMark(ctx, position.ID, price, marketValue, unrealized, profitRate, drawdown); err != nil {
			continue
		}
		s.publishPosition(ctx, position.ID)
	}
	return nil
}

func (s *Service) executeBuy(ctx context.Context, signal model.TradeSignal) error {
	mode := signal.TradeMode
	order := model.TradeOrder{
		ID:                uuid.NewString(),
		AccountID:         s.account.ID,
		SignalID:          signal.ID,
		TradeMode:         mode,
		ExecutionChannel:  executionChannelForMode(mode),
		TokenAddress:      signal.TokenAddress,
		Side:              model.TradeSignalTypeBuy,
		IntentAmountUSD:   0,
		IntentAmountSOL:   s.account.BuyAmountSOL,
		IntentTokenAmount: 0,
		Status:            model.TradeOrderStatusPending,
	}
	s.enqueueCreateOrder(order, map[string]any{"signalId": signal.SignalID})
	result, err := s.executor.Execute(ctx, ExecutionRequest{Account: s.account, Signal: signal, Order: order, Config: s.cfg, Mode: mode})
	if err != nil {
		s.finishInFlight(signal.TokenAddress)
		s.enqueueOrderFailure(order.ID, err.Error())
		return err
	}
	position := model.TradePosition{
		ID:           uuid.NewString(),
		AccountID:    s.account.ID,
		TradeMode:    mode,
		TokenAddress: order.TokenAddress,
		Status:       model.TradePositionStatusOpen,
		OpenOrderID:  order.ID,
		Quantity:     result.FilledToken,
		CostAmount:   result.FilledQuote + result.FeeAmount,
		AvgCostPrice: result.AvgPrice,
		LastPrice:    result.AvgPrice,
		MarketValue:  result.FilledQuote,
		OpenedAt:     result.ExecutedAt,
		UpdatedAt:    result.ExecutedAt,
	}
	fill := model.TradeFill{
		ID:                uuid.NewString(),
		OrderID:           order.ID,
		TradeMode:         mode,
		IsSimulated:       result.Simulated,
		TxHash:            result.TxHash,
		Side:              model.TradeSignalTypeBuy,
		TokenAddress:      order.TokenAddress,
		FilledTokenAmount: result.FilledToken,
		FilledQuoteAmount: result.FilledQuote,
		AvgPrice:          result.AvgPrice,
		FeeAmount:         result.FeeAmount,
		FeeAsset:          defaultString(result.FeeAsset, "USD"),
		ExecutedAt:        result.ExecutedAt,
	}
	s.markOpenPosition(position)
	s.enqueueFilledBuy(order, position, fill, result)
	return nil
}

func (s *Service) executeSell(ctx context.Context, signal model.TradeSignal, position model.TradePosition) error {
	mode := signal.TradeMode
	order := model.TradeOrder{
		ID:                uuid.NewString(),
		AccountID:         s.account.ID,
		SignalID:          signal.ID,
		TradeMode:         mode,
		ExecutionChannel:  executionChannelForMode(mode),
		TokenAddress:      signal.TokenAddress,
		Side:              model.TradeSignalTypeSell,
		IntentAmountUSD:   0,
		IntentAmountSOL:   0,
		IntentTokenAmount: position.Quantity,
		Status:            model.TradeOrderStatusPending,
	}
	s.enqueueCreateOrder(order, map[string]any{"positionId": position.ID})
	result, err := s.executor.Execute(ctx, ExecutionRequest{Account: s.account, Signal: signal, Order: order, Position: &position, Config: s.cfg, Mode: mode})
	if err != nil {
		s.restoreOpenPosition(position)
		s.enqueueOrderFailure(order.ID, err.Error())
		return err
	}
	fill := model.TradeFill{
		ID:                uuid.NewString(),
		OrderID:           order.ID,
		TradeMode:         mode,
		IsSimulated:       result.Simulated,
		TxHash:            result.TxHash,
		Side:              model.TradeSignalTypeSell,
		TokenAddress:      order.TokenAddress,
		FilledTokenAmount: result.FilledToken,
		FilledQuoteAmount: result.FilledQuote,
		AvgPrice:          result.AvgPrice,
		FeeAmount:         result.FeeAmount,
		FeeAsset:          defaultString(result.FeeAsset, "USD"),
		ExecutedAt:        result.ExecutedAt,
	}
	s.finishInFlight(signal.TokenAddress)
	s.enqueueFilledSell(position, order, fill, result)
	return nil
}

func (s *Service) loadRuntimePositions(ctx context.Context) error {
	items, err := s.repo.ListPositions(ctx, string(model.TradePositionStatusOpen), "", 200)
	if err != nil {
		return err
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	for _, item := range items {
		s.openPositions[item.TokenAddress] = item
	}
	return nil
}

func (s *Service) markSignalSeen(signalID string) bool {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if _, exists := s.seenSignals[signalID]; exists {
		return false
	}
	s.seenSignals[signalID] = struct{}{}
	return true
}

func (s *Service) tryBeginBuy(tokenAddress string) bool {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if _, exists := s.openPositions[tokenAddress]; exists {
		return false
	}
	if _, busy := s.inFlight[tokenAddress]; busy {
		return false
	}
	s.inFlight[tokenAddress] = model.TradeSignalTypeBuy
	return true
}

func (s *Service) tryBeginSell(tokenAddress string) (model.TradePosition, bool) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if _, busy := s.inFlight[tokenAddress]; busy {
		return model.TradePosition{}, false
	}
	position, exists := s.openPositions[tokenAddress]
	if !exists {
		return model.TradePosition{}, false
	}
	delete(s.openPositions, tokenAddress)
	s.inFlight[tokenAddress] = model.TradeSignalTypeSell
	return position, true
}

func (s *Service) markOpenPosition(position model.TradePosition) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.openPositions[position.TokenAddress] = position
	delete(s.inFlight, position.TokenAddress)
}

func (s *Service) restoreOpenPosition(position model.TradePosition) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.openPositions[position.TokenAddress] = position
	delete(s.inFlight, position.TokenAddress)
}

func (s *Service) finishInFlight(tokenAddress string) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	delete(s.inFlight, tokenAddress)
}

func (s *Service) enqueuePersistSignal(signal model.TradeSignal) {
	s.persister.Enqueue(persistTask{
		name: "insert_signal",
		run: func(ctx context.Context) error {
			stored, _, err := s.repo.InsertSignalIfAbsent(ctx, signal)
			if err != nil {
				return err
			}
			s.publishSignal(ctx, stored.ID)
			return nil
		},
	})
}

func (s *Service) enqueueSignalStatus(signalID string, status string) {
	s.persister.Enqueue(persistTask{
		name: "update_signal_status",
		run: func(ctx context.Context) error {
			if err := s.repo.UpdateSignalStatus(ctx, signalID, status); err != nil {
				return err
			}
			s.publishSignal(ctx, signalID)
			return nil
		},
	})
}

func (s *Service) enqueueCreateOrder(order model.TradeOrder, detail map[string]any) {
	s.persister.Enqueue(persistTask{
		name: "create_order",
		run: func(ctx context.Context) error {
			stored, err := s.repo.CreateOrder(ctx, order)
			if err != nil {
				return err
			}
			if err := s.repo.AddOrderEvent(ctx, stored.ID, "created", detail); err != nil {
				return err
			}
			s.publishOrder(ctx, stored.ID)
			return nil
		},
	})
}

func (s *Service) enqueueOrderFailure(orderID string, reason string) {
	s.persister.Enqueue(persistTask{
		name: "order_failed",
		run: func(ctx context.Context) error {
			if err := s.repo.UpdateOrderExecution(ctx, orderID, model.TradeOrderStatusFailed, "", nil, nil, reason, nil); err != nil {
				return err
			}
			if err := s.repo.AddOrderEvent(ctx, orderID, "failed", map[string]any{"error": reason}); err != nil {
				return err
			}
			s.publishOrder(ctx, orderID)
			return nil
		},
	})
}

func (s *Service) enqueueFilledBuy(order model.TradeOrder, position model.TradePosition, fill model.TradeFill, result ExecutionResult) {
	s.persister.Enqueue(persistTask{
		name: "filled_buy",
		run: func(ctx context.Context) error {
			if err := s.repo.UpdateOrderExecution(ctx, order.ID, model.TradeOrderStatusSubmitted, result.TxHash, result.RequestPayload, result.ResponsePayload, "", &result.ExecutedAt); err != nil {
				return err
			}
			if err := s.repo.SaveFilledBuy(ctx, position, order, fill); err != nil {
				return err
			}
			s.publishOrder(ctx, order.ID)
			s.publishPosition(ctx, position.ID)
			return nil
		},
	})
}

func (s *Service) enqueueFilledSell(position model.TradePosition, order model.TradeOrder, fill model.TradeFill, result ExecutionResult) {
	s.persister.Enqueue(persistTask{
		name: "filled_sell",
		run: func(ctx context.Context) error {
			if err := s.repo.UpdateOrderExecution(ctx, order.ID, model.TradeOrderStatusSubmitted, result.TxHash, result.RequestPayload, result.ResponsePayload, "", &result.ExecutedAt); err != nil {
				return err
			}
			if err := s.repo.SaveFilledSell(ctx, position, order, fill); err != nil {
				return err
			}
			s.publishOrder(ctx, order.ID)
			s.publishPosition(ctx, position.ID)
			return nil
		},
	})
}

func (s *Service) publishSignal(ctx context.Context, id string) {
	if s.eventBus == nil {
		return
	}
	item, err := s.repo.GetSignalByID(ctx, id)
	if err != nil {
		return
	}
	s.eventBus.Publish(eventbus.TopicSignals, eventbus.Event{Type: eventbus.EventUpsert, ID: item.ID, Data: item})
}

func (s *Service) publishOrder(ctx context.Context, id string) {
	if s.eventBus == nil {
		return
	}
	item, err := s.repo.GetOrder(ctx, id)
	if err != nil {
		return
	}
	s.eventBus.Publish(eventbus.TopicOrders, eventbus.Event{Type: eventbus.EventUpsert, ID: item.ID, Data: item})
}

func (s *Service) publishPosition(ctx context.Context, id string) {
	if s.eventBus == nil {
		return
	}
	item, err := s.GetPosition(ctx, id)
	if err != nil {
		return
	}
	s.eventBus.Publish(eventbus.TopicPositions, eventbus.Event{Type: eventbus.EventUpsert, ID: item.ID, Data: item})
}

func (s *Service) enrichExecutedMarketCaps(ctx context.Context, item *model.TradePosition) {
	if item == nil || s.supplyProvider == nil {
		return
	}
	supply, err := s.supplyProvider.GetTokenSupply(ctx, item.TokenAddress)
	if err != nil || supply <= 0 {
		return
	}
	if item.EntryExecutedPrice > 0 {
		item.EntryMarketCap = item.EntryExecutedPrice * supply
	}
	if item.ExitExecutedPrice > 0 {
		item.ExitMarketCap = item.ExitExecutedPrice * supply
	}
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func isValidTradeMode(mode model.TradeMode) bool {
	return mode == model.TradeModePaper || mode == model.TradeModeLive
}

func normalizeTradeModeFilter(mode model.TradeMode) model.TradeMode {
	if !isValidTradeMode(mode) {
		return ""
	}
	return mode
}

func executionChannelForMode(mode model.TradeMode) string {
	if mode == model.TradeModePaper {
		return string(model.TradeExecutionChannelJupiterPaper)
	}
	return string(model.TradeExecutionChannelJupiterLive)
}

func resolveWalletAddress(configured string, privateKey string) (string, error) {
	configured = strings.TrimSpace(configured)
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		if configured == "" {
			return "", errors.New("交易钱包私钥未配置")
		}
		return configured, nil
	}
	key, err := solana.PrivateKeyFromBase58(privateKey)
	if err != nil {
		return "", fmt.Errorf("交易钱包私钥格式错误: %w", err)
	}
	derived := key.PublicKey().String()
	if configured != "" && configured != derived {
		return "", fmt.Errorf("配置的钱包地址与私钥推导地址不一致: %s != %s", configured, derived)
	}
	return derived, nil
}
