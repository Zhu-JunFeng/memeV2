package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/apptime"
	"solana-meme-backtest/backend/internal/model"
)

var ErrTradeOrderNotFound = errors.New("交易订单不存在")
var ErrTradePositionNotFound = errors.New("交易持仓不存在")

const (
	tradeModeSettingKey          = "trade.mode"
	tradeModeStartedAtSettingKey = "trade.mode.started_at"
)

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
		INSERT INTO trade_accounts (id, name, wallet_address, status, buy_amount_usd, buy_amount_sol, slippage_bps, priority_fee_lamports, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (name) DO UPDATE SET
			wallet_address = excluded.wallet_address,
			status = excluded.status,
			buy_amount_sol = excluded.buy_amount_sol,
			slippage_bps = excluded.slippage_bps,
			priority_fee_lamports = excluded.priority_fee_lamports,
			updated_at = excluded.updated_at`,
		account.ID, account.Name, account.WalletAddress, account.Status, account.BuyAmountUSD, account.BuyAmountSOL, account.SlippageBPS, account.PriorityFeeLamports, now, now,
	); err != nil {
		return model.TradeAccount{}, err
	}
	return r.GetAccountByName(ctx, account.Name)
}

func (r *TradeRepository) GetAccountByName(ctx context.Context, name string) (model.TradeAccount, error) {
	var item model.TradeAccount
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, name, wallet_address, status, buy_amount_usd, buy_amount_sol, slippage_bps, priority_fee_lamports, created_at, updated_at
		FROM trade_accounts WHERE name = $1`, name,
	).Scan(&item.ID, &item.Name, &item.WalletAddress, &item.Status, &item.BuyAmountUSD, &item.BuyAmountSOL, &item.SlippageBPS, &item.PriorityFeeLamports, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.TradeAccount{}, err
	}
	return item, nil
}

func (r *TradeRepository) ListAccounts(ctx context.Context) ([]model.TradeAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, wallet_address, status, buy_amount_usd, buy_amount_sol, slippage_bps, priority_fee_lamports, created_at, updated_at
		FROM trade_accounts ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeAccount, 0)
	for rows.Next() {
		var item model.TradeAccount
		if err := rows.Scan(&item.ID, &item.Name, &item.WalletAddress, &item.Status, &item.BuyAmountUSD, &item.BuyAmountSOL, &item.SlippageBPS, &item.PriorityFeeLamports, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) UpdateAccountBuyAmountUSD(ctx context.Context, accountID string, buyAmountUSD float64) (model.TradeAccount, error) {
	var item model.TradeAccount
	err := r.db.QueryRowContext(ctx, `
		UPDATE trade_accounts
		SET buy_amount_usd = $2, updated_at = $3
		WHERE id = $1
		RETURNING id, name, wallet_address, status, buy_amount_usd, buy_amount_sol, slippage_bps, priority_fee_lamports, created_at, updated_at`,
		accountID, buyAmountUSD, time.Now().UTC(),
	).Scan(&item.ID, &item.Name, &item.WalletAddress, &item.Status, &item.BuyAmountUSD, &item.BuyAmountSOL, &item.SlippageBPS, &item.PriorityFeeLamports, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *TradeRepository) GetTradeModeState(ctx context.Context) (model.TradeMode, time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT setting_key, setting_value
		FROM system_runtime_settings
		WHERE setting_key IN ($1, $2)`, tradeModeSettingKey, tradeModeStartedAtSettingKey)
	if err != nil {
		return "", time.Time{}, err
	}
	defer rows.Close()
	var mode model.TradeMode
	var startedAt time.Time
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return "", time.Time{}, err
		}
		switch key {
		case tradeModeSettingKey:
			mode = model.TradeMode(strings.TrimSpace(value))
		case tradeModeStartedAtSettingKey:
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
			if err != nil {
				return "", time.Time{}, fmt.Errorf("解析交易模式开始时间失败: %w", err)
			}
			startedAt = parsed.UTC()
		}
	}
	return mode, startedAt, rows.Err()
}

func (r *TradeRepository) SetTradeModeState(ctx context.Context, mode model.TradeMode, startedAt time.Time) error {
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{
		tradeModeSettingKey:          string(mode),
		tradeModeStartedAtSettingKey: startedAt.UTC().Format(time.RFC3339Nano),
	} {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO system_runtime_settings (setting_key, setting_value, created_at, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (setting_key) DO UPDATE SET
				setting_value = excluded.setting_value,
				updated_at = excluded.updated_at`, key, value, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *TradeRepository) GetTradeModePeriodSummary(ctx context.Context, accountID string, mode model.TradeMode, startedAt time.Time, endedAt time.Time) (model.TradeModePeriodSummary, error) {
	summary := model.TradeModePeriodSummary{TradeMode: mode, StartedAt: startedAt.UTC(), EndedAt: endedAt.UTC()}
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT COUNT(*) FROM trade_fills f JOIN trade_orders o ON o.id = f.order_id WHERE o.account_id = $1 AND f.trade_mode = $2 AND f.side = 'buy' AND f.executed_at >= $3 AND f.executed_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_fills f JOIN trade_orders o ON o.id = f.order_id WHERE o.account_id = $1 AND f.trade_mode = $2 AND f.side = 'sell' AND f.executed_at >= $3 AND f.executed_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_orders WHERE account_id = $1 AND trade_mode = $2 AND status = 'failed' AND created_at >= $3 AND created_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_positions WHERE account_id = $1 AND trade_mode = $2 AND status = 'closed' AND closed_at >= $3 AND closed_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_positions WHERE account_id = $1 AND trade_mode = $2 AND status = 'closed' AND realized_pnl > 0 AND closed_at >= $3 AND closed_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_positions WHERE account_id = $1 AND trade_mode = $2 AND status = 'closed' AND realized_pnl < 0 AND closed_at >= $3 AND closed_at < $4), 0),
			COALESCE((SELECT SUM(realized_pnl) FROM trade_positions WHERE account_id = $1 AND trade_mode = $2 AND status = 'closed' AND closed_at >= $3 AND closed_at < $4), 0),
			COALESCE((SELECT COUNT(*) FROM trade_positions WHERE account_id = $1 AND trade_mode = $2 AND status = 'open'), 0)`,
		accountID, mode, startedAt.UTC(), endedAt.UTC(),
	).Scan(
		&summary.BuyCount,
		&summary.SellCount,
		&summary.FailedOrderCount,
		&summary.ClosedPositionCount,
		&summary.WinCount,
		&summary.LossCount,
		&summary.RealizedPNL,
		&summary.OpenPositionCount,
	)
	return summary, err
}

func (r *TradeRepository) InsertSignalIfAbsent(ctx context.Context, signal model.TradeSignal) (model.TradeSignal, bool, error) {
	now := time.Now().UTC()
	if signal.ID == "" {
		signal.ID = uuid.NewString()
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO trade_signals (id, signal_id, trade_mode, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, raw_payload_json, consume_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (signal_id) DO NOTHING`,
		signal.ID, signal.SignalID, signal.TradeMode, signal.SignalType, signal.StrategyCode, signal.TokenAddress, signal.Interval, signal.SignalTime.UTC(), signal.TriggerPrice, signal.TriggerMarketCap, signal.Reason, json.RawMessage(signal.RawPayloadJSON), signal.ConsumeStatus, now,
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
	result, err := r.db.ExecContext(ctx, `UPDATE trade_signals SET consume_status = $2 WHERE id = $1`, signalID, status)
	return requireAffectedRow(result, err)
}

