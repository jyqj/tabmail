package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ store.RefreshRotationStore = (*PgStore)(nil)

// All rotations and family revocations lock the same stable family before any
// token row. Locking only the old token misses concurrent descendant rotations.
func lockRefreshFamily(ctx context.Context, tx pgx.Tx, hash string) (uuid.UUID, error) {
	var family uuid.UUID
	err := tx.QueryRow(ctx, `SELECT family_id FROM refresh_tokens WHERE token_hash=$1`, hash).Scan(&family)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tabmail:refresh-family:"+family.String())
	return family, err
}

func (s *PgStore) RotateRefreshToken(ctx context.Context, oldHash string, next *models.RefreshToken) (bool, bool, error) {
	if next == nil || next.TokenHash == "" || next.TokenHash == oldHash {
		return false, false, fmt.Errorf("invalid refresh replacement")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	family, err := lockRefreshFamily(ctx, tx, oldHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	// Lock the user before token rows, matching user deletion's FK lock order.
	var userID uuid.UUID
	var active bool
	err = tx.QueryRow(ctx, `SELECT u.id,u.is_active FROM users u JOIN refresh_tokens r ON r.user_id=u.id WHERE r.token_hash=$1 FOR SHARE OF u`, oldHash).Scan(&userID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	var id uuid.UUID
	var revoked *time.Time
	var expired bool
	err = tx.QueryRow(ctx, `SELECT id,revoked_at,expires_at<=clock_timestamp() FROM refresh_tokens WHERE token_hash=$1 AND family_id=$2 FOR UPDATE`, oldHash, family).Scan(&id, &revoked, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if revoked != nil {
		if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE family_id=$1 AND revoked_at IS NULL`, family); err != nil {
			return false, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	if expired || !active {
		return false, false, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, id)
	if err != nil {
		return false, false, err
	}
	if tag.RowsAffected() != 1 {
		return false, false, nil
	}
	if next.ID == uuid.Nil {
		next.ID = uuid.New()
	}
	next.UserID = userID
	next.FamilyID = family
	next.RevokedAt = nil
	next.CreatedAt = time.Now().UTC()
	tag, err = tx.Exec(ctx, `INSERT INTO refresh_tokens(id,user_id,token_hash,expires_at,created_at,family_id)
 SELECT $1,$2,$3,$4,clock_timestamp(),$5 WHERE $4>clock_timestamp()`, next.ID, userID, next.TokenHash, next.ExpiresAt, family)
	if err != nil {
		return false, false, err
	}
	if tag.RowsAffected() != 1 {
		return false, false, fmt.Errorf("replacement refresh token already expired")
	}
	if err = tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// Logout revokes the cookie's family, including a child created concurrently
// with logout. Missing/already revoked tokens are idempotent; DB errors are not.
func (s *PgStore) RevokeRefreshTokenByHash(ctx context.Context, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	family, err := lockRefreshFamily(ctx, tx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE family_id=$1 AND revoked_at IS NULL`, family); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) deleteExpiredRefreshFamily(ctx context.Context, family uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tabmail:refresh-family:"+family.String()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE family_id=$1 AND NOT EXISTS (
        SELECT 1 FROM refresh_tokens WHERE family_id=$1 AND expires_at>clock_timestamp()
    )`, family); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
