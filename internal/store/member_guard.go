package store

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"tabmail/internal/authz"
	"tabmail/internal/models"
)

var (
	ErrLastAdministrator = errors.New("cannot remove the last active company administrator")
	ErrMemberNotFound    = errors.New("company member not found")
	ErrMemberOwnsMailbox = errors.New("transfer owned mailboxes before deleting this member")
)

type MemberGuardStore interface {
	UpdateUserGuarded(context.Context, authz.Actor, uuid.UUID, uuid.UUID, models.UserAdminPatch) (*models.User, error)
	DeleteUserGuarded(context.Context, authz.Actor, uuid.UUID, uuid.UUID) error
}

type RefreshRotationStore interface {
	RotateRefreshToken(context.Context, string, *models.RefreshToken) (rotated bool, familyRevoked bool, err error)
	RevokeRefreshTokenByHash(context.Context, string) error
}
