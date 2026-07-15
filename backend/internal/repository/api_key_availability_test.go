package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"solana-meme-backtest/backend/internal/model"
)

func TestGMGNAPIKeyAvailabilityCountsAvailableAndTotalKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FILTER (WHERE status = $1), COUNT(*) FROM gmgn_api_keys")).
		WithArgs(model.GMGNAPIKeyStatusAvailable).
		WillReturnRows(sqlmock.NewRows([]string{"available", "total"}).AddRow(2, 5))

	available, total, err := NewGMGNAPIKeyRepository(db).APIKeyAvailability(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if available != 2 || total != 5 {
		t.Fatalf("unexpected availability: %d/%d", available, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