func (r *TradeRepository) UpdateSignalStatusAndReason(ctx context.Context, signalID string, status string, reason string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE trade_signals
		SET consume_status = $2,
			reason = $3
		WHERE id = $1`, signalID, status, reason)
	return requireAffectedRow(result, err)
}

func (r *TradeRepository) GetSignalByExternalID(ctx context.Context, externalID string) (model.TradeSignal, error) {
	return r.getSignal(ctx, `WHERE signal_id = $1`, externalID)
}

func (r *TradeRepository) GetSignalByID(ctx context.Context, id string) (model.TradeSignal, error) {
	return r.getSignal(ctx, `WHERE id = $1`, id)
}

func (r *TradeRepository) GetSignalBySignalID(ctx context.Context, signalID string) (model.TradeSignal, error) {
	return r.getSignal(ctx, `WHERE signal_id = $1`, signalID)
}

func (r *TradeRepository) getSignal(ctx context.Context, where string, arg any) (model.TradeSignal, error) {
	var item model.TradeSignal
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, signal_id, trade_mode, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, raw_payload_json, consume_status, created_at
		FROM trade_signals `+where, arg,
	).Scan(&item.ID, &item.SignalID, &item.TradeMode, &item.SignalType, &item.StrategyCode, &item.TokenAddress, &item.Interval, &item.SignalTime, &item.TriggerPrice, &item.TriggerMarketCap, &item.Reason, &item.RawPayloadJSON, &item.ConsumeStatus, &item.CreatedAt); err != nil {
		return model.TradeSignal{}, err
	}
	return item, nil
}

