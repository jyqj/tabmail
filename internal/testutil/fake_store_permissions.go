package testutil

import (
	"context"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) CreatePermissionProfile(_ context.Context, _ *models.PermissionProfile) error {
	return nil
}

func (s *FakeStore) GetPermissionProfile(_ context.Context, _ uuid.UUID) (*models.PermissionProfile, error) {
	return nil, nil
}

func (s *FakeStore) GetPermissionProfileByName(_ context.Context, _ string) (*models.PermissionProfile, error) {
	return nil, nil
}

func (s *FakeStore) ListPermissionProfiles(_ context.Context, _ *uuid.UUID) ([]*models.PermissionProfile, error) {
	return nil, nil
}

func (s *FakeStore) UpdatePermissionProfile(_ context.Context, _ *models.PermissionProfile) error {
	return nil
}

func (s *FakeStore) DeletePermissionProfile(_ context.Context, _ uuid.UUID, _ *uuid.UUID) error {
	return nil
}

func (s *FakeStore) UpsertUserPermissionOverride(_ context.Context, _ *models.UserPermissionOverride) error {
	return nil
}

func (s *FakeStore) DeleteUserPermissionOverride(_ context.Context, _ uuid.UUID) error { return nil }

func (s *FakeStore) EffectivePermission(_ context.Context, _ uuid.UUID) (*models.EffectivePermission, error) {
	return nil, nil
}
