package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func TestSendIdentityIsAuthoritativeForOutboundSubmission(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewFakeStore()
	publicID := uuid.MustParse(publicTenantIDForTests)
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	st.SeedTenant(&models.Tenant{ID: publicID, Name: "public"})
	st.SeedTenant(tenant)
	user := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "sender@example.test", Role: models.RoleUser, IsActive: true}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	zoneOwner := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &zoneOwner, Domain: "shared.example.test", IsVerified: true, MXVerified: true}
	st.SeedZone(zone)

	svc := outbound.NewService(config.Outbound{Enabled: false}, st, nil, zerolog.Nop())
	h := NewOutboundHandler(svc, st, zerolog.Nop())
	body := `{"from":"Sender <NOONE@SHARED.EXAMPLE.TEST>","to":["Recipient <RCPT@EXAMPLE.ORG>"],"subject":"hello","text_body":"body"}`

	rr := doIdentitySend(t, st, h, user, body)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "authorized send identity") {
		t.Fatalf("missing identity expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	identity := &models.SendIdentity{TenantID: tenant.ID, ZoneID: zone.ID, Address: "*@shared.example.test", IdentityType: models.SendIdentityDomainWildcard, Verified: false}
	if err := st.CreateSendIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	rr = doIdentitySend(t, st, h, user, body)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "send identity is not verified") {
		t.Fatalf("unverified identity expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}

	if err := st.UpdateSendIdentitiesVerifiedByZone(ctx, zone.ID, true); err != nil {
		t.Fatal(err)
	}
	rr = doIdentitySend(t, st, h, user, body)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "outbound sending is disabled") {
		t.Fatalf("verified identity should pass authorization and canonicalization, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestEnvelopeAddressCanonicalizationAndGroupDeduplication(t *testing.T) {
	to, err := normalizeEnvelopeAddresses([]string{
		"Alice <ALICE@Example.COM>",
		"alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := normalizeEnvelopeAddresses([]string{
		"Bob <BOB@example.com>",
		"alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	bcc, err := normalizeEnvelopeAddresses([]string{
		"bob@example.com",
		"Carol <CAROL@example.com>",
	})
	if err != nil {
		t.Fatal(err)
	}
	to, cc, bcc = dedupeRecipientGroups(to, cc, bcc)
	if len(to) != 1 || to[0] != "alice@example.com" {
		t.Fatalf("unexpected To group: %v", to)
	}
	if len(cc) != 1 || cc[0] != "bob@example.com" {
		t.Fatalf("unexpected Cc group: %v", cc)
	}
	if len(bcc) != 1 || bcc[0] != "carol@example.com" {
		t.Fatalf("unexpected Bcc group: %v", bcc)
	}
}

func doIdentitySend(t *testing.T, st *testutil.FakeStore, h *OutboundHandler, user *models.User, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := authn.IssueAccessToken(outboundTestJWTSecret, user)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(h.Send)).ServeHTTP(rr, req)
	return rr
}
