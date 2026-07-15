package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"solana-meme-backtest/backend/internal/runtimeconfig"
)

func TestRuntimeSettingsRepositoryLoadsBothSwitches(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE setting_key IN ($1, $2)")).
		WithArgs(caMonitoringEnabledSettingKey, tradeExecutionEnabledSettingKey).
		WillReturnRows(sqlmock.NewRows([]string{"setting_key", "setting_value"}).
			AddRow(caMonitoringEnabledSettingKey, "true").
			AddRow(tradeExecutionEnabledSettingKey, "false"))

	caMonitoring, tradeExecution, err := NewRuntimeSettingsRepository(db).LoadRuntimeSwitches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caMonitoring == nil || !*caMonitoring || tradeExecution == nil || *tradeExecution {
		t.Fatalf("unexpected switches: ca=%v trade=%v", caMonitoring, tradeExecution)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSettingsRepositorySavesBothSwitchesInTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)
	mock.ExpectBegin()
	query := regexp.QuoteMeta("INSERT INTO system_runtime_settings (setting_key, setting_value, created_at, updated_at)")
	mock.ExpectExec(query).
		WithArgs(caMonitoringEnabledSettingKey, "true", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(query).
		WithArgs(tradeExecutionEnabledSettingKey, "false", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = NewRuntimeSettingsRepository(db).SaveRuntimeSwitches(context.Background(), runtimeconfig.State{
		CAMonitoringEnabled:   true,
		TradeExecutionEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
