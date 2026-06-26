package model

import "time"

type Kline struct {
	TokenAddress   string    `json:"tokenAddress"`
	Interval       string    `json:"interval"`
	OpenTime       time.Time `json:"openTime"`
	CloseTime      time.Time `json:"closeTime"`
	Open           float64   `json:"open,omitempty"`
	High           float64   `json:"high,omitempty"`
	Low            float64   `json:"low,omitempty"`
	Close          float64   `json:"close,omitempty"`
	MarketCapOpen  float64   `json:"marketCapOpen"`
	MarketCapHigh  float64   `json:"marketCapHigh"`
	MarketCapLow   float64   `json:"marketCapLow"`
	MarketCapClose float64   `json:"marketCapClose"`
	Volume         float64   `json:"volume"`
}

type Token struct {
	Address string `json:"address"`
	Symbol  string `json:"symbol"`
	Name    string `json:"name"`
}

type TradeSide string

const (
	TradeSideBuy  TradeSide = "buy"
	TradeSideSell TradeSide = "sell"
)

type TradePoint struct {
	Side  TradeSide `json:"side"`
	Time  time.Time `json:"time"`
	Price *float64  `json:"price,omitempty"`
	Note  string    `json:"note,omitempty"`
}

type MatchedTradePoint struct {
	TradePoint
	MatchedKlineTime time.Time `json:"matchedKlineTime"`
	MatchedPrice     float64   `json:"matchedPrice"`
}

type TradeResult struct {
	Buy            MatchedTradePoint `json:"buy"`
	Sell           MatchedTradePoint `json:"sell"`
	Profit         float64           `json:"profit"`
	ProfitRate     float64           `json:"profitRate"`
	HoldingSeconds int64             `json:"holdingSeconds"`
	Win            bool              `json:"win"`
}

type Metrics struct {
	TradeCount            int     `json:"tradeCount"`
	WinRate               float64 `json:"winRate"`
	TotalProfitRate       float64 `json:"totalProfitRate"`
	MaxDrawdownRate       float64 `json:"maxDrawdownRate"`
	AverageHoldingSeconds int64   `json:"averageHoldingSeconds"`
}

type LevelType string

const (
	LevelTypeSupport    LevelType = "support"
	LevelTypeResistance LevelType = "resistance"
)

type LevelStatus string

const (
	LevelStatusHolding           LevelStatus = "holding"
	LevelStatusRejected          LevelStatus = "rejected"
	LevelStatusSupportPierced    LevelStatus = "support_pierced"
	LevelStatusSupportBroken     LevelStatus = "support_broken"
	LevelStatusResistancePierced LevelStatus = "resistance_pierced"
	LevelStatusResistanceBroken  LevelStatus = "resistance_broken"
)

type PriceLevel struct {
	Type        LevelType        `json:"type"`
	Price       float64          `json:"marketCap"`
	Lower       float64          `json:"lowerMarketCap"`
	Upper       float64          `json:"upperMarketCap"`
	Touches     int              `json:"touches"`
	Volume      float64          `json:"volume"`
	Score       float64          `json:"score"`
	Status      LevelStatus      `json:"status"`
	FirstTime   time.Time        `json:"firstTime"`
	LastTime    time.Time        `json:"lastTime"`
	Calculation LevelCalculation `json:"calculation"`
	Breakout    *BreakoutSetup   `json:"breakout,omitempty"`
}

type LevelCalculation struct {
	PivotCount      int                `json:"pivotCount"`
	SupportVotes    int                `json:"supportVotes"`
	ResistanceVotes int                `json:"resistanceVotes"`
	Tolerance       float64            `json:"tolerance"`
	CurrentPrice    float64            `json:"currentMarketCap"`
	TypeReason      string             `json:"typeReason"`
	StatusReason    string             `json:"statusReason"`
	ScoreParts      LevelScoreParts    `json:"scoreParts"`
	Pivots          []LevelAnchorPoint `json:"pivots"`
	SampleTouches   []LevelAnchorPoint `json:"sampleTouches"`
}

type LevelScoreParts struct {
	Touch    float64 `json:"touch"`
	Volume   float64 `json:"volume"`
	Recency  float64 `json:"recency"`
	Distance float64 `json:"distance"`
}

type LevelAnchorPoint struct {
	Time   time.Time `json:"time"`
	Price  float64   `json:"marketCap"`
	Volume float64   `json:"volume"`
}

type BreakoutOutcome string

const (
	BreakoutOutcomePending    BreakoutOutcome = "pending"
	BreakoutOutcomeStopLoss   BreakoutOutcome = "stop_loss"
	BreakoutOutcomeTakeProfit BreakoutOutcome = "take_profit"
	BreakoutOutcomeTimeout    BreakoutOutcome = "timeout"
)

type BreakoutSetup struct {
	Triggered       bool                `json:"triggered"`
	FailedTouches   []LevelAnchorPoint  `json:"failedTouches"`
	FailedAttempts  []FailedAttemptZone `json:"failedAttempts,omitempty"`
	Consolidation   *ConsolidationZone  `json:"consolidation,omitempty"`
	BreakoutPoint   *LevelAnchorPoint   `json:"breakoutPoint,omitempty"`
	BuyPoint        *LevelAnchorPoint   `json:"buyPoint,omitempty"`
	ExitPoint       *LevelAnchorPoint   `json:"exitPoint,omitempty"`
	StopLoss        float64             `json:"stopLoss,omitempty"`
	TakeProfit      float64             `json:"takeProfit,omitempty"`
	Risk            float64             `json:"risk,omitempty"`
	HoldingBars     int                 `json:"holdingBars,omitempty"`
	ProfitRate      float64             `json:"profitRate,omitempty"`
	Outcome         BreakoutOutcome     `json:"outcome,omitempty"`
	BreakoutReason  string              `json:"breakoutReason,omitempty"`
	AttemptStrategy string              `json:"attemptStrategy,omitempty"`
}

type FailedAttemptZone struct {
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	HighPrice float64   `json:"highPrice"`
	LowPrice  float64   `json:"lowPrice"`
	BarCount  int       `json:"barCount"`
}

type ConsolidationZone struct {
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	HighPrice  float64   `json:"highPrice"`
	LowPrice   float64   `json:"lowPrice"`
	BarCount   int       `json:"barCount"`
	TouchCount int       `json:"touchCount"`
}
