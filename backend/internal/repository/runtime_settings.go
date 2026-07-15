package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"solana-meme-backtest/backend/internal/runtimeconfig"
)

const (
	caMonitoringEnabledSettingKey   = "signal.ca_monitoring_enabled"
	tradeExecutionEnabledSettingKey = "trade.signal_execution_enabled"
)

type RuntimeSettingsRepository struct {
	db *sql.DB
}

func NewRuntimeSettingsRepository(db *sql.DB) *RuntimeSettingsRepository {
	return &RuntimeSettingsRepository{db: db}
}

func (r *RuntimeSettingsRepository) LoadRuntimeSwitches(ctx context.Context) (*bool, *bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT setting_key, setting_value
		FROM system_runtime_settings
		WHERE setting_key IN ($1, $2)`, caMonitoringEnabledSettingKey, tradeExecutionEnabledSettingKey)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var caMonitoring *bool
	var tradeExecution *bool
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, nil, err
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, nil, err
		}
		switch key {
		case caMonitoringEnabledSettingKey:
			caMonitoring = &parsed
		case tradeExecutionEnabledSettingKey:
			tradeExecution = &parsed
		}
	}
	return caMonitoring, tradeExecution, rows.Err()
}

func (r *RuntimeSettingsRepository) SaveRuntimeSwitches(ctx context.Context, state runtimeconfig.State) error {
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settings := map[string]string{
		caMonitoringEnabledSettingKey:   strconv.FormatBool(state.CAMonitoringEnabled),
		tradeExecutionEnabledSettingKey: strconv.FormatBool(state.TradeExecutionEnabled),
	}
	for key, value := range settings {
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
