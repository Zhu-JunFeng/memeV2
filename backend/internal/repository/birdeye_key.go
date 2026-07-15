package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"solana-meme-backtest/backend/internal/model"
)

var ErrBirdeyeAPIKeyEmpty = errors.New("Birdeye API Key 不能为空")

type BirdeyeAPIKeyRepository struct {
	db *sql.DB
}

func NewBirdeyeAPIKeyRepository(db *sql.DB) *BirdeyeAPIKeyRepository {
	return &BirdeyeAPIKeyRepository{db: db}
}

func (r *BirdeyeAPIKeyRepository) EnsureConfigKeys(ctx context.Context, keys []string) error {
	for _, key := range normalizeBirdeyeKeys(keys) {
		now := time.Now().UTC()
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO birdeye_api_keys (id, api_key, key_mask, status, unavailable_reason, created_at, updated_at)
			VALUES ($1, $2, $3, $4, '', $5, $6)
			ON CONFLICT (api_key) DO NOTHING`,
			uuid.NewString(), key, maskBirdeyeKey(key), model.BirdeyeAPIKeyStatusAvailable, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *BirdeyeAPIKeyRepository) AddKey(ctx context.Context, apiKey string) (model.BirdeyeAPIKey, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return model.BirdeyeAPIKey{}, ErrBirdeyeAPIKeyEmpty
	}
	now := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO birdeye_api_keys (id, api_key, key_mask, status, unavailable_reason, unavailable_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, '', NULL, $5, $6)
		ON CONFLICT (api_key) DO UPDATE SET
			key_mask = excluded.key_mask,
			status = excluded.status,
			unavailable_reason = '',
			unavailable_at = NULL,
			updated_at = excluded.updated_at`,
		uuid.NewString(), apiKey, maskBirdeyeKey(apiKey), model.BirdeyeAPIKeyStatusAvailable, now, now,
	); err != nil {
		return model.BirdeyeAPIKey{}, err
	}
	return r.GetByKey(ctx, apiKey)
}

func (r *BirdeyeAPIKeyRepository) GetByKey(ctx context.Context, apiKey string) (model.BirdeyeAPIKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, key_mask, status, unavailable_reason, unavailable_at, last_successful_used_at, created_at, updated_at
		FROM birdeye_api_keys
		WHERE api_key = $1`, strings.TrimSpace(apiKey))
	return scanBirdeyeAPIKey(row)
}

func (r *BirdeyeAPIKeyRepository) ListAvailableBirdeyeKeys(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT api_key
		FROM birdeye_api_keys
		WHERE status = $1
		ORDER BY created_at ASC`, model.BirdeyeAPIKeyStatusAvailable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	return keys, rows.Err()
}

func (r *BirdeyeAPIKeyRepository) MarkBirdeyeKeyUnavailable(ctx context.Context, apiKey string, reason string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE birdeye_api_keys
		SET status = $2,
			unavailable_reason = $3,
			unavailable_at = $4,
			updated_at = $5
		WHERE api_key = $1`,
		strings.TrimSpace(apiKey), model.BirdeyeAPIKeyStatusUnavailable, strings.TrimSpace(reason), now, now,
	)
	return err
}

func (r *BirdeyeAPIKeyRepository) MarkBirdeyeKeySuccessful(ctx context.Context, apiKey string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE birdeye_api_keys
		SET last_successful_used_at = $2,
			updated_at = $2
		WHERE api_key = $1`,
		strings.TrimSpace(apiKey), now,
	)
	return err
}

func scanBirdeyeAPIKey(row interface {
	Scan(dest ...any) error
}) (model.BirdeyeAPIKey, error) {
	var item model.BirdeyeAPIKey
	var unavailableAt sql.NullTime
	var lastSuccessfulUsedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.KeyMask, &item.Status, &item.UnavailableReason, &unavailableAt, &lastSuccessfulUsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.BirdeyeAPIKey{}, err
	}
	if unavailableAt.Valid {
		item.UnavailableAt = &unavailableAt.Time
	}
	if lastSuccessfulUsedAt.Valid {
		item.LastSuccessfulUsedAt = &lastSuccessfulUsedAt.Time
	}
	return item, nil
}

func normalizeBirdeyeKeys(keys []string) []string {
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, item := range keys {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized
}

func maskBirdeyeKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) <= 10 {
		return apiKey
	}
	return apiKey[:6] + "***" + apiKey[len(apiKey)-4:]
}
