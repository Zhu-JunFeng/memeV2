package model

import "time"

type BacktestSession struct {
	ID           string    `gorm:"primaryKey;size:36"`
	TokenAddress string    `gorm:"index;size:128;not null"`
	TokenSymbol  string    `gorm:"size:64"`
	Interval     string    `gorm:"size:32;not null"`
	StartTime    time.Time `gorm:"index;not null"`
	EndTime      time.Time `gorm:"index;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type BacktestTradePoint struct {
	ID               string    `gorm:"primaryKey;size:36"`
	SessionID        string    `gorm:"index;size:36;not null"`
	Side             string    `gorm:"size:16;not null"`
	PointTime        time.Time `gorm:"index;not null"`
	InputPrice       *float64
	Note             string `gorm:"size:512"`
	MatchedKlineTime *time.Time
	MatchedPrice     *float64
	CreatedAt        time.Time
}

type BacktestTradeResult struct {
	ID                   string    `gorm:"primaryKey;size:36"`
	SessionID            string    `gorm:"index;size:36;not null"`
	BuyPointID           string    `gorm:"size:36;not null"`
	SellPointID          string    `gorm:"size:36;not null"`
	BuyMatchedKlineTime  time.Time `gorm:"not null"`
	SellMatchedKlineTime time.Time `gorm:"not null"`
	BuyPrice             float64   `gorm:"not null"`
	SellPrice            float64   `gorm:"not null"`
	Profit               float64   `gorm:"not null"`
	ProfitRate           float64   `gorm:"not null"`
	HoldingSeconds       int64     `gorm:"not null"`
	Win                  bool      `gorm:"not null"`
	CreatedAt            time.Time
}

type BacktestMetricSnapshot struct {
	ID                    string  `gorm:"primaryKey;size:36"`
	SessionID             string  `gorm:"uniqueIndex;size:36;not null"`
	TradeCount            int     `gorm:"not null"`
	WinRate               float64 `gorm:"not null"`
	TotalProfitRate       float64 `gorm:"not null"`
	MaxDrawdownRate       float64 `gorm:"not null"`
	AverageHoldingSeconds int64   `gorm:"not null"`
	CreatedAt             time.Time
}
