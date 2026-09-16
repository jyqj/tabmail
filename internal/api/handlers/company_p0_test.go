package handlers

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
	"testing"
)

func TestP0EmployeeCannotUseUnassignedMailboxFrom(t *testing.T) {
	f := newOutboundAccessFixture(t)
	zone := &models.DomainZone{ID: uuid.New(), TenantID: f.tenantID, Domain: "company.test", IsVerified: true, MXVerified: true}
	f.st.SeedZone(zone)
	f.st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, FullAddress: "finance@company.test", LocalPart: "finance", ResolvedDomain: zone.Domain})
	h := NewOutboundHandler(outbound.NewService(config.Outbound{Enabled: true}, f.st, testutil.DeniedTemplateGovernance{}, zerolog.Nop()), f.st, zerolog.Nop())
	rr := doOutboundSendRequest(t, f.st, h, `{"from":"finance@company.test","to":["recipient@example.test"],"subject":"test","text_body":"test"}`, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unassigned employee From: got %d, want 403; %s", rr.Code, rr.Body.String())
	}
}
