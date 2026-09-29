package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

// effectivePermissionSnapshot requires the current user's SHARE lock, acquired
// by currentMemberActor. That row protects profile assignment and both presence
// and absence of an override: the override writers below lock the same user.
// The assigned profile is separately SHARE-locked before the canonical merge.
//
// Do not wait for a profile while holding its user: profile deletion first locks
// the profile and can then need the user for ON DELETE SET NULL. NOWAIT turns
// that opposite order into a retryable conflict, not a new deadlock. No tenant
// lock, persistent revision, or alternative permission semantics are added here.
func effectivePermissionSnapshot(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*models.EffectivePermission, error) {
	var profileID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT p.id FROM users u JOIN permission_profiles p ON p.id=u.permission_profile_id WHERE u.id=$1 FOR SHARE OF p NOWAIT`, userID).Scan(&profileID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "55P03" {
			return nil, app.Conflict("permission profile is changing; reload before retrying")
		}
		return nil, err
	}
	// No assigned profile is valid: defaults/overrides retain their existing
	// COALESCE, explicit false/zero and NULL/empty-array meanings.
	return effectivePermission(ctx, tx, userID)
}

// Override rows are optional, so locking only an existing override cannot fence
// first insertion or deletion/recreation. Use the stable user row instead, before
// touching the override. NO KEY UPDATE conflicts with readers' SHARE while
// remaining compatible with foreign-key KEY SHARE; no tenant FK is introduced.
// Public administrative authorization and revision/CAS remain separate concerns.
func (s *PgStore) permissionOverrideTx(ctx context.Context, userID uuid.UUID, missingOK bool, write func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		if missingOK {
			return nil
		}
		return fmt.Errorf("user %s not found", userID)
	}
	if err != nil {
		return err
	}
	if err = write(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
