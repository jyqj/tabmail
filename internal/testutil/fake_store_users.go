package testutil

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) CreateUser(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if s.users == nil {
		s.users = map[uuid.UUID]*models.User{}
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *FakeStore) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, nil
	}
	u, ok := s.users[id]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (s *FakeStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, nil
	}
	lower := strings.ToLower(strings.TrimSpace(email))
	for _, u := range s.users {
		if strings.ToLower(u.Email) == lower {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *FakeStore) ListUsers(_ context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.User, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return nil, 0, nil
	}
	var all []*models.User
	for _, u := range s.users {
		if u.TenantID == tenantID {
			cp := *u
			all = append(all, &cp)
		}
	}
	return all, len(all), nil
}

func (s *FakeStore) UpdateUser(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return errors.New("user not found")
	}
	if _, ok := s.users[u.ID]; !ok {
		return errors.New("user not found")
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *FakeStore) UpdateUserPassword(_ context.Context, id uuid.UUID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		return errors.New("user not found")
	}
	u, ok := s.users[id]
	if !ok {
		return errors.New("user not found")
	}
	u.PasswordHash = hash
	return nil
}

func (s *FakeStore) DeleteUser(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users != nil {
		delete(s.users, id)
	}
	return nil
}

func (s *FakeStore) TouchUserLogin(_ context.Context, _ uuid.UUID) error { return nil }

func (s *FakeStore) CreateRefreshToken(_ context.Context, rt *models.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	cp := *rt
	s.refreshTokens[cp.ID] = &cp
	return nil
}

func (s *FakeStore) GetRefreshToken(_ context.Context, _ string) (*models.RefreshToken, error) {
	return nil, nil
}

func (s *FakeStore) RevokeRefreshToken(_ context.Context, _ uuid.UUID) error { return nil }

func (s *FakeStore) ConsumeRefreshToken(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.refreshTokens[id]
	if rt == nil || rt.RevokedAt != nil || !rt.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	now := time.Now()
	rt.RevokedAt = &now
	return true, nil
}

func (s *FakeStore) RevokeUserRefreshTokens(_ context.Context, _ uuid.UUID) error { return nil }

func (s *FakeStore) DeleteExpiredRefreshTokens(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := 0
	now := time.Now()
	for id, rt := range s.refreshTokens {
		if rt.ExpiresAt.Before(now) {
			delete(s.refreshTokens, id)
			deleted++
		}
	}
	return deleted, nil
}

// CountRefreshTokens reports the rows currently held; tests use it to verify
// retention sweeps.
func (s *FakeStore) CountRefreshTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refreshTokens)
}

func (s *FakeStore) CreateAdminInvitation(_ context.Context, _ *models.AdminInvitation) error {
	return nil
}

func (s *FakeStore) GetAdminInvitationByCode(_ context.Context, _ string) (*models.AdminInvitation, error) {
	return nil, nil
}

func (s *FakeStore) AcceptAdminInvitation(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}
