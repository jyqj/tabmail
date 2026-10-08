package models

import "github.com/google/uuid"

// RefreshTokenIssuance binds a new session to the precise credentials observed
// by authentication. Adapters check it while holding current user ownership
// until the refresh token commits. It is neither persisted nor serialized.
type RefreshTokenIssuance struct {
	UserID         uuid.UUID `json:"-" db:"-"`
	TenantID       uuid.UUID `json:"-" db:"-"`
	PasswordHash   string    `json:"-" db:"-"`
	SessionVersion int64     `json:"-" db:"-"`
}

func (p RefreshTokenIssuance) MatchesUser(id uuid.UUID, user *User) bool {
	return user != nil && user.IsActive && p.UserID != uuid.Nil && p.TenantID != uuid.Nil && p.PasswordHash != "" &&
		id == p.UserID && user.ID == p.UserID && user.TenantID == p.TenantID &&
		user.PasswordHash == p.PasswordHash && user.SessionVersion == p.SessionVersion
}
