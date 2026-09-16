package testutil

import (
	"context"

	"github.com/google/uuid"

	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// DeniedTemplateGovernance is an explicit outbound.TemplateGovernance stub for
// test assemblies whose store does not implement company.Repository. It
// reproduces the pre-refactor degraded verdict — every published-template
// lookup is denied — so wiring it in is a visible assembly decision, not a
// silent capability loss at send time.
type DeniedTemplateGovernance struct{}

func (DeniedTemplateGovernance) TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error) {
	return nil, "", "", authz.ErrForbidden("published template governance unavailable")
}