func (r *TradeRepository) ListSignals(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeSignal, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, signal_id, trade_mode, signal_type, strategy_code, token_address, "interval", signal_time, trigger_price, trigger_market_cap, reason, '{}'::jsonb AS raw_payload_json, consume_status, created_at
		FROM trade_signals`
	args := []any{}
	if tradeMode != "" {
		query += ` WHERE trade_mode = $1`
		args = append(args, tradeMode)
	}
	query += ` ORDER BY signal_time DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeSignal, 0)
	for rows.Next() {
		var item model.TradeSignal
		if err := rows.Scan(&item.ID, &item.SignalID, &item.TradeMode, &item.SignalType, &item.StrategyCode, &item.TokenAddress, &item.Interval, &item.SignalTime, &item.TriggerPrice, &item.TriggerMarketCap, &item.Reason, &item.RawPayloadJSON, &item.ConsumeStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) ListTradeSummaries(ctx context.Context) ([]model.TradeSummaryItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH modes AS (
			SELECT ''::varchar(16) AS trade_mode
			UNION ALL SELECT 'paper'::varchar(16)
			UNION ALL SELECT 'live'::varchar(16)
		),
		aggregated AS (
			SELECT
				''::varchar(16) AS trade_mode,
				COALESCE(SUM(realized_pnl + unrealized_pnl), 0) AS total_pnl,
				COALESCE(SUM(realized_pnl), 0) AS realized_pnl,
				COALESCE(SUM(CASE WHEN status = 'open' THEN unrealized_pnl ELSE 0 END), 0) AS unrealized_pnl,
				COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) AS trade_count,
				COALESCE(SUM(CASE WHEN status = 'closed' AND realized_pnl > 0 THEN 1 ELSE 0 END), 0) AS win_count,
				COALESCE(SUM(CASE WHEN status = 'closed' AND realized_pnl < 0 THEN 1 ELSE 0 END), 0) AS loss_count,
				COALESCE(SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END), 0) AS open_position_count,
				COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) AS closed_position_count,
				COALESCE(MIN(max_drawdown_amount), 0) AS max_drawdown_amount,
				MAX(updated_at) AS updated_at
			FROM trade_positions
			UNION ALL
			SELECT
				trade_mode,
				COALESCE(SUM(realized_pnl + unrealized_pnl), 0) AS total_pnl,
				COALESCE(SUM(realized_pnl), 0) AS realized_pnl,
				COALESCE(SUM(CASE WHEN status = 'open' THEN unrealized_pnl ELSE 0 END), 0) AS unrealized_pnl,
				COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) AS trade_count,
				COALESCE(SUM(CASE WHEN status = 'closed' AND realized_pnl > 0 THEN 1 ELSE 0 END), 0) AS win_count,
				COALESCE(SUM(CASE WHEN status = 'closed' AND realized_pnl < 0 THEN 1 ELSE 0 END), 0) AS loss_count,
				COALESCE(SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END), 0) AS open_position_count,
				COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) AS closed_position_count,
				COALESCE(MIN(max_drawdown_amount), 0) AS max_drawdown_amount,
				MAX(updated_at) AS updated_at
			FROM trade_positions
			GROUP BY trade_mode
		)
		SELECT
			modes.trade_mode,
			COALESCE(aggregated.total_pnl, 0),
			COALESCE(aggregated.realized_pnl, 0),
			COALESCE(aggregated.unrealized_pnl, 0),
			COALESCE(aggregated.trade_count, 0),
			COALESCE(aggregated.win_count, 0),
			COALESCE(aggregated.loss_count, 0),
			COALESCE(aggregated.open_position_count, 0),
			COALESCE(aggregated.closed_position_count, 0),
			COALESCE(aggregated.max_drawdown_amount, 0),
			aggregated.updated_at
		FROM modes
		LEFT JOIN aggregated ON aggregated.trade_mode = modes.trade_mode
		ORDER BY CASE modes.trade_mode WHEN '' THEN 0 WHEN 'paper' THEN 1 ELSE 2 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeSummaryItem, 0, 3)
	for rows.Next() {
		var item model.TradeSummaryItem
		var updatedAt sql.NullTime
		if err := rows.Scan(
			&item.TradeMode,
			&item.TotalPNL,
			&item.RealizedPNL,
			&item.UnrealizedPNL,
			&item.TradeCount,
			&item.WinCount,
			&item.LossCount,
			&item.OpenPositionCount,
			&item.ClosedPositionCount,
			&item.MaxDrawdownAmount,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if item.TradeMode == "" {
			item.TradeMode = model.TradeMode("all")
		}
		if item.TradeCount > 0 {
			item.WinRate = float64(item.WinCount) / float64(item.TradeCount)
		}
		if updatedAt.Valid {
			value := updatedAt.Time
			item.UpdatedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) ListDailyStats(ctx context.Context, tradeMode model.TradeMode, startTime time.Time, endTime time.Time) ([]model.TradeDailyStatsItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH days AS (
			SELECT generate_series(
				date(timezone('Asia/Shanghai', $2::timestamptz)),
				date(timezone('Asia/Shanghai', $3::timestamptz - INTERVAL '1 microsecond')),
				INTERVAL '1 day'
			)::date AS stat_day
		),
		signals AS (
			SELECT
				date(timezone('Asia/Shanghai', signal_time)) AS stat_day,
				COUNT(*) AS signal_count,
				COUNT(*) FILTER (WHERE signal_type = 'buy') AS buy_signal_count,
				COUNT(*) FILTER (WHERE signal_type = 'sell') AS sell_signal_count,
				COUNT(*) FILTER (WHERE consume_status = 'executed') AS executed_signal_count,
				COUNT(*) FILTER (WHERE consume_status = 'skipped') AS skipped_signal_count,
				COUNT(*) FILTER (WHERE consume_status = 'rejected') AS rejected_signal_count,
				MAX(signal_time) AS last_activity_at
			FROM trade_signals
			WHERE ($1 = '' OR trade_mode = $1)
				AND signal_time >= $2
				AND signal_time < $3
			GROUP BY 1
		),
		orders AS (
			SELECT
				date(timezone('Asia/Shanghai', created_at)) AS stat_day,
				COUNT(*) AS order_count,
				COUNT(*) FILTER (WHERE side = 'buy') AS buy_order_count,
				COUNT(*) FILTER (WHERE side = 'sell') AS sell_order_count,
				COUNT(*) FILTER (WHERE status = 'filled') AS filled_order_count,
				COUNT(*) FILTER (WHERE status = 'failed') AS failed_order_count,
				COUNT(*) FILTER (WHERE status = 'pending') AS pending_order_count,
				COUNT(*) FILTER (WHERE status = 'submitted') AS submitted_order_count,
				MAX(created_at) AS last_activity_at
			FROM trade_orders
			WHERE ($1 = '' OR trade_mode = $1)
				AND created_at >= $2
				AND created_at < $3
			GROUP BY 1
		),
		opened_positions AS (
			SELECT
				date(timezone('Asia/Shanghai', opened_at)) AS stat_day,
				COUNT(*) AS opened_position_count,
				MAX(opened_at) AS last_activity_at
			FROM trade_positions
			WHERE ($1 = '' OR trade_mode = $1)
				AND opened_at >= $2
				AND opened_at < $3
			GROUP BY 1
		),
		closed_positions AS (
			SELECT
				date(timezone('Asia/Shanghai', closed_at)) AS stat_day,
				COUNT(*) AS closed_position_count,
				COUNT(*) FILTER (WHERE realized_pnl > 0) AS win_count,
				COUNT(*) FILTER (WHERE realized_pnl < 0) AS loss_count,
				COUNT(*) FILTER (WHERE realized_pnl = 0) AS neutral_count,
				COALESCE(SUM(realized_pnl), 0) AS realized_pnl,
				COALESCE(AVG(realized_pnl), 0) AS average_pnl,
				COALESCE(MAX(realized_pnl), 0) AS best_pnl,
				COALESCE(MIN(realized_pnl), 0) AS worst_pnl,
				MAX(closed_at) AS last_activity_at
			FROM trade_positions
			WHERE ($1 = '' OR trade_mode = $1)
				AND status = 'closed'
				AND closed_at >= $2
				AND closed_at < $3
			GROUP BY 1
		)
		SELECT
			to_char(days.stat_day, 'YYYY-MM-DD') AS stat_date,
			CASE WHEN $1 = '' THEN 'all' ELSE $1 END AS trade_mode,
			COALESCE(signals.signal_count, 0),
			COALESCE(signals.buy_signal_count, 0),
			COALESCE(signals.sell_signal_count, 0),
			COALESCE(signals.executed_signal_count, 0),
			COALESCE(signals.skipped_signal_count, 0),
			COALESCE(signals.rejected_signal_count, 0),
			COALESCE(orders.order_count, 0),
			COALESCE(orders.buy_order_count, 0),
			COALESCE(orders.sell_order_count, 0),
			COALESCE(orders.filled_order_count, 0),
			COALESCE(orders.failed_order_count, 0),
			COALESCE(orders.pending_order_count, 0),
			COALESCE(orders.submitted_order_count, 0),
			COALESCE(opened_positions.opened_position_count, 0),
			COALESCE(closed_positions.closed_position_count, 0),
			COALESCE(closed_positions.win_count, 0),
			COALESCE(closed_positions.loss_count, 0),
			COALESCE(closed_positions.neutral_count, 0),
			COALESCE(closed_positions.realized_pnl, 0),
			COALESCE(closed_positions.average_pnl, 0),
			COALESCE(closed_positions.best_pnl, 0),
			COALESCE(closed_positions.worst_pnl, 0),
			(
				SELECT MAX(value)
				FROM (VALUES
					(signals.last_activity_at),
					(orders.last_activity_at),
					(opened_positions.last_activity_at),
					(closed_positions.last_activity_at)
				) AS activity(value)
			) AS last_activity_at
		FROM days
		LEFT JOIN signals ON signals.stat_day = days.stat_day
		LEFT JOIN orders ON orders.stat_day = days.stat_day
		LEFT JOIN opened_positions ON opened_positions.stat_day = days.stat_day
		LEFT JOIN closed_positions ON closed_positions.stat_day = days.stat_day
		ORDER BY days.stat_day DESC`, string(tradeMode), startTime.UTC(), endTime.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TradeDailyStatsItem, 0)
	for rows.Next() {
		var item model.TradeDailyStatsItem
		var lastActivityAt sql.NullTime
		if err := rows.Scan(
			&item.Date,
			&item.TradeMode,
			&item.SignalCount,
			&item.BuySignalCount,
			&item.SellSignalCount,
			&item.ExecutedSignalCount,
			&item.SkippedSignalCount,
			&item.RejectedSignalCount,
			&item.OrderCount,
			&item.BuyOrderCount,
			&item.SellOrderCount,
			&item.FilledOrderCount,
			&item.FailedOrderCount,
			&item.PendingOrderCount,
			&item.SubmittedOrderCount,
			&item.OpenedPositionCount,
			&item.ClosedPositionCount,
			&item.WinCount,
			&item.LossCount,
			&item.NeutralCount,
			&item.RealizedPNL,
			&item.AveragePNL,
			&item.BestPNL,
			&item.WorstPNL,
			&lastActivityAt,
		); err != nil {
			return nil, err
		}
		if item.ClosedPositionCount > 0 {
			item.WinRate = float64(item.WinCount) / float64(item.ClosedPositionCount)
		}
		if lastActivityAt.Valid {
			value := apptime.InBeijing(lastActivityAt.Time)
			item.LastActivityAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TradeRepository) GetOpenPosition(ctx context.Context, accountID string, tokenAddress string) (model.TradePosition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, trade_mode, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, closed_at, updated_at
		FROM trade_positions
		WHERE account_id = $1 AND token_address = $2 AND status = 'open'
		LIMIT 1`, accountID, tokenAddress)
	return scanBasicTradePosition(row)
}

func (r *TradeRepository) GetOpenPositionBySignalID(ctx context.Context, signalID string) (model.TradePosition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT p.id, p.account_id, p.trade_mode, p.token_address, p.status, p.open_order_id, p.close_order_id, p.quantity, p.cost_amount, p.avg_cost_price, p.last_price, p.market_value, p.realized_pnl, p.unrealized_pnl, p.max_profit_rate, p.max_drawdown_amount, p.opened_at, p.closed_at, p.updated_at
		FROM trade_positions p
		JOIN trade_orders o ON o.id = p.open_order_id
		JOIN trade_signals s ON s.id = o.signal_id
		WHERE s.signal_id = $1 AND p.status = 'open'
		ORDER BY p.opened_at DESC
		LIMIT 1`, signalID)
	return scanBasicTradePosition(row)
}

func (r *TradeRepository) CreateOrder(ctx context.Context, order model.TradeOrder) (model.TradeOrder, error) {
	now := time.Now().UTC()
	if order.ID == "" {
		order.ID = uuid.NewString()
	}
	order.CreatedAt = now
	order.UpdatedAt = now
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO trade_orders (id, account_id, signal_id, trade_mode, execution_channel, token_address, side, intent_amount_usd, intent_amount_sol, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (id) DO NOTHING`,
		order.ID, order.AccountID, order.SignalID, order.TradeMode, order.ExecutionChannel, order.TokenAddress, order.Side, order.IntentAmountUSD, order.IntentAmountSOL, order.IntentTokenAmount, order.Status, json.RawMessage(order.JupiterRequestJSON), json.RawMessage(order.JupiterResponseJSON), order.SubmitTxHash, order.ConfirmedAt, order.FailReason, now, now,
	); err != nil {
		return model.TradeOrder{}, err
	}
	return order, nil
}

