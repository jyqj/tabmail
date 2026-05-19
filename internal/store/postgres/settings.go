package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) GetSetting(ctx context.Context, key string) (*models.SystemSetting, error) {
	ss := &models.SystemSetting{}
	err := s.pool.QueryRow(ctx,
		`SELECT key, value, description, updated_at FROM system_settings WHERE key = $1`, key).
		Scan(&ss.Key, &ss.Value, &ss.Description, &ss.UpdatedAt)
	if err == pgx.ErrNoRows {
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
