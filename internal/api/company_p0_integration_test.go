package api_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"strings"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"testing"
)

type p0AuditFailure struct{ *testutil.FakeStore }

func (s p0AuditFailure) InsertAudit(context.Context, *models.AuditEntry) error {
	return errors.New("audit storage unavailable")
}
func TestP0RouterTenantBoundaryAndRequiredAudit(t *testing.T) {
	st, obj, tenant := seededStores(t)
	ctx := context.Background()
	admin := seedUserForTest(t, st, tenant, models.RoleAdmin)
	stranger := uuid.New()
	baseTenant, _ := st.GetTenant(ctx, tenant)
	st.SeedTenant(&models.Tenant{ID: stranger, PlanID: baseTenant.PlanID, Name: "other"})
	zone := uuid.New()
	st.SeedZone(&models.DomainZone{ID: zone, TenantID: stranger, Domain: "other.test", IsVerified: true, MXVerified: true})
	other := &models.Mailbox{ID: uuid.New(), TenantID: stranger, ZoneID: zone, FullAddress: "private@other.test", AccessMode: models.AccessAPIKey}
	st.SeedMailbox(other)
	message := &models.Message{ID: uuid.New(), TenantID: stranger, ZoneID: zone, MailboxID: other.ID, Subject: "private", RawObjectKey: "private.eml"}
	st.SeedMessage(message)
	secret := "private-content-never-returned"
	if err := obj.Put(ctx, message.RawObjectKey, bytes.NewBufferString("Subject: Private\r\n\r\n"+secret), 0); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer redisClient.Close()
	router := testRouter(st, obj, redisClient)
	token := issueAccessTokenForExistingUser(t, admin)
	prefix := "/api/v1/mailbox/" + other.FullAddress
	for _, tc := range []struct{ method, path string }{
		{"GET", prefix}, {"GET", prefix + "/events"}, {"GET", prefix + "/" + message.ID.String()},
		{"GET", prefix + "/" + message.ID.String() + "/source"},
		{"POST", prefix + "/" + message.ID.String() + "/break-glass"},
		{"POST", prefix + "/" + message.ID.String() + "/break-glass/source"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"reason":"security regression"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if (rr.Code != http.StatusNotFound && rr.Code != http.StatusForbidden) || strings.Contains(rr.Body.String(), secret) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}
	own := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: findTenantZone(t, st, tenant), FullAddress: "owned@mail.test", AccessMode: models.AccessAPIKey}
	st.SeedMailbox(own)
	ownMessage := &models.Message{ID: uuid.New(), TenantID: tenant, MailboxID: own.ID, ZoneID: own.ZoneID, RawObjectKey: message.RawObjectKey}
	st.SeedMessage(ownMessage)
	failing := testRouter(p0AuditFailure{st}, obj, redisClient)
	for _, suffix := range []string{"/break-glass", "/break-glass/source"} {
		req := httptest.NewRequest("POST", "/api/v1/mailbox/"+own.FullAddress+"/"+ownMessage.ID.String()+suffix, strings.NewReader(`{"reason":"review"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		failing.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), secret) {
			t.Fatalf("audit failure: %d %s", rr.Code, rr.Body.String())
		}
	}
	// An explicit read grant, not the admin title, provides normal content access.
	if err := st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: tenant, MailboxID: own.ID, UserID: admin.ID, CanRead: true}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/mailbox/"+own.FullAddress+"/"+ownMessage.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	failing.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), secret) {
		t.Fatalf("ordinary granted read: %d %s", rr.Code, rr.Body.String())
	}
}
