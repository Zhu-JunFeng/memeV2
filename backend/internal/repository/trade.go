package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/model"
)

var ErrTradeOrderNotFound = errors.New("交易订单不存在")
var ErrTradePositionNotFound = errors.New("交易持仓不存在")

type TradeRepository struct {
	db *sql.DB
}

func NewTradeRepository(db *sql.DB) *TradeRepository {
	return &TradeRepository{db: db}
}

func (r *TradeRepository) EnsureAccount(ctx context.Context, account model.TradeAccount) (model.TradeAccount, error) {
	now := time.Now().UTC()
	if account.ID == "" {
		account.ID = uuid.NewString()
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO trade_accounts (id, name, wallet_address, status, buy_amount_usd, slippage_bps, priority_fee_lamports, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (name) DO UPDATE SET
			wallet_address = excluded.wallet_address,
			status = excluded.status,
			buy_amount_usd = excluded.buy_amount_usd,
			slippage_bps = excluded.slippage_bps,
			priority_fee_lamports = excluded.priority_fee_lamports,
			updated_at = excluded.updated_at`,
		account.ID, account.Name, account.WalletAddress, account.Status, account.BuyAmountUSD, account.SlippageBPS, account.PriorityFeeLamports, now, now,
	); err != nil {
		return model.TradeAccount{}, err
	}
	return r.GetAccountByName(ctx, account.Name)
}

func (r *TradeRepository) GetAccountByName(ctx context.Context, name string) (model.TradeAccount, error) {
	var item model.TradeAccount
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, name, wallet_address, status, buy_amount_usd, slippage_bps, priority_fee_lamports, created_at, updated_at
		FROM trade_accounts WHERE name = $1`, name,
	).Scan(&item.ID, &item.Name, &item.WalletAddress, &item.Status, &item.BuyAmountUSD, &item.SlippageBPS, &item.PriorityFeeLamports, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.TradeAccount{}, err
	}
	return item, nil
}

func (r *TradeRepository) ListAccounts(ctx context.Context) ([]model.TradeAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, wallet_address, status, buy_amount_usd, slippage_bps, priority_fee_lamports, created_at, updated_at
		FROM trade_accounts ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeAccount, 0)
	for rows.Next() {
		var item model.TradeAccount
		if err := rows.Scan(&item.ID, &item.Name, &item.WalletAddress, &item.Status, &item.BuyAmountUSD, &item.SlippageBPS, &item.PriorityFeeLamports, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) InsertSignalIfAbsent(ctx context.Context, signal model.TradeSignal) (model.TradeSignal, bool, error) {
	now := time.Now().UTC()
	if signal.ID == "" {
		signal.ID = uuid.NewString()
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO trade_signals (id, signal_id, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, raw_payload_json, consume_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (signal_id) DO NOTHING`,
		signal.ID, signal.SignalID, signal.SignalType, signal.StrategyCode, signal.TokenAddress, signal.Interval, signal.SignalTime.UTC(), signal.TriggerPrice, signal.TriggerMarketCap, signal.Reason, json.RawMessage(signal.RawPayloadJSON), signal.ConsumeStatus, now,
	)
	if err != nil {
		return model.TradeSignal{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.TradeSignal{}, false, err
	}
	stored, err := r.GetSignalByExternalID(ctx, signal.SignalID)
	if err != nil {
		return model.TradeSignal{}, false, err
	}
	return stored, affected == 1, nil
}

func (r *TradeRepository) UpdateSignalStatus(ctx context.Context, signalID string, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE trade_signals SET consume_status = $2 WHERE id = $1`, signalID, status)
	return err
}

func (r *TradeRepository) GetSignalByExternalID(ctx context.Context, externalID string) (model.TradeSignal, error) {
	return r.getSignal(ctx, `WHERE signal_id = $1`, externalID)
}

func (r *TradeRepository) GetSignalByID(ctx context.Context, id string) (model.TradeSignal, error) {
	return r.getSignal(ctx, `WHERE id = $1`, id)
}

func (r *TradeRepository) getSignal(ctx context.Context, where string, arg any) (model.TradeSignal, error) {
	var item model.TradeSignal
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, signal_id, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, raw_payload_json, consume_status, created_at
		FROM trade_signals `+where, arg,
	).Scan(&item.ID, &item.SignalID, &item.SignalType, &item.StrategyCode, &item.TokenAddress, &item.Interval, &item.SignalTime, &item.TriggerPrice, &item.TriggerMarketCap, &item.Reason, &item.RawPayloadJSON, &item.ConsumeStatus, &item.CreatedAt); err != nil {
		return model.TradeSignal{}, err
	}
	return item, nil
}

func (r *TradeRepository) ListSignals(ctx context.Context, limit int) ([]model.TradeSignal, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, signal_id, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, raw_payload_json, consume_status, created_at
		FROM trade_signals ORDER BY signal_time DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeSignal, 0)
	for rows.Next() {
		var item model.TradeSignal
		if err := rows.Scan(&item.ID, &item.SignalID, &item.SignalType, &item.StrategyCode, &item.TokenAddress, &item.Interval, &item.SignalTime, &item.TriggerPrice, &item.TriggerMarketCap, &item.Reason, &item.RawPayloadJSON, &item.ConsumeStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) GetOpenPosition(ctx context.Context, accountID string, tokenAddress string) (model.TradePosition, error) {
	var item model.TradePosition
	var closedAt sql.NullTime
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, closed_at, updated_at
		FROM trade_positions
		WHERE account_id = $1 AND token_address = $2 AND status = 'open'
		LIMIT 1`, accountID, tokenAddress,
	).Scan(&item.ID, &item.AccountID, &item.TokenAddress, &item.Status, &item.OpenOrderID, &item.CloseOrderID, &item.Quantity, &item.CostAmount, &item.AvgCostPrice, &item.LastPrice, &item.MarketValue, &item.RealizedPNL, &item.UnrealizedPNL, &item.MaxProfitRate, &item.MaxDrawdownAmount, &item.OpenedAt, &closedAt, &item.UpdatedAt); err != nil {
		return model.TradePosition{}, err
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	return item, nil
}

func (r *TradeRepository) CreateOrder(ctx context.Context, order model.TradeOrder) (model.TradeOrder, error) {
	now := time.Now().UTC()
	if order.ID == "" {
		order.ID = uuid.NewString()
	}
	order.CreatedAt = now
	order.UpdatedAt = now
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO trade_orders (id, account_id, signal_id, token_address, side, intent_amount_usd, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		order.ID, order.AccountID, order.SignalID, order.TokenAddress, order.Side, order.IntentAmountUSD, order.IntentTokenAmount, order.Status, json.RawMessage(order.JupiterRequestJSON), json.RawMessage(order.JupiterResponseJSON), order.SubmitTxHash, order.ConfirmedAt, order.FailReason, now, now,
	); err != nil {
		return model.TradeOrder{}, err
	}
	return order, nil
}

func (r *TradeRepository) UpdateOrderExecution(ctx context.Context, orderID string, status model.TradeOrderStatus, txHash string, requestJSON json.RawMessage, responseJSON json.RawMessage, failReason string, confirmedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = $2,
			submit_tx_hash = $3,
			jupiter_request_json = COALESCE($4, jupiter_request_json),
			jupiter_response_json = COALESCE($5, jupiter_response_json),
			fail_reason = $6,
			confirmed_at = $7,
			updated_at = $8
		WHERE id = $1`, orderID, status, txHash, nullableJSON(requestJSON), nullableJSON(responseJSON), failReason, confirmedAt, time.Now().UTC())
	return err
}

func (r *TradeRepository) AddOrderEvent(ctx context.Context, orderID string, eventType string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO trade_order_events (id, order_id, event_type, event_time, detail_json)
		VALUES ($1, $2, $3, $4, $5)`, uuid.NewString(), orderID, eventType, time.Now().UTC(), raw)
	return err
}

func (r *TradeRepository) SaveFilledBuy(ctx context.Context, order model.TradeOrder, fill model.TradeFill) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if fill.ID == "" {
		fill.ID = uuid.NewString()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trade_fills (id, order_id, tx_hash, side, token_address, filled_token_amount, filled_quote_amount, avg_price, fee_amount, fee_asset, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		fill.ID, fill.OrderID, fill.TxHash, fill.Side, fill.TokenAddress, fill.FilledTokenAmount, fill.FilledQuoteAmount, fill.AvgPrice, fill.FeeAmount, fill.FeeAsset, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = 'filled', submit_tx_hash = $2, confirmed_at = $3, fail_reason = '', updated_at = $4
		WHERE id = $1`, order.ID, fill.TxHash, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trade_positions (id, account_id, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, updated_at)
		VALUES ($1, $2, $3, 'open', $4, '', $5, $6, $7, $8, $9, 0, 0, 0, 0, $10, $11)`,
		uuid.NewString(), order.AccountID, order.TokenAddress, order.ID, fill.FilledTokenAmount, fill.FilledQuoteAmount+fill.FeeAmount, fill.AvgPrice, fill.AvgPrice, fill.FilledQuoteAmount, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TradeRepository) SaveFilledSell(ctx context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if fill.ID == "" {
		fill.ID = uuid.NewString()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trade_fills (id, order_id, tx_hash, side, token_address, filled_token_amount, filled_quote_amount, avg_price, fee_amount, fee_asset, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		fill.ID, fill.OrderID, fill.TxHash, fill.Side, fill.TokenAddress, fill.FilledTokenAmount, fill.FilledQuoteAmount, fill.AvgPrice, fill.FeeAmount, fill.FeeAsset, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	realized := fill.FilledQuoteAmount - position.CostAmount - fill.FeeAmount
	if _, err := tx.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = 'filled', submit_tx_hash = $2, confirmed_at = $3, fail_reason = '', updated_at = $4
		WHERE id = $1`, order.ID, fill.TxHash, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE trade_positions
		SET status = 'closed', close_order_id = $2, quantity = $3, last_price = $4, market_value = 0, realized_pnl = $5, unrealized_pnl = 0, closed_at = $6, updated_at = $7
		WHERE id = $1`, position.ID, order.ID, fill.FilledTokenAmount, fill.AvgPrice, realized, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TradeRepository) ListOrders(ctx context.Context, limit int) ([]model.TradeOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, signal_id, token_address, side, intent_amount_usd, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at
		FROM trade_orders ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeOrder, 0)
	for rows.Next() {
		item, err := scanTradeOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) GetOrder(ctx context.Context, id string) (model.TradeOrder, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, signal_id, token_address, side, intent_amount_usd, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at
		FROM trade_orders WHERE id = $1`, id)
	item, err := scanTradeOrder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.TradeOrder{}, ErrTradeOrderNotFound
		}
		return model.TradeOrder{}, err
	}
	return item, nil
}

func (r *TradeRepository) ListPositions(ctx context.Context, status string, limit int) ([]model.TradePosition, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, account_id, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, closed_at, updated_at
		FROM trade_positions`
	args := []any{}
	if status != "" {
		query += ` WHERE status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradePosition, 0)
	for rows.Next() {
		item, err := scanTradePosition(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) GetPosition(ctx context.Context, id string) (model.TradePosition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, closed_at, updated_at
		FROM trade_positions WHERE id = $1`, id)
	item, err := scanTradePosition(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.TradePosition{}, ErrTradePositionNotFound
		}
		return model.TradePosition{}, err
	}
	return item, nil
}

func (r *TradeRepository) UpdatePositionMark(ctx context.Context, positionID string, lastPrice float64, marketValue float64, unrealized float64, maxProfitRate float64, maxDrawdownAmount float64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE trade_positions
		SET last_price = $2, market_value = $3, unrealized_pnl = $4, max_profit_rate = GREATEST(max_profit_rate, $5), max_drawdown_amount = LEAST(max_drawdown_amount, $6), updated_at = $7
		WHERE id = $1`, positionID, lastPrice, marketValue, unrealized, maxProfitRate, maxDrawdownAmount, time.Now().UTC())
	return err
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTradeOrder(scanner rowScanner) (model.TradeOrder, error) {
	var item model.TradeOrder
	var confirmedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.AccountID, &item.SignalID, &item.TokenAddress, &item.Side, &item.IntentAmountUSD, &item.IntentTokenAmount, &item.Status, &item.JupiterRequestJSON, &item.JupiterResponseJSON, &item.SubmitTxHash, &confirmedAt, &item.FailReason, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.TradeOrder{}, err
	}
	if confirmedAt.Valid {
		item.ConfirmedAt = &confirmedAt.Time
	}
	return item, nil
}

func scanTradePosition(scanner rowScanner) (model.TradePosition, error) {
	var item model.TradePosition
	var closedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.AccountID, &item.TokenAddress, &item.Status, &item.OpenOrderID, &item.CloseOrderID, &item.Quantity, &item.CostAmount, &item.AvgCostPrice, &item.LastPrice, &item.MarketValue, &item.RealizedPNL, &item.UnrealizedPNL, &item.MaxProfitRate, &item.MaxDrawdownAmount, &item.OpenedAt, &closedAt, &item.UpdatedAt); err != nil {
		return model.TradePosition{}, err
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	return item, nil
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
