package messageapp

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
	"testing"
)

func TestP0TenantAdminCannotResolveAnotherTenantMailbox(t *testing.T) {
	_, svc, _, mb := seededMessageService(t, models.AccessAPIKey, nil)
	v := Viewer{Tenant: &models.Tenant{ID: uuid.New()}, IsAdmin: true, AuthMode: "admin"}
	if _, err := svc.ResolveMailbox(context.Background(), mb.FullAddress, v); err == nil {
		t.Fatal("tenant admin resolved another tenant's private mailbox")
	}
}

type p0AuditFailure struct{ *testutil.FakeStore }

func (p0AuditFailure) InsertAudit(context.Context, *models.AuditEntry) error {
	return errors.New("injected audit failure")
}
func TestP0BreakGlassRequiresPersistedAudit(t *testing.T) {
	st, _, tenant, mb := seededMessageService(t, models.AccessAPIKey, nil)
	msg := &models.Message{ID: uuid.New(), TenantID: tenant.ID, MailboxID: mb.ID, ZoneID: mb.ZoneID}
	st.SeedMessage(msg)
	svc := NewService(p0AuditFailure{st}, testutil.NewMemoryObjectStore(), nil, nil, nil, policy.NamingFull, true, "secret", zerolog.Nop())
	v := Viewer{Tenant: tenant, IsAdmin: true, AuthMode: "admin"}
	detail, err := svc.BreakGlassRead(context.Background(), mb.FullAddress, msg.ID, v, "user:admin", "investigation")
	if err == nil || detail != nil {
		t.Fatalf("audit failure returned detail=%#v error=%v", detail, err)
	}
}
