package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetOpenPositionBySignalIDScansBasicPositionColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	openedAt := time.Date(2026, 7, 14, 20, 18, 31, 0, time.UTC)
	updatedAt := openedAt.Add(time.Minute)
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "trade_mode", "token_address", "status",
		"open_order_id", "close_order_id", "quantity", "cost_amount",
		"avg_cost_price", "last_price", "market_value", "realized_pnl",
		"unrealized_pnl", "max_profit_rate", "max_drawdown_amount",
		"opened_at", "closed_at", "updated_at",
	}).AddRow(
		"position-1", "account-1", "live", "token-1", "open",
		"order-1", "", 3278.156139, 10.3002291,
		0.0030474448, 0.0031, 10.1, 0.0,
		-0.2, 0.0965, -2.05,
		openedAt, nil, updatedAt,
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE s.signal_id = $1 AND p.status = 'open'")).
		WithArgs("buy-signal-1").
		WillReturnRows(rows)

	item, err := NewTradeRepository(db).GetOpenPositionBySignalID(context.Background(), "buy-signal-1")
	if err != nil {
		t.Fatalf("get open position by signal id: %v", err)
	}
	if item.ID != "position-1" || item.AvgCostPrice != 0.0030474448 {
		t.Fatalf("unexpected position: %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetOpenPositionScansBasicPositionColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	openedAt := time.Date(2026, 7, 14, 20, 18, 31, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "account_id", "trade_mode", "token_address", "status",
		"open_order_id", "close_order_id", "quantity", "cost_amount",
		"avg_cost_price", "last_price", "market_value", "realized_pnl",
		"unrealized_pnl", "max_profit_rate", "max_drawdown_amount",
		"opened_at", "closed_at", "updated_at",
	}).AddRow(
		"position-1", "account-1", "live", "token-1", "open",
		"order-1", "", 3278.156139, 10.3002291,
		0.0030474448, 0.0031, 10.1, 0.0,
		-0.2, 0.0965, -2.05,
		openedAt, nil, openedAt.Add(time.Minute),
	)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE account_id = $1 AND token_address = $2 AND status = 'open'")).
		WithArgs("account-1", "token-1").
		WillReturnRows(rows)

	item, err := NewTradeRepository(db).GetOpenPosition(context.Background(), "account-1", "token-1")
	if err != nil {
		t.Fatalf("get open position: %v", err)
	}
	if item.ID != "position-1" || item.AvgCostPrice != 0.0030474448 {
		t.Fatalf("unexpected position: %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
