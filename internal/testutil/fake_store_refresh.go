package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tabmail/internal/models"
)

func (s *FakeStore) CreateRefreshToken(_ context.Context, r *models.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createRefreshLocked(r)
}
func (s *FakeStore) createRefreshLocked(r *models.RefreshToken) error {
	if s.refreshTokens == nil {
		s.refreshTokens = map[string]*models.RefreshToken{}
	}
	if r == nil || r.TokenHash == "" {
		return fmt.Errorf("invalid refresh token")
	}
	if _, exists := s.refreshTokens[r.TokenHash]; exists {
		return fmt.Errorf("duplicate refresh token")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.FamilyID == uuid.Nil {
		r.FamilyID = r.ID
	}
	r.CreatedAt = time.Now().UTC()
	cp := *r
	s.refreshTokens[r.TokenHash] = &cp
	return nil
}
func (s *FakeStore) GetRefreshToken(_ context.Context, hash string) (*models.RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.refreshTokens[hash]
	if r == nil {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}
func (s *FakeStore) RevokeRefreshToken(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, r := range s.refreshTokens {
		if r.ID == id && r.RevokedAt == nil {
			r.RevokedAt = &now
		}
	}
	return nil
}
func (s *FakeStore) revokeFamilyLocked(family uuid.UUID) {
	now := time.Now().UTC()
	for _, r := range s.refreshTokens {
		if r.FamilyID == family && r.RevokedAt == nil {
			r.RevokedAt = &now
		}
	}
}
func (s *FakeStore) RevokeRefreshTokenByHash(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.refreshTokens[hash]; r != nil {
		s.revokeFamilyLocked(r.FamilyID)
	}
	return nil
}
func (s *FakeStore) RevokeUserRefreshTokens(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, r := range s.refreshTokens {
		if r.UserID == id && r.RevokedAt == nil {
			r.RevokedAt = &now
		}
	}
	return nil
}
func (s *FakeStore) DeleteExpiredRefreshTokens(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	retained := make(map[uuid.UUID]bool)
	for _, r := range s.refreshTokens {
		if r.ExpiresAt.After(now) {
			retained[r.FamilyID] = true
		}
	}
	for hash, r := range s.refreshTokens {
		if !retained[r.FamilyID] {
			delete(s.refreshTokens, hash)
		}
	}
	return nil
}
func (s *FakeStore) RotateRefreshToken(_ context.Context, hash string, next *models.RefreshToken) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == nil || next.TokenHash == "" || next.TokenHash == hash {
		return false, false, fmt.Errorf("invalid refresh replacement")
	}
	old := s.refreshTokens[hash]
	if old == nil {
		return false, false, nil
	}
	if old.RevokedAt != nil {
		s.revokeFamilyLocked(old.FamilyID)
		return false, true, nil
	}
	u := s.users[old.UserID]
	if !old.ExpiresAt.After(time.Now()) || u == nil || !u.IsActive {
		return false, false, nil
	}
	if !next.ExpiresAt.After(time.Now()) {
		return false, false, fmt.Errorf("replacement expired")
	}
	if _, exists := s.refreshTokens[next.TokenHash]; exists {
		return false, false, fmt.Errorf("duplicate refresh token")
	}
	next.UserID = old.UserID
	next.FamilyID = old.FamilyID
	next.RevokedAt = nil
	if err := s.createRefreshLocked(next); err != nil {
		return false, false, err
	}
	now := time.Now().UTC()
	old.RevokedAt = &now
	return true, false, nil
}
