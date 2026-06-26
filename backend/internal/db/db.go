package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(dsn string, autoMigrate bool) (*sql.DB, error) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return nil, errors.New("PostgreSQL DSN 未配置")
	}
	database, err := sql.Open("pgx", trimmed)
	if err != nil {
		return nil, err
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	if autoMigrate {
		if err := migrate(database); err != nil {
			database.Close()
			return nil, err
		}
	}
	return database, nil
}

func migrate(database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS backtest_sessions (
			id varchar(36) PRIMARY KEY,
			token_address varchar(128) NOT NULL,
			token_symbol varchar(64) NOT NULL DEFAULT '',
			"interval" varchar(32) NOT NULL,
			start_time timestamptz NOT NULL,
			end_time timestamptz NOT NULL,
			created_at timestamptz,
			updated_at timestamptz
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_token_address ON backtest_sessions (token_address)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_start_time ON backtest_sessions (start_time)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_end_time ON backtest_sessions (end_time)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_points (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL,
			side varchar(16) NOT NULL,
			point_time timestamptz NOT NULL,
			input_price double precision,
			note varchar(512) NOT NULL DEFAULT '',
			matched_kline_time timestamptz,
			matched_price double precision,
			created_at timestamptz
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_points_session_id ON backtest_trade_points (session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_points_point_time ON backtest_trade_points (point_time)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_results (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL,
			buy_point_id varchar(36) NOT NULL,
			sell_point_id varchar(36) NOT NULL,
			buy_matched_kline_time timestamptz NOT NULL,
			sell_matched_kline_time timestamptz NOT NULL,
			buy_price double precision NOT NULL,
			sell_price double precision NOT NULL,
			profit double precision NOT NULL,
			profit_rate double precision NOT NULL,
			holding_seconds bigint NOT NULL,
			win boolean NOT NULL,
			created_at timestamptz
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_results_session_id ON backtest_trade_results (session_id)`,
		`CREATE TABLE IF NOT EXISTS backtest_metric_snapshots (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL UNIQUE,
			trade_count bigint NOT NULL,
			win_rate double precision NOT NULL,
			total_profit_rate double precision NOT NULL,
			max_drawdown_rate double precision NOT NULL,
			average_holding_seconds bigint NOT NULL,
			created_at timestamptz
		)`,
		`CREATE TABLE IF NOT EXISTS birdeye_kline_cache (
			token_address varchar(128) NOT NULL,
			"interval" varchar(32) NOT NULL,
			open_time timestamptz NOT NULL,
			close_time timestamptz NOT NULL,
			market_cap_open double precision NOT NULL,
			market_cap_high double precision NOT NULL,
			market_cap_low double precision NOT NULL,
			market_cap_close double precision NOT NULL,
			volume double precision NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			PRIMARY KEY (token_address, "interval", open_time)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_birdeye_kline_cache_token_interval_open_time ON birdeye_kline_cache (token_address, "interval", open_time)`,
		`CREATE TABLE IF NOT EXISTS birdeye_kline_cache_ranges (
			token_address varchar(128) NOT NULL,
			"interval" varchar(32) NOT NULL,
			range_start timestamptz NOT NULL,
			range_end timestamptz NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			PRIMARY KEY (token_address, "interval", range_start, range_end)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_birdeye_kline_cache_ranges_lookup ON birdeye_kline_cache_ranges (token_address, "interval", range_start, range_end)`,
		`CREATE TABLE IF NOT EXISTS trade_accounts (
			id varchar(36) PRIMARY KEY,
			name varchar(64) NOT NULL UNIQUE,
			wallet_address varchar(128) NOT NULL,
			status varchar(16) NOT NULL,
			buy_amount_usd double precision NOT NULL,
			slippage_bps integer NOT NULL,
			priority_fee_lamports bigint NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS trade_signals (
			id varchar(36) PRIMARY KEY,
			signal_id varchar(96) NOT NULL UNIQUE,
			signal_type varchar(16) NOT NULL,
			strategy_code varchar(64) NOT NULL,
			token_address varchar(128) NOT NULL,
			"interval" varchar(32) NOT NULL,
			signal_time timestamptz NOT NULL,
			trigger_price double precision NOT NULL,
			trigger_market_cap double precision NOT NULL,
			reason text NOT NULL,
			raw_payload_json jsonb NOT NULL,
			consume_status varchar(16) NOT NULL,
			created_at timestamptz NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_signals_token_address ON trade_signals (token_address)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_signals_signal_time ON trade_signals (signal_time DESC)`,
		`CREATE TABLE IF NOT EXISTS trade_orders (
			id varchar(36) PRIMARY KEY,
			account_id varchar(36) NOT NULL,
			signal_id varchar(36) NOT NULL,
			token_address varchar(128) NOT NULL,
			side varchar(16) NOT NULL,
			intent_amount_usd double precision NOT NULL,
			intent_token_amount double precision NOT NULL,
			status varchar(24) NOT NULL,
			jupiter_request_json jsonb,
			jupiter_response_json jsonb,
			submit_tx_hash varchar(128) NOT NULL DEFAULT '',
			confirmed_at timestamptz,
			fail_reason text NOT NULL DEFAULT '',
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_orders_account_created_at ON trade_orders (account_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_orders_signal_id ON trade_orders (signal_id)`,
		`CREATE TABLE IF NOT EXISTS trade_fills (
			id varchar(36) PRIMARY KEY,
			order_id varchar(36) NOT NULL,
			tx_hash varchar(128) NOT NULL,
			side varchar(16) NOT NULL,
			token_address varchar(128) NOT NULL,
			filled_token_amount double precision NOT NULL,
			filled_quote_amount double precision NOT NULL,
			avg_price double precision NOT NULL,
			fee_amount double precision NOT NULL,
			fee_asset varchar(32) NOT NULL,
			executed_at timestamptz NOT NULL,
			created_at timestamptz NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_fills_order_id ON trade_fills (order_id)`,
		`CREATE TABLE IF NOT EXISTS trade_positions (
			id varchar(36) PRIMARY KEY,
			account_id varchar(36) NOT NULL,
			token_address varchar(128) NOT NULL,
			status varchar(16) NOT NULL,
			open_order_id varchar(36) NOT NULL,
			close_order_id varchar(36) NOT NULL DEFAULT '',
			quantity double precision NOT NULL,
			cost_amount double precision NOT NULL,
			avg_cost_price double precision NOT NULL,
			last_price double precision NOT NULL,
			market_value double precision NOT NULL,
			realized_pnl double precision NOT NULL,
			unrealized_pnl double precision NOT NULL,
			max_profit_rate double precision NOT NULL,
			max_drawdown_amount double precision NOT NULL,
			opened_at timestamptz NOT NULL,
			closed_at timestamptz,
			updated_at timestamptz NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_trade_positions_account_token_open ON trade_positions (account_id, token_address) WHERE status = 'open'`,
		`CREATE INDEX IF NOT EXISTS idx_trade_positions_status_updated_at ON trade_positions (status, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trade_order_events (
			id varchar(36) PRIMARY KEY,
			order_id varchar(36) NOT NULL,
			event_type varchar(32) NOT NULL,
			event_time timestamptz NOT NULL,
			detail_json jsonb NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_order_events_order_time ON trade_order_events (order_id, event_time DESC)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("migrate postgres failed: %w", err)
		}
	}
	return nil
}
