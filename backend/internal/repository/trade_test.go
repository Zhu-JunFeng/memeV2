package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"solana-meme-backtest/backend/internal/model"
)

func TestTradeStatusUpdatesRetryWhenDependencyIsMissing(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repo := NewTradeRepository(database)

	mock.ExpectExec("UPDATE trade_signals").WithArgs("signal-1", "executed").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UpdateSignalStatus(context.Background(), "signal-1", "executed"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected missing signal to retry, got %v", err)
	}
	mock.ExpectExec("SET status = CASE WHEN status = 'filled' THEN status ELSE \\$2 END").
		WithArgs("order-1", model.TradeOrderStatusSubmitted, "tx", nil, nil, "", nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UpdateOrderExecution(context.Background(), "order-1", model.TradeOrderStatusSubmitted, "tx", json.RawMessage(nil), json.RawMessage(nil), "", nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected missing order to retry, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAccountBuyAmountUSDReturnsUpdatedAccount(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "name", "wallet_address", "status", "buy_amount_usd", "buy_amount_sol", "slippage_bps", "priority_fee_lamports", "created_at", "updated_at"}).
		AddRow("account-1", "default", "wallet", "active", 12.5, 0.0, 200, int64(0), now, now)
	mock.ExpectQuery("UPDATE trade_accounts").WithArgs("account-1", 12.5, sqlmock.AnyArg()).WillReturnRows(rows)

	account, err := NewTradeRepository(database).UpdateAccountBuyAmountUSD(context.Background(), "account-1", 12.5)
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != "account-1" || account.BuyAmountUSD != 12.5 {
		t.Fatalf("unexpected account: %#v", account)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFilledSellUpdatesCARiskInSameTransaction(t *testing.T) {
	tests := []struct {
		name        string
		filledQuote float64
		fee         float64
		profitRate  float64
		loss        bool
	}{
		{name: "loss", filledQuote: 9, fee: 0.1, profitRate: -0.11, loss: true},
		{name: "non-loss resets streak", filledQuote: 11, fee: 0.1, profitRate: 0.09, loss: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()

			executedAt := time.Date(2026, 7, 17, 1, 30, 0, 0, time.UTC)
			position := model.TradePosition{ID: "position-1", CostAmount: 10}
			order := model.TradeOrder{ID: "order-1"}
			fill := model.TradeFill{
				ID: "fill-1", OrderID: order.ID, TradeMode: model.TradeModePaper,
				IsSimulated: true, TxHash: "paper-tx", Side: model.TradeSignalTypeSell,
				TokenAddress: "token-1", FilledTokenAmount: 100, FilledQuoteAmount: tt.filledQuote,
				AvgPrice: 0.1, FeeAmount: tt.fee, FeeAsset: "USD", ExecutedAt: executedAt,
				ProfitRate: tt.profitRate,
			}

			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO trade_fills").
				WithArgs("fill-1", "order-1", model.TradeModePaper, true, "paper-tx", model.TradeSignalTypeSell, "token-1", 100.0, tt.filledQuote, 0.1, tt.fee, "USD", executedAt, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE trade_orders").
				WithArgs("order-1", "paper-tx", executedAt, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 1))
			realized := tt.filledQuote - position.CostAmount - tt.fee
			mock.ExpectExec("UPDATE trade_positions").
				WithArgs("position-1", "order-1", 100.0, 0.1, realized, executedAt, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta("CASE WHEN $2 THEN $3::timestamptz + INTERVAL '1 hour' ELSE NULL END")).
				WithArgs("token-1", tt.loss, executedAt, model.TradeModePaper, tt.profitRate, anyTimeArgument{}).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			if err := NewTradeRepository(database).SaveFilledSell(context.Background(), position, order, fill); err != nil {
				t.Fatalf("save filled sell: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUpdatePositionMarkOnlyUpdatesOpenPosition(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	mock.ExpectExec(regexp.QuoteMeta("WHERE id = $1 AND status = 'open'")).
		WithArgs("position-1", 0.12, 18.5, -1.5, -0.075, -1.5, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := NewTradeRepository(database).UpdatePositionMark(context.Background(), "position-1", 0.12, 18.5, -1.5, -0.075, -1.5); err != nil {
		t.Fatalf("update position mark: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type anyTimeArgument struct{}

func (anyTimeArgument) Match(value driver.Value) bool {
	_, ok := value.(time.Time)
	return ok
}

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

func TestListDailyStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	end := start.AddDate(0, 0, 5)
	last := time.Date(2026, 7, 29, 9, 6, 56, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"stat_date", "trade_mode", "signal_count", "buy_signal_count", "sell_signal_count",
		"executed_signal_count", "skipped_signal_count", "rejected_signal_count",
		"order_count", "buy_order_count", "sell_order_count", "filled_order_count",
		"failed_order_count", "pending_order_count", "submitted_order_count",
		"opened_position_count", "closed_position_count", "win_count", "loss_count",
		"neutral_count", "realized_pnl", "average_pnl", "best_pnl", "worst_pnl",
		"last_activity_at",
	}).AddRow(
		"2026-07-29", "paper", 4, 2, 2,
		4, 0, 0,
		4, 2, 2, 4,
		0, 0, 0,
		2, 4, 3, 1,
		0, 1.25, 0.3125, 0.8, -0.2,
		last,
	)

	mock.ExpectQuery(regexp.QuoteMeta("WITH days AS")).
		WithArgs(string(model.TradeModePaper), start.UTC(), end.UTC()).
		WillReturnRows(rows)

	items, err := NewTradeRepository(db).ListDailyStats(context.Background(), model.TradeModePaper, start, end)
	if err != nil {
		t.Fatalf("list daily stats: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(items))
	}
	if items[0].Date != "2026-07-29" || items[0].TradeMode != model.TradeModePaper {
		t.Fatalf("unexpected identity fields: %#v", items[0])
	}
	if items[0].ClosedPositionCount != 4 || items[0].WinRate != 0.75 || items[0].RealizedPNL != 1.25 {
		t.Fatalf("unexpected pnl fields: %#v", items[0])
	}
	if items[0].LastActivityAt == nil || items[0].LastActivityAt.Location().String() != "Asia/Shanghai" {
		t.Fatalf("expected Beijing last activity, got %#v", items[0].LastActivityAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
