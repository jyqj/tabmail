package handlers

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

func TestP0EmployeeCannotUseUnassignedMailboxFrom(t *testing.T) {
	f := newOutboundAccessFixture(t)
	zone := &models.DomainZone{ID: uuid.New(), TenantID: f.tenantID, Domain: "company.test", IsVerified: true, MXVerified: true}
	f.st.SeedZone(zone)
	f.st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, FullAddress: "finance@company.test", LocalPart: "finance", ResolvedDomain: zone.Domain})
	h := NewOutboundHandler(outbound.NewService(config.Outbound{Enabled: true}, f.st, testutil.DeniedTemplateGovernance{}, zerolog.Nop()), f.st, zerolog.Nop())
	rr := doOutboundHandlerRequest(t, f.st, func(w http.ResponseWriter, r *http.Request) {
		h.submitAuthorized(w, r, outboundSubmitInput{
			From: "finance@company.test",
			To:   []string{"recipient@example.test"},
			Subject: "test", TextBody: "test",
			Draft: &store.DraftConsumption{
				TenantID: f.tenantID,
				UserID:   f.userA.ID,
				ID:       uuid.New(),
				Revision: 1,
			},
		})
	}, http.MethodPost, "/api/v1/company/drafts/x/submit", nil, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unassigned employee From: got %d, want 403; %s", rr.Code, rr.Body.String())
	}
}