func (r *TradeRepository) UpdateOrderExecution(ctx context.Context, orderID string, status model.TradeOrderStatus, txHash string, requestJSON json.RawMessage, responseJSON json.RawMessage, failReason string, confirmedAt *time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = CASE WHEN status = 'filled' THEN status ELSE $2 END,
			submit_tx_hash = $3,
			jupiter_request_json = COALESCE($4, jupiter_request_json),
			jupiter_response_json = COALESCE($5, jupiter_response_json),
			fail_reason = $6,
			confirmed_at = $7,
			updated_at = $8
		WHERE id = $1`, orderID, status, txHash, nullableJSON(requestJSON), nullableJSON(responseJSON), failReason, confirmedAt, time.Now().UTC())
	return requireAffectedRow(result, err)
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

func (r *TradeRepository) SaveFilledBuy(ctx context.Context, position model.TradePosition, order model.TradeOrder, fill model.TradeFill) error {
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
		INSERT INTO trade_fills (id, order_id, trade_mode, is_simulated, tx_hash, side, token_address, filled_token_amount, filled_quote_amount, avg_price, fee_amount, fee_asset, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		fill.ID, fill.OrderID, fill.TradeMode, fill.IsSimulated, fill.TxHash, fill.Side, fill.TokenAddress, fill.FilledTokenAmount, fill.FilledQuoteAmount, fill.AvgPrice, fill.FeeAmount, fill.FeeAsset, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = 'filled', submit_tx_hash = $2, confirmed_at = $3, fail_reason = '', updated_at = $4
		WHERE id = $1`, order.ID, fill.TxHash, fill.ExecutedAt.UTC(), now,
	)
	if err := requireAffectedRow(result, err); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trade_positions (id, account_id, trade_mode, token_address, status, open_order_id, close_order_id, quantity, cost_amount, avg_cost_price, last_price, market_value, realized_pnl, unrealized_pnl, max_profit_rate, max_drawdown_amount, opened_at, updated_at)
		VALUES ($1, $2, $3, $4, 'open', $5, '', $6, $7, $8, $9, $10, 0, 0, 0, 0, $11, $12)`,
		position.ID, order.AccountID, order.TradeMode, order.TokenAddress, order.ID, fill.FilledTokenAmount, fill.FilledQuoteAmount+fill.FeeAmount, fill.AvgPrice, fill.AvgPrice, fill.FilledQuoteAmount, fill.ExecutedAt.UTC(), now,
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
		INSERT INTO trade_fills (id, order_id, trade_mode, is_simulated, tx_hash, side, token_address, filled_token_amount, filled_quote_amount, avg_price, fee_amount, fee_asset, executed_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		fill.ID, fill.OrderID, fill.TradeMode, fill.IsSimulated, fill.TxHash, fill.Side, fill.TokenAddress, fill.FilledTokenAmount, fill.FilledQuoteAmount, fill.AvgPrice, fill.FeeAmount, fill.FeeAsset, fill.ExecutedAt.UTC(), now,
	); err != nil {
		return err
	}
	realized := fill.FilledQuoteAmount - position.CostAmount - fill.FeeAmount
	result, err := tx.ExecContext(ctx, `
		UPDATE trade_orders
		SET status = 'filled', submit_tx_hash = $2, confirmed_at = $3, fail_reason = '', updated_at = $4
		WHERE id = $1`, order.ID, fill.TxHash, fill.ExecutedAt.UTC(), now,
	)
	if err := requireAffectedRow(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE trade_positions
		SET status = 'closed', close_order_id = $2, quantity = $3, last_price = $4, market_value = 0, realized_pnl = $5, unrealized_pnl = 0, closed_at = $6, updated_at = $7
		WHERE id = $1`, position.ID, order.ID, fill.FilledTokenAmount, fill.AvgPrice, realized, fill.ExecutedAt.UTC(), now,
	)
	if err := requireAffectedRow(result, err); err != nil {
		return err
	}
	loss := realized < 0
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ca_blacklist (
			token_address, consecutive_loss_count, cooldown_until, is_blacklisted,
			blacklist_reason, blacklist_source, blacklisted_at, last_trade_mode,
			last_profit_rate, created_at, updated_at
		)
		VALUES (
			$1, CASE WHEN $2 THEN 1 ELSE 0 END,
			CASE WHEN $2 THEN $3::timestamptz + INTERVAL '1 hour' ELSE NULL END,
			false, '', '', NULL, $4, $5, $6, $6
		)
		ON CONFLICT (token_address) DO UPDATE SET
			consecutive_loss_count = CASE WHEN $2 THEN ca_blacklist.consecutive_loss_count + 1 ELSE 0 END,
			cooldown_until = CASE WHEN $2 THEN $3::timestamptz + INTERVAL '1 hour' ELSE NULL END,
			is_blacklisted = ca_blacklist.is_blacklisted OR ($2 AND ca_blacklist.consecutive_loss_count + 1 >= 3),
			blacklist_reason = CASE
				WHEN ca_blacklist.is_blacklisted THEN ca_blacklist.blacklist_reason
				WHEN $2 AND ca_blacklist.consecutive_loss_count + 1 >= 3 THEN '连续3次卖出亏损'
				ELSE ''
			END,
			blacklist_source = CASE
				WHEN ca_blacklist.is_blacklisted THEN ca_blacklist.blacklist_source
				WHEN $2 AND ca_blacklist.consecutive_loss_count + 1 >= 3 THEN 'auto'
				ELSE ''
			END,
			blacklisted_at = CASE
				WHEN ca_blacklist.is_blacklisted THEN ca_blacklist.blacklisted_at
				WHEN $2 AND ca_blacklist.consecutive_loss_count + 1 >= 3 THEN $3
				ELSE NULL
			END,
			last_trade_mode = excluded.last_trade_mode,
			last_profit_rate = excluded.last_profit_rate,
			updated_at = excluded.updated_at`,
		fill.TokenAddress, loss, fill.ExecutedAt.UTC(), fill.TradeMode, fill.ProfitRate, now,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TradeRepository) GetCABlacklistState(ctx context.Context, tokenAddress string) (model.CABlacklistState, error) {
	var item model.CABlacklistState
	err := r.db.QueryRowContext(ctx, `
		SELECT token_address, consecutive_loss_count, cooldown_until, is_blacklisted,
			blacklist_reason, blacklist_source, blacklisted_at, last_trade_mode,
			last_profit_rate, created_at, updated_at
		FROM ca_blacklist
		WHERE token_address = $1`, strings.TrimSpace(tokenAddress),
	).Scan(
		&item.TokenAddress, &item.ConsecutiveLossCount, &item.CooldownUntil, &item.IsBlacklisted,
		&item.BlacklistReason, &item.BlacklistSource, &item.BlacklistedAt, &item.LastTradeMode,
		&item.LastProfitRate, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CABlacklistState{TokenAddress: strings.TrimSpace(tokenAddress)}, nil
	}
	return item, err
}

func (r *TradeRepository) BlacklistCA(ctx context.Context, tokenAddress string, reason string, source string) (model.CABlacklistState, error) {
	now := time.Now().UTC()
	tokenAddress = strings.TrimSpace(tokenAddress)
	reason = strings.TrimSpace(reason)
	source = strings.TrimSpace(source)
	if reason == "" {
		reason = "手动拉黑"
	}
	if source == "" {
		source = "manual"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ca_blacklist (
			token_address, consecutive_loss_count, cooldown_until, is_blacklisted,
			blacklist_reason, blacklist_source, blacklisted_at, last_trade_mode,
			last_profit_rate, created_at, updated_at
		)
		VALUES ($1, 0, NULL, true, $2, $3, $4, '', 0, $4, $4)
		ON CONFLICT (token_address) DO UPDATE SET
			is_blacklisted = true,
			blacklist_reason = excluded.blacklist_reason,
			blacklist_source = excluded.blacklist_source,
			blacklisted_at = excluded.blacklisted_at,
			updated_at = excluded.updated_at`, tokenAddress, reason, source, now)
	if err != nil {
		return model.CABlacklistState{}, err
	}
	return r.GetCABlacklistState(ctx, tokenAddress)
}

