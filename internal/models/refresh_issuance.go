package models

import "github.com/google/uuid"

// RefreshTokenIssuance binds a new session to the precise credentials observed
// by authentication. Adapters check it while holding current user ownership
// until the refresh token commits. A successful rotation returns the snapshot
// it observed under that same user lock, so a later read cannot adopt a newer
// authenticated session. It is neither persisted nor serialized.
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
