package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func OpenSQLite(path string) (*sql.DB, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("Birdeye cache sqlite 路径未配置")
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
	if err := sqliteMigrate(database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func sqliteMigrate(database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS birdeye_kline_cache (
			token_address TEXT NOT NULL,
			interval TEXT NOT NULL,
			open_time TEXT NOT NULL,
			close_time TEXT NOT NULL,
			market_cap_open REAL NOT NULL,
			market_cap_high REAL NOT NULL,
			market_cap_low REAL NOT NULL,
			market_cap_close REAL NOT NULL,
			volume REAL NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (token_address, interval, open_time)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_birdeye_kline_cache_token_interval_open_time
			ON birdeye_kline_cache (token_address, interval, open_time)`,
		`CREATE TABLE IF NOT EXISTS birdeye_kline_cache_ranges (
			token_address TEXT NOT NULL,
			interval TEXT NOT NULL,
			range_start TEXT NOT NULL,
			range_end TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (token_address, interval, range_start, range_end)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_birdeye_kline_cache_ranges_lookup
			ON birdeye_kline_cache_ranges (token_address, interval, range_start, range_end)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