func (r *TradeRepository) ListOrders(ctx context.Context, tradeMode model.TradeMode, limit int) ([]model.TradeOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, account_id, signal_id, trade_mode, execution_channel, token_address, side, intent_amount_usd, intent_amount_sol, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at
		FROM trade_orders`
	args := []any{}
	if tradeMode != "" {
		query += ` WHERE trade_mode = $1`
		args = append(args, tradeMode)
	}
	query += ` ORDER BY created_at DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
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
		SELECT id, account_id, signal_id, trade_mode, execution_channel, token_address, side, intent_amount_usd, intent_amount_sol, intent_token_amount, status, jupiter_request_json, jupiter_response_json, submit_tx_hash, confirmed_at, fail_reason, created_at, updated_at
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

func (r *TradeRepository) ListPositions(ctx context.Context, status string, tradeMode model.TradeMode, limit int) ([]model.TradePosition, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT
			p.id, p.account_id, p.trade_mode, p.token_address, p.status, p.open_order_id, p.close_order_id,
			open_order.signal_id, close_order.signal_id,
			open_signal.trigger_market_cap, close_signal.trigger_market_cap, open_fill.avg_price, close_fill.avg_price, p.quantity, p.cost_amount, p.avg_cost_price, p.last_price, p.market_value, p.realized_pnl,
			p.unrealized_pnl, p.max_profit_rate, p.max_drawdown_amount, p.opened_at, p.closed_at, p.updated_at,
			open_signal.signal_time, close_signal.signal_time, close_signal.reason,
			COALESCE(NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,token}', ''), NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,symbol}', '')),
			NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,publishedAt}', '')::bigint
		FROM trade_positions p
		LEFT JOIN trade_orders open_order ON open_order.id = p.open_order_id
		LEFT JOIN trade_signals open_signal ON open_signal.id = open_order.signal_id
		LEFT JOIN trade_fills open_fill ON open_fill.order_id = open_order.id
		LEFT JOIN trade_orders close_order ON close_order.id = p.close_order_id
		LEFT JOIN trade_signals close_signal ON close_signal.id = close_order.signal_id
		LEFT JOIN trade_fills close_fill ON close_fill.order_id = close_order.id`
	args := make([]any, 0, 3)
	clauses := make([]string, 0, 2)
	if strings.TrimSpace(status) != "" {
		args = append(args, status)
		clauses = append(clauses, `p.status = $`+itoa(len(args)))
	}
	if tradeMode != "" {
		args = append(args, tradeMode)
		clauses = append(clauses, `p.trade_mode = $`+itoa(len(args)))
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	query += ` ORDER BY p.updated_at DESC LIMIT $` + itoa(len(args)+1)
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
		SELECT
			p.id, p.account_id, p.trade_mode, p.token_address, p.status, p.open_order_id, p.close_order_id,
			open_order.signal_id, close_order.signal_id,
			open_signal.trigger_market_cap, close_signal.trigger_market_cap, open_fill.avg_price, close_fill.avg_price, p.quantity, p.cost_amount, p.avg_cost_price, p.last_price, p.market_value, p.realized_pnl,
			p.unrealized_pnl, p.max_profit_rate, p.max_drawdown_amount, p.opened_at, p.closed_at, p.updated_at,
			open_signal.signal_time, close_signal.signal_time, close_signal.reason,
			COALESCE(NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,token}', ''), NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,symbol}', '')),
			NULLIF(open_signal.raw_payload_json #>> '{metadata,upstream,publishedAt}', '')::bigint
		FROM trade_positions p
		LEFT JOIN trade_orders open_order ON open_order.id = p.open_order_id
		LEFT JOIN trade_signals open_signal ON open_signal.id = open_order.signal_id
		LEFT JOIN trade_fills open_fill ON open_fill.order_id = open_order.id
		LEFT JOIN trade_orders close_order ON close_order.id = p.close_order_id
		LEFT JOIN trade_signals close_signal ON close_signal.id = close_order.signal_id
		LEFT JOIN trade_fills close_fill ON close_fill.order_id = close_order.id
		WHERE p.id = $1`, id)
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
		WHERE id = $1 AND status = 'open'`, positionID, lastPrice, marketValue, unrealized, maxProfitRate, maxDrawdownAmount, time.Now().UTC())
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

func scanBasicTradePosition(scanner rowScanner) (model.TradePosition, error) {
	var item model.TradePosition
	var closedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.AccountID, &item.TradeMode, &item.TokenAddress, &item.Status,
		&item.OpenOrderID, &item.CloseOrderID, &item.Quantity, &item.CostAmount,
		&item.AvgCostPrice, &item.LastPrice, &item.MarketValue, &item.RealizedPNL,
		&item.UnrealizedPNL, &item.MaxProfitRate, &item.MaxDrawdownAmount,
		&item.OpenedAt, &closedAt, &item.UpdatedAt,
	); err != nil {
		return model.TradePosition{}, err
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	return item, nil
}

func scanTradeOrder(scanner rowScanner) (model.TradeOrder, error) {
	var item model.TradeOrder
	var confirmedAt sql.NullTime
	var requestJSON []byte
	var responseJSON []byte
	if err := scanner.Scan(&item.ID, &item.AccountID, &item.SignalID, &item.TradeMode, &item.ExecutionChannel, &item.TokenAddress, &item.Side, &item.IntentAmountUSD, &item.IntentAmountSOL, &item.IntentTokenAmount, &item.Status, &requestJSON, &responseJSON, &item.SubmitTxHash, &confirmedAt, &item.FailReason, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.TradeOrder{}, err
	}
	if len(requestJSON) > 0 {
		item.JupiterRequestJSON = append(json.RawMessage(nil), requestJSON...)
	}
	if len(responseJSON) > 0 {
		item.JupiterResponseJSON = append(json.RawMessage(nil), responseJSON...)
	}
	if confirmedAt.Valid {
		item.ConfirmedAt = &confirmedAt.Time
	}
	return item, nil
}

func scanTradePosition(scanner rowScanner) (model.TradePosition, error) {
	var item model.TradePosition
	var closedAt sql.NullTime
	var openSignalTime sql.NullTime
	var closeSignalTime sql.NullTime
	var exitReason sql.NullString
	var signalEntryMarketCap sql.NullFloat64
	var signalExitMarketCap sql.NullFloat64
	var entryExecutedPrice sql.NullFloat64
	var exitExecutedPrice sql.NullFloat64
	var openSignalToken sql.NullString
	var candidatePublishedAt sql.NullInt64
	var openTradeSignalID sql.NullString
	var closeTradeSignalID sql.NullString
	if err := scanner.Scan(&item.ID, &item.AccountID, &item.TradeMode, &item.TokenAddress, &item.Status, &item.OpenOrderID, &item.CloseOrderID, &openTradeSignalID, &closeTradeSignalID, &signalEntryMarketCap, &signalExitMarketCap, &entryExecutedPrice, &exitExecutedPrice, &item.Quantity, &item.CostAmount, &item.AvgCostPrice, &item.LastPrice, &item.MarketValue, &item.RealizedPNL, &item.UnrealizedPNL, &item.MaxProfitRate, &item.MaxDrawdownAmount, &item.OpenedAt, &closedAt, &item.UpdatedAt, &openSignalTime, &closeSignalTime, &exitReason, &openSignalToken, &candidatePublishedAt); err != nil {
		return model.TradePosition{}, err
	}
	if openTradeSignalID.Valid {
		item.OpenTradeSignalID = openTradeSignalID.String
	}
	if closeTradeSignalID.Valid {
		item.CloseTradeSignalID = closeTradeSignalID.String
	}
	if signalEntryMarketCap.Valid {
		item.SignalEntryMarketCap = signalEntryMarketCap.Float64
	}
	if signalExitMarketCap.Valid {
		item.SignalExitMarketCap = signalExitMarketCap.Float64
	}
	if entryExecutedPrice.Valid {
		item.EntryExecutedPrice = entryExecutedPrice.Float64
	}
	if exitExecutedPrice.Valid {
		item.ExitExecutedPrice = exitExecutedPrice.Float64
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	if openSignalTime.Valid {
		item.OpenSignalTime = &openSignalTime.Time
	}
	if closeSignalTime.Valid {
		item.CloseSignalTime = &closeSignalTime.Time
	}
	if exitReason.Valid {
		item.ExitReason = exitReason.String
	}
	if openSignalToken.Valid {
		item.TokenSymbol = strings.TrimSpace(openSignalToken.String)
	}
	if candidatePublishedAt.Valid && candidatePublishedAt.Int64 > 0 {
		candidateAt := time.UnixMilli(candidatePublishedAt.Int64).UTC()
		item.CandidateAt = &candidateAt
	}
	return item, nil
}

type tradeSignalPayload struct {
	Metadata json.RawMessage `json:"metadata"`
}

type tradeSignalMetadata struct {
	Upstream struct {
		Token       string `json:"token"`
		PublishedAt int64  `json:"publishedAt"`
	} `json:"upstream"`
}

// enrichTradePositionMeta 只从买入信号里提取展示层需要的字段，
// 这样 Positions 表和图表联动不需要额外查一轮接口。
func enrichTradePositionMeta(item *model.TradePosition, raw []byte) {
	if item == nil || len(raw) == 0 {
		return
	}
	var payload tradeSignalPayload
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Metadata) == 0 {
		return
	}
	var metadata tradeSignalMetadata
	if err := json.Unmarshal(payload.Metadata, &metadata); err != nil {
		return
	}
	item.TokenSymbol = strings.TrimSpace(metadata.Upstream.Token)
	if metadata.Upstream.PublishedAt > 0 {
		candidateAt := time.UnixMilli(metadata.Upstream.PublishedAt).UTC()
		item.CandidateAt = &candidateAt
	}
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func requireAffectedRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
