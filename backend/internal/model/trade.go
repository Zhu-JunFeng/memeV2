package model

import (
	"encoding/json"
	"time"
)

type TradeSignalType string

type TradeOrderStatus string

type TradePositionStatus string

type TradeMode string

type TradeExecutionChannel string

const (
	TradeSignalTypeBuy  TradeSignalType = "buy"
	TradeSignalTypeSell TradeSignalType = "sell"

	TradeOrderStatusPending   TradeOrderStatus = "pending"
	TradeOrderStatusSubmitted TradeOrderStatus = "submitted"
	TradeOrderStatusFilled    TradeOrderStatus = "filled"
	TradeOrderStatusFailed    TradeOrderStatus = "failed"

	TradePositionStatusOpen   TradePositionStatus = "open"
	TradePositionStatusClosed TradePositionStatus = "closed"

	TradeModePaper TradeMode = "paper"
	TradeModeLive  TradeMode = "live"

	TradeExecutionChannelJupiterLive  TradeExecutionChannel = "jupiter_live"
	TradeExecutionChannelJupiterPaper TradeExecutionChannel = "jupiter_paper"
)

type TradeAccount struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	WalletAddress       string    `json:"walletAddress"`
	Status              string    `json:"status"`
	BuyAmountUSD        float64   `json:"buyAmountUsd"`
	SlippageBPS         int       `json:"slippageBps"`
	PriorityFeeLamports int64     `json:"priorityFeeLamports"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type TradeSignal struct {
	ID               string          `json:"id"`
	SignalID         string          `json:"signalId"`
	TradeMode        TradeMode       `json:"tradeMode"`
	SignalType       TradeSignalType `json:"signalType"`
	StrategyCode     string          `json:"strategyCode"`
	TokenAddress     string          `json:"tokenAddress"`
	Interval         string          `json:"interval"`
	SignalTime       time.Time       `json:"signalTime"`
	TriggerPrice     float64         `json:"triggerPrice"`
	TriggerMarketCap float64         `json:"triggerMarketCap"`
	Reason           string          `json:"reason"`
	RawPayloadJSON   json.RawMessage `json:"rawPayloadJson"`
	ConsumeStatus    string          `json:"consumeStatus"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type TradeOrder struct {
	ID                  string           `json:"id"`
	AccountID           string           `json:"accountId"`
	SignalID            string           `json:"signalId"`
	TradeMode           TradeMode        `json:"tradeMode"`
	ExecutionChannel    string           `json:"executionChannel"`
	TokenAddress        string           `json:"tokenAddress"`
	Side                TradeSignalType  `json:"side"`
	IntentAmountUSD     float64          `json:"intentAmountUsd"`
	IntentTokenAmount   float64          `json:"intentTokenAmount"`
	Status              TradeOrderStatus `json:"status"`
	JupiterRequestJSON  json.RawMessage  `json:"jupiterRequestJson,omitempty"`
	JupiterResponseJSON json.RawMessage  `json:"jupiterResponseJson,omitempty"`
	SubmitTxHash        string           `json:"submitTxHash"`
	ConfirmedAt         *time.Time       `json:"confirmedAt,omitempty"`
	FailReason          string           `json:"failReason"`
	CreatedAt           time.Time        `json:"createdAt"`
	UpdatedAt           time.Time        `json:"updatedAt"`
}

type TradeFill struct {
	ID                string          `json:"id"`
	OrderID           string          `json:"orderId"`
	TradeMode         TradeMode       `json:"tradeMode"`
	IsSimulated       bool            `json:"isSimulated"`
	TxHash            string          `json:"txHash"`
	Side              TradeSignalType `json:"side"`
	TokenAddress      string          `json:"tokenAddress"`
	FilledTokenAmount float64         `json:"filledTokenAmount"`
	FilledQuoteAmount float64         `json:"filledQuoteAmount"`
	AvgPrice          float64         `json:"avgPrice"`
	FeeAmount         float64         `json:"feeAmount"`
	FeeAsset          string          `json:"feeAsset"`
	ExecutedAt        time.Time       `json:"executedAt"`
	CreatedAt         time.Time       `json:"createdAt"`
}

type TradePosition struct {
	ID                string              `json:"id"`
	AccountID         string              `json:"accountId"`
	TradeMode         TradeMode           `json:"tradeMode"`
	TokenAddress      string              `json:"tokenAddress"`
	Status            TradePositionStatus `json:"status"`
	OpenOrderID       string              `json:"openOrderId"`
	CloseOrderID      string              `json:"closeOrderId"`
	Quantity          float64             `json:"quantity"`
	CostAmount        float64             `json:"costAmount"`
	AvgCostPrice      float64             `json:"avgCostPrice"`
	LastPrice         float64             `json:"lastPrice"`
	MarketValue       float64             `json:"marketValue"`
	RealizedPNL       float64             `json:"realizedPnl"`
	UnrealizedPNL     float64             `json:"unrealizedPnl"`
	MaxProfitRate     float64             `json:"maxProfitRate"`
	MaxDrawdownAmount float64             `json:"maxDrawdownAmount"`
	OpenedAt          time.Time           `json:"openedAt"`
	ClosedAt          *time.Time          `json:"closedAt,omitempty"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type TradeOrderEvent struct {
	ID         string          `json:"id"`
	OrderID    string          `json:"orderId"`
	EventType  string          `json:"eventType"`
	EventTime  time.Time       `json:"eventTime"`
	DetailJSON json.RawMessage `json:"detailJson"`
}

type TradeSignalMessage struct {
	SignalID         string          `json:"signalId"`
	SignalType       TradeSignalType `json:"signalType"`
	StrategyCode     string          `json:"strategyCode"`
	TokenAddress     string          `json:"tokenAddress"`
	Interval         string          `json:"interval"`
	SignalTime       time.Time       `json:"signalTime"`
	TriggerPrice     float64         `json:"triggerPrice"`
	TriggerMarketCap float64         `json:"triggerMarketCap"`
	Reason           string          `json:"reason"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}
