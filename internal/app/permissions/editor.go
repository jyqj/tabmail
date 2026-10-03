package permissions

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// EditorStore is the versioned port consumed by the existing permission
// service. It is not a parallel rules engine or a replacement for effective reads.
type EditorStore interface {
	GetPermissionEditorSnapshot(context.Context, authz.Actor, uuid.UUID) (*company.PermissionEditorSnapshot, error)
	PatchPermissionEditor(context.Context, authz.Actor, uuid.UUID, company.PermissionEditorCommand) (*company.PermissionEditorSnapshot, error)
}

func editorActor(a authz.Actor, tenant *uuid.UUID) (authz.Actor, error) {
	if a.Type != authz.PrincipalUser || a.ID == uuid.Nil {
		return a, app.Forbidden("interactive administrator required")
	}
	if tenant == nil || *tenant == uuid.Nil {
		return a, app.BadRequest("selected tenant is required")
	}
	if a.TenantID != *tenant && !a.IsSuperAdmin {
		return a, app.Forbidden("tenant context differs from actor")
	}
	a.TenantID = *tenant
	return a, nil
}
func editorError(err error) error {
	if authz.IsAuthzError(err) {
		return app.FromAuthz(err)
	}
	return app.Internal(err)
}
func (s *Service) EditorSnapshot(ctx context.Context, a authz.Actor, tenant *uuid.UUID, user uuid.UUID) (*company.PermissionEditorSnapshot, error) {
	a, err := editorActor(a, tenant)
	if err != nil {
		return nil, err
	}
	port, ok := s.store.(EditorStore)
	if !ok {
		return nil, app.Internal(nilEditorPortError{})
	}
	out, err := port.GetPermissionEditorSnapshot(ctx, a, user)
	return out, editorError(err)
}
func (s *Service) PatchEditor(ctx context.Context, a authz.Actor, tenant *uuid.UUID, user uuid.UUID, cmd company.PermissionEditorCommand) (*company.PermissionEditorSnapshot, error) {
	a, err := editorActor(a, tenant)
	if err != nil {
		return nil, err
	}
	if err = cmd.ExpectedRevision.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	if cmd.ExpectedRevision.UserID != user || cmd.ExpectedRevision.TenantID != a.TenantID {
		return nil, app.Conflict("permission observation belongs to another target")
	}
	if err = cmd.Patch.Validate(); err != nil {
		return nil, app.BadRequest(err.Error())
	}
	port, ok := s.store.(EditorStore)
	if !ok {
		return nil, app.Internal(nilEditorPortError{})
	}
	out, err := port.PatchPermissionEditor(ctx, a, user, cmd)
	return out, editorError(err)
}

type nilEditorPortError struct{}

func (nilEditorPortError) Error() string { return "versioned permission editor port unavailable" }

// ProfileCASStore extends the same service port; validation is reused through
// a persistence adapter whose only write is the formal transaction/CAS method.
type ProfileCASStore interface {
	UpdatePermissionProfileCAS(context.Context, authz.Actor, *models.PermissionProfile, string) (*models.PermissionProfile, error)
	DeletePermissionProfileCAS(context.Context, authz.Actor, uuid.UUID, string, []company.PermissionRevision) error
	GetPermissionProfileDeletionPreview(context.Context, authz.Actor, uuid.UUID) (*company.PermissionProfileDeletionPreview, error)
	AssignPermissionEditor(context.Context, authz.Actor, uuid.UUID, company.PermissionAssignmentCommand) (*company.PermissionEditorSnapshot, error)
}
type profileCASAdapter struct {
	Store
	port     ProfileCASStore
	actor    authz.Actor
	expected string
	result   *models.PermissionProfile
}

func (a *profileCASAdapter) UpdatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error {
	out, err := a.port.UpdatePermissionProfileCAS(ctx, a.actor, p, a.expected)
	if err == nil {
		a.result = out
		*p = *out
	}
	return editorError(err)
}
func (s *Service) UpdateProfileVersioned(ctx context.Context, a authz.Actor, tenant *uuid.UUID, id uuid.UUID, expected string, in UpdateInput) (*models.PermissionProfile, error) {
	a, err := editorActor(a, tenant)
	if err != nil {
		return nil, err
	}
	if expected == "" {
		return nil, app.Conflict("profile revision is required; reload before editing")
	}
	port, ok := s.store.(ProfileCASStore)
	if !ok {
		return nil, app.Internal(nilEditorPortError{})
	}
	adapter := &profileCASAdapter{Store: s.store, port: port, actor: a, expected: expected}
	child := *s
	child.store = adapter
	return child.UpdateProfile(ctx, a, tenant, id, in)
}
func (s *Service) ProfileDeletionPreview(ctx context.Context, a authz.Actor, tenant *uuid.UUID, id uuid.UUID) (*company.PermissionProfileDeletionPreview, error) {
	a, err := editorActor(a, tenant)
	if err != nil {
		return nil, err
	}
	port, ok := s.store.(ProfileCASStore)
	if !ok {
		return nil, app.Internal(nilEditorPortError{})
	}
	out, err := port.GetPermissionProfileDeletionPreview(ctx, a, id)
	return out, editorError(err)
}
func (s *Service) DeleteProfileVersioned(ctx context.Context, a authz.Actor, tenant *uuid.UUID, id uuid.UUID, expected string, members []company.PermissionRevision) error {
	a, err := editorActor(a, tenant)
	if err != nil {
		return err
	}
	if expected == "" {
		return app.Conflict("profile revision and impact confirmation required")
	}
	port, ok := s.store.(ProfileCASStore)
	if !ok {
		return app.Internal(nilEditorPortError{})
	}
	return editorError(port.DeletePermissionProfileCAS(ctx, a, id, expected, members))
}
func (s *Service) AssignEditor(ctx context.Context, a authz.Actor, tenant *uuid.UUID, id uuid.UUID, cmd company.PermissionAssignmentCommand) (*company.PermissionEditorSnapshot, error) {
	a, err := editorActor(a, tenant)
	if err != nil {
		return nil, err
	}
	port, ok := s.store.(ProfileCASStore)
	if !ok {
		return nil, app.Internal(nilEditorPortError{})
	}
	out, err := port.AssignPermissionEditor(ctx, a, id, cmd)
	return out, editorError(err)
}
