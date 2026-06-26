package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

func Open(dsn string, autoMigrate bool) (*sql.DB, error) {
	if dsn == "" {
		return nil, errors.New("数据库 DSN 未配置")
	}
	if strings.HasPrefix(dsn, "sqlite:") {
		return openBusinessSQLite(strings.TrimPrefix(dsn, "sqlite:"), autoMigrate)
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := database.Ping(); err != nil {
		return nil, err
	}
	if autoMigrate {
		if err := migrate(database); err != nil {
			return nil, err
		}
	}
	return database, nil
}

func openBusinessSQLite(path string, autoMigrate bool) (*sql.DB, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, errors.New("sqlite 数据库路径未配置")
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", trimmed)
	if err != nil {
		return nil, err
	}
	if _, err := database.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		database.Close()
		return nil, err
	}
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		database.Close()
		return nil, err
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	if autoMigrate {
		if err := migrateSQLite(database); err != nil {
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
			token_symbol varchar(64),
			` + "`interval`" + ` varchar(32) NOT NULL,
			start_time datetime(3) NOT NULL,
			end_time datetime(3) NOT NULL,
			created_at datetime(3),
			updated_at datetime(3),
			INDEX idx_backtest_sessions_token_address (token_address),
			INDEX idx_backtest_sessions_start_time (start_time),
			INDEX idx_backtest_sessions_end_time (end_time)
		)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_points (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL,
			side varchar(16) NOT NULL,
			point_time datetime(3) NOT NULL,
			input_price double,
			note varchar(512),
			matched_kline_time datetime(3),
			matched_price double,
			created_at datetime(3),
			INDEX idx_backtest_trade_points_session_id (session_id),
			INDEX idx_backtest_trade_points_point_time (point_time)
		)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_results (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL,
			buy_point_id varchar(36) NOT NULL,
			sell_point_id varchar(36) NOT NULL,
			buy_matched_kline_time datetime(3) NOT NULL,
			sell_matched_kline_time datetime(3) NOT NULL,
			buy_price double NOT NULL,
			sell_price double NOT NULL,
			profit double NOT NULL,
			profit_rate double NOT NULL,
			holding_seconds bigint NOT NULL,
			win boolean NOT NULL,
			created_at datetime(3),
			INDEX idx_backtest_trade_results_session_id (session_id)
		)`,
		`CREATE TABLE IF NOT EXISTS backtest_metric_snapshots (
			id varchar(36) PRIMARY KEY,
			session_id varchar(36) NOT NULL UNIQUE,
			trade_count bigint NOT NULL,
			win_rate double NOT NULL,
			total_profit_rate double NOT NULL,
			max_drawdown_rate double NOT NULL,
			average_holding_seconds bigint NOT NULL,
			created_at datetime(3)
		)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateSQLite(database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS backtest_sessions (
			id TEXT PRIMARY KEY,
			token_address TEXT NOT NULL,
			token_symbol TEXT,
			interval TEXT NOT NULL,
			start_time TEXT NOT NULL,
			end_time TEXT NOT NULL,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_token_address ON backtest_sessions (token_address)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_start_time ON backtest_sessions (start_time)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_sessions_end_time ON backtest_sessions (end_time)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_points (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			side TEXT NOT NULL,
			point_time TEXT NOT NULL,
			input_price REAL,
			note TEXT,
			matched_kline_time TEXT,
			matched_price REAL,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_points_session_id ON backtest_trade_points (session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_points_point_time ON backtest_trade_points (point_time)`,
		`CREATE TABLE IF NOT EXISTS backtest_trade_results (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			buy_point_id TEXT NOT NULL,
			sell_point_id TEXT NOT NULL,
			buy_matched_kline_time TEXT NOT NULL,
			sell_matched_kline_time TEXT NOT NULL,
			buy_price REAL NOT NULL,
			sell_price REAL NOT NULL,
			profit REAL NOT NULL,
			profit_rate REAL NOT NULL,
			holding_seconds INTEGER NOT NULL,
			win INTEGER NOT NULL,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_trade_results_session_id ON backtest_trade_results (session_id)`,
		`CREATE TABLE IF NOT EXISTS backtest_metric_snapshots (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL UNIQUE,
			trade_count INTEGER NOT NULL,
			win_rate REAL NOT NULL,
			total_profit_rate REAL NOT NULL,
			max_drawdown_rate REAL NOT NULL,
			average_holding_seconds INTEGER NOT NULL,
			created_at TEXT
		)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("migrate sqlite failed: %w", err)
		}
	}
	return nil
}
