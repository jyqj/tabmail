package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"tabmail/internal/models"
)

func hashInviteCode(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// ================================================================
// Users
// ================================================================

func (s *PgStore) CreateUser(ctx context.Context, u *models.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	now := time.Now()
	u.CreatedAt = now
	u.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, display_name, role, is_active, permission_profile_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		u.ID, u.TenantID, strings.ToLower(strings.TrimSpace(u.Email)),
		u.PasswordHash, u.DisplayName, u.Role, u.IsActive, u.PermissionProfileID, u.CreatedAt, u.UpdatedAt)
	return err
}

const userSelect = `SELECT id, tenant_id, email, password_hash, display_name, role, is_active,
	       permission_profile_id, created_at, updated_at, last_login_at
	FROM users`

func scanUser(row pgx.Row) (*models.User, error) {
	u := &models.User{}
	var profileID pgtype.UUID
	err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.Role, &u.IsActive, &profileID, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if profileID.Valid {
		id := uuid.UUID(profileID.Bytes)
		u.PermissionProfileID = &id
	}
	return u, nil
}

func (s *PgStore) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return scanUser(s.pool.QueryRow(ctx, userSelect+` WHERE id = $1`, id))
}

func (s *PgStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return scanUser(s.pool.QueryRow(ctx, userSelect+` WHERE LOWER(email) = LOWER($1)`, strings.TrimSpace(email)))
}

func (s *PgStore) ListUsers(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.User, int, error) {
	pg = pg.Normalize()
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		userSelect+` WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (s *PgStore) UpdateUser(ctx context.Context, u *models.User) error {
	u.UpdatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET email = $2, display_name = $3, role = $4, is_active = $5,
			permission_profile_id = $6, updated_at = $7
		WHERE id = $1`,
		u.ID, strings.ToLower(strings.TrimSpace(u.Email)), u.DisplayName, u.Role, u.IsActive,
		u.PermissionProfileID, u.UpdatedAt)
	return err
}

func (s *PgStore) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	return err
}

func (s *PgStore) DeleteUser(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM tenant_api_keys WHERE owner_user_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) TouchUserLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return err
}

// ================================================================
// Refresh tokens
// ================================================================

func (s *PgStore) CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error {
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	rt.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		rt.ID, rt.UserID, rt.TokenHash, rt.ExpiresAt, rt.CreatedAt)
	return err
}

func (s *PgStore) GetRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	rt := &models.RefreshToken{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at, revoked_at
		FROM refresh_tokens WHERE token_hash = $1`, tokenHash).
		Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.CreatedAt, &rt.RevokedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return rt, err
}

func (s *PgStore) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1`, id)
	return err
}

func (s *PgStore) RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *PgStore) DeleteExpiredRefreshTokens(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	return err
}

// ================================================================
// Admin invitations
// ================================================================

func (s *PgStore) CreateAdminInvitation(ctx context.Context, inv *models.AdminInvitation) error {
	if inv.ID == uuid.Nil {
		inv.ID = uuid.New()
	}
	inv.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_invitations (id, email, invite_code, invited_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		inv.ID, strings.ToLower(strings.TrimSpace(inv.Email)), hashInviteCode(inv.InviteCode),
		inv.InvitedBy, inv.ExpiresAt, inv.CreatedAt)
	return err
}

func (s *PgStore) GetAdminInvitationByCode(ctx context.Context, code string) (*models.AdminInvitation, error) {
	inv := &models.AdminInvitation{}
	var invitedBy pgtype.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, invite_code, invited_by, expires_at, accepted_at, created_at
		FROM admin_invitations WHERE invite_code = $1`, hashInviteCode(code)).
		Scan(&inv.ID, &inv.Email, &inv.InviteCode, &invitedBy,
			&inv.ExpiresAt, &inv.AcceptedAt, &inv.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if invitedBy.Valid {
		id := uuid.UUID(invitedBy.Bytes)
		inv.InvitedBy = &id
	}
	return inv, nil
}

func (s *PgStore) MarkInvitationAccepted(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE admin_invitations SET accepted_at = now() WHERE id = $1`, id)
	return err
}
