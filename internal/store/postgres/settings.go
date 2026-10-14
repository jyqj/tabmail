package postgres

import (
	"context"
	"errors"
	"time"

	"tabmail/internal/models"

	"github.com/jackc/pgx/v5"
)

func (s *PgStore) GetSetting(ctx context.Context, key string) (*models.SystemSetting, error) {
	ss := &models.SystemSetting{}
	err := s.pool.QueryRow(ctx,
		`SELECT key, value, description, updated_at FROM system_settings WHERE key = $1`, key).
		Scan(&ss.Key, &ss.Value, &ss.Description, &ss.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ss, err
}

func (s *PgStore) UpsertSetting(ctx context.Context, key, value, description string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, description, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description, updated_at = EXCLUDED.updated_at`,
		key, value, description, time.Now().UTC())
	return err
}

// SeedSetting inserts an absent default atomically. A concurrent administrator
// or startup may have already committed this key; retain that row verbatim.
func (s *PgStore) SeedSetting(ctx context.Context, key, value, description string) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, description, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO NOTHING`, key, value, description, time.Now().UTC())
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (s *PgStore) ListSettings(ctx context.Context) ([]*models.SystemSetting, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, value, description, updated_at FROM system_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SystemSetting
	for rows.Next() {
		ss := &models.SystemSetting{}
		if err := rows.Scan(&ss.Key, &ss.Value, &ss.Description, &ss.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}
