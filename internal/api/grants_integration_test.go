package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"tabmail/internal/models"
)

// dataList extracts "data" from a JSON response as a slice, treating null as empty.
func dataList(t *testing.T, resp map[string]any) []any {
	t.Helper()
	v, ok := resp["data"]
	if !ok || v == nil {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected data to be an array, got %T: %#v", v, v)
	}
	return items
}

// ================================================================
// A. Mailbox Grants
// ================================================================

func TestMailboxGrants_CRUD(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	// Seed a user in the tenant to be the grant principal.
	principal := seedUserForTest(t, st, tenantID, models.RoleUser)

	// Seed a mailbox.
	zoneID := findTenantZone(t, st, tenantID)
	mailboxID := uuid.New()
	st.SeedMailbox(&models.Mailbox{
		ID:             mailboxID,
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      "grants-test",
		ResolvedDomain: "mail.test",
		FullAddress:    "grants-test@mail.test",
		AccessMode:     models.AccessPublic,
	})

	headers := adminHeaders(t, st, tenantID)

	// 1. Create a mailbox grant.
	createResp := doJSON(t, router, http.MethodPost,
		"/api/v1/mailboxes/"+mailboxID.String()+"/grants",
		map[string]any{
			"principal_type": "user",
			"principal_id":   principal.ID.String(),
			"role":           "reader",
		}, headers)
	data := createResp["data"].(map[string]any)
	grantID := data["id"].(string)
	if grantID == "" {
		t.Fatal("expected grant id in response")
	}
	if data["role"] != "reader" {
		t.Fatalf("expected role=reader, got %v", data["role"])
	}
	if data["principal_type"] != "user" {
		t.Fatalf("expected principal_type=user, got %v", data["principal_type"])
	}

	// 2. List grants and verify the created grant appears.
	listResp := doJSON(t, router, http.MethodGet,
		"/api/v1/mailboxes/"+mailboxID.String()+"/grants",
		nil, headers)
	items := dataList(t, listResp)
	if len(items) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(items))
	}
	firstGrant := items[0].(map[string]any)
	if firstGrant["id"] != grantID {
		t.Fatalf("expected grant id %s, got %v", grantID, firstGrant["id"])
	}

	// 3. Delete the grant.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/mailboxes/"+mailboxID.String()+"/grants/"+grantID, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 4. List grants again — should be empty.
	listResp2 := doJSON(t, router, http.MethodGet,
		"/api/v1/mailboxes/"+mailboxID.String()+"/grants",
		nil, headers)
	items2 := dataList(t, listResp2)
	if len(items2) != 0 {
		t.Fatalf("expected 0 grants after delete, got %d", len(items2))
	}
}

func TestMailboxGrants_TenantIsolation(t *testing.T) {
	st, obj, tenantAID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	// Create tenant B.
	tenantBID := uuid.New()
	planID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	st.SeedTenant(&models.Tenant{ID: tenantBID, Name: "tenant-b", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         uuid.New(),
		TenantID:   tenantBID,
		Domain:     "tenant-b.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=b",
	})
	zoneBID := findTenantZone(t, st, tenantBID)
	mailboxBID := uuid.New()
	st.SeedMailbox(&models.Mailbox{
		ID:             mailboxBID,
		TenantID:       tenantBID,
		ZoneID:         zoneBID,
		LocalPart:      "box-b",
		ResolvedDomain: "tenant-b.test",
		FullAddress:    "box-b@tenant-b.test",
		AccessMode:     models.AccessPublic,
	})

	// Authenticate as tenant A tenant_admin (not platform admin).
	adminA := seedUserForTest(t, st, tenantAID, models.RoleTenantAdmin)
	tokenA := issueAccessTokenForExistingUser(t, adminA)
	headersA := map[string]string{"Authorization": "Bearer " + tokenA}

	// Try to list grants on tenant B's mailbox — should be forbidden.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/mailboxes/"+mailboxBID.String()+"/grants", nil)
	for k, v := range headersA {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant list, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Try to create a grant on tenant B's mailbox — should be forbidden.
	principalA := seedUserForTest(t, st, tenantAID, models.RoleUser)
	body, _ := json.Marshal(map[string]any{
		"principal_type": "user",
		"principal_id":   principalA.ID.String(),
		"role":           "reader",
	})
	req = httptest.NewRequest(http.MethodPost,
		"/api/v1/mailboxes/"+mailboxBID.String()+"/grants",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headersA {
		req.Header.Set(k, v)
	}
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant create, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMailboxGrants_InvalidInput(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID)
	mailboxID := uuid.New()
	st.SeedMailbox(&models.Mailbox{
		ID:             mailboxID,
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      "invalid-input-test",
		ResolvedDomain: "mail.test",
		FullAddress:    "invalid-input-test@mail.test",
		AccessMode:     models.AccessPublic,
	})

	headers := adminHeaders(t, st, tenantID)

	cases := []struct {
		name string
		body map[string]any
	}{
		{
			name: "invalid principal_type",
			body: map[string]any{
				"principal_type": "invalid",
				"principal_id":   uuid.New().String(),
				"role":           "reader",
			},
		},
		{
			name: "empty principal_id",
			body: map[string]any{
				"principal_type": "user",
				"principal_id":   uuid.Nil.String(),
				"role":           "reader",
			},
		},
		{
			name: "invalid role",
			body: map[string]any{
				"principal_type": "user",
				"principal_id":   uuid.New().String(),
				"role":           "superadmin",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost,
				"/api/v1/mailboxes/"+mailboxID.String()+"/grants",
				bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d body=%s", tc.name, rr.Code, rr.Body.String())
			}
		})
	}

	// Cross-tenant principal: create a user in a different tenant.
	otherTenantID := uuid.New()
	st.SeedTenant(&models.Tenant{ID: otherTenantID, Name: "other-tenant", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000010")})
	foreignUser := seedUserForTest(t, st, otherTenantID, models.RoleUser)

	t.Run("cross-tenant principal", func(t *testing.T) {
		bodyBytes, _ := json.Marshal(map[string]any{
			"principal_type": "user",
			"principal_id":   foreignUser.ID.String(),
			"role":           "reader",
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/mailboxes/"+mailboxID.String()+"/grants",
			bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for cross-tenant principal, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

// ================================================================
// B. Zone Grants (via DomainHandler /domains/{id}/grants)
// ================================================================

func TestZoneGrants_CRUD(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	principal := seedUserForTest(t, st, tenantID, models.RoleUser)
	zoneID := findTenantZone(t, st, tenantID)
	headers := adminHeaders(t, st, tenantID)

	// 1. Create a zone grant with role "editor".
	createResp := doJSON(t, router, http.MethodPost,
		"/api/v1/domains/"+zoneID.String()+"/grants",
		map[string]any{
			"principal_type": "user",
			"principal_id":   principal.ID.String(),
			"role":           "editor",
		}, headers)
	data := createResp["data"].(map[string]any)
	grantID := data["id"].(string)
	if grantID == "" {
		t.Fatal("expected zone grant id in response")
	}
	if data["role"] != "editor" {
		t.Fatalf("expected role=editor, got %v", data["role"])
	}

	// 2. List grants and verify.
	listResp := doJSON(t, router, http.MethodGet,
		"/api/v1/domains/"+zoneID.String()+"/grants",
		nil, headers)
	items := dataList(t, listResp)
	if len(items) != 1 {
		t.Fatalf("expected 1 zone grant, got %d", len(items))
	}

	// 3. Delete the grant.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/domains/"+zoneID.String()+"/grants/"+grantID, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 4. List again — should be empty.
	listResp2 := doJSON(t, router, http.MethodGet,
		"/api/v1/domains/"+zoneID.String()+"/grants",
		nil, headers)
	items2 := dataList(t, listResp2)
	if len(items2) != 0 {
		t.Fatalf("expected 0 zone grants after delete, got %d", len(items2))
	}
}

func TestZoneGrants_TenantIsolation(t *testing.T) {
	st, obj, tenantAID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	// Create tenant B with a zone.
	tenantBID := uuid.New()
	planID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	st.SeedTenant(&models.Tenant{ID: tenantBID, Name: "tenant-b-zone", PlanID: planID})
	zoneBID := uuid.New()
	st.SeedZone(&models.DomainZone{
		ID:         zoneBID,
		TenantID:   tenantBID,
		Domain:     "zone-b.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=zb",
	})

	// Authenticate as tenant A tenant_admin (not platform admin).
	adminA := seedUserForTest(t, st, tenantAID, models.RoleTenantAdmin)
	tokenA := issueAccessTokenForExistingUser(t, adminA)
	headersA := map[string]string{"Authorization": "Bearer " + tokenA}

	// Try to list zone grants on tenant B's zone.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/domains/"+zoneBID.String()+"/grants", nil)
	for k, v := range headersA {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	// Tenant isolation may manifest as 403, 404, or 200 with empty/null data
	// (the service layer filters zones outside the actor's tenant).
	switch rr.Code {
	case http.StatusForbidden, http.StatusNotFound:
		// Explicit denial — OK.
	case http.StatusOK:
		// Ensure no grants from tenant B are leaked.
		var body map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		items := dataList(t, body)
		if len(items) != 0 {
			t.Fatalf("expected empty list for cross-tenant zone grants, got %d items", len(items))
		}
	default:
		t.Fatalf("unexpected status for cross-tenant zone grant access, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestZoneGrants_RoleValidation(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID)
	headers := adminHeaders(t, st, tenantID)

	bodyBytes, _ := json.Marshal(map[string]any{
		"principal_type": "user",
		"principal_id":   uuid.New().String(),
		"role":           "superadmin",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/domains/"+zoneID.String()+"/grants",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid zone grant role, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ================================================================
// C. Send Identities
// ================================================================

func TestSendIdentities_CRUD(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID)
	headers := adminHeaders(t, st, tenantID)

	// 1. Create a send identity.
	createResp := doJSON(t, router, http.MethodPost, "/api/v1/send-identities",
		map[string]any{
			"zone_id": zoneID.String(),
			"address": "test@mail.test",
		}, headers)
	data := createResp["data"].(map[string]any)
	identityID := data["id"].(string)
	if identityID == "" {
		t.Fatal("expected send identity id in response")
	}
	if data["address"] != "test@mail.test" {
		t.Fatalf("expected address=test@mail.test, got %v", data["address"])
	}

	// 2. List send identities.
	listResp := doJSON(t, router, http.MethodGet, "/api/v1/send-identities",
		nil, headers)
	items := dataList(t, listResp)
	if len(items) != 1 {
		t.Fatalf("expected 1 send identity, got %d", len(items))
	}

	// 3. Delete the send identity.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/send-identities/"+identityID, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 4. List again — should be empty.
	listResp2 := doJSON(t, router, http.MethodGet, "/api/v1/send-identities",
		nil, headers)
	items2 := dataList(t, listResp2)
	if len(items2) != 0 {
		t.Fatalf("expected 0 send identities after delete, got %d", len(items2))
	}
}

func TestSendIdentities_AdminOnly(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID)

	// Authenticate as a regular user (non-admin).
	regularUser := seedUserForTest(t, st, tenantID, models.RoleUser)
	token := issueAccessTokenForExistingUser(t, regularUser)
	userHeaders := map[string]string{"Authorization": "Bearer " + token}

	// Create should be forbidden.
	bodyBytes, _ := json.Marshal(map[string]any{
		"zone_id": zoneID.String(),
		"address": "nope@mail.test",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send-identities",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range userHeaders {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Create as admin first, then try to delete as regular user.
	adminHdrs := adminHeaders(t, st, tenantID)
	createResp := doJSON(t, router, http.MethodPost, "/api/v1/send-identities",
		map[string]any{
			"zone_id": zoneID.String(),
			"address": "admin-created@mail.test",
		}, adminHdrs)
	identityID := createResp["data"].(map[string]any)["id"].(string)

	req = httptest.NewRequest(http.MethodDelete,
		"/api/v1/send-identities/"+identityID, nil)
	for k, v := range userHeaders {
		req.Header.Set(k, v)
	}
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin delete, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSendIdentities_DomainMismatch(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID) // zone domain is "mail.test"
	headers := adminHeaders(t, st, tenantID)

	// Address domain does not match zone domain.
	bodyBytes, _ := json.Marshal(map[string]any{
		"zone_id": zoneID.String(),
		"address": "wrong@different-domain.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send-identities",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for domain mismatch, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSendIdentities_TenantIsolation(t *testing.T) {
	st, obj, tenantAID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	// Create tenant B with a zone.
	tenantBID := uuid.New()
	planID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	st.SeedTenant(&models.Tenant{ID: tenantBID, Name: "tenant-b-si", PlanID: planID})
	zoneBID := uuid.New()
	st.SeedZone(&models.DomainZone{
		ID:         zoneBID,
		TenantID:   tenantBID,
		Domain:     "si-b.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=si-b",
	})

	// Use platform admin impersonating tenant A — this satisfies the admin check
	// but the zone belongs to tenant B, so the handler should reject it.
	headersA := adminHeaders(t, st, tenantAID)

	// Try to create a send identity for zone belonging to tenant B.
	bodyBytes, _ := json.Marshal(map[string]any{
		"zone_id": zoneBID.String(),
		"address": "cross@si-b.test",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/send-identities",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headersA {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-tenant zone identity, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ================================================================
// D. Send-As Grants
// ================================================================

func TestSendAsGrants_CRUD(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	principal := seedUserForTest(t, st, tenantID, models.RoleUser)
	zoneID := findTenantZone(t, st, tenantID)
	headers := adminHeaders(t, st, tenantID)

	// Create a send identity first.
	createSIResp := doJSON(t, router, http.MethodPost, "/api/v1/send-identities",
		map[string]any{
			"zone_id": zoneID.String(),
			"address": "sendas@mail.test",
		}, headers)
	identityID := createSIResp["data"].(map[string]any)["id"].(string)

	// 1. Create a send-as grant.
	createResp := doJSON(t, router, http.MethodPost,
		"/api/v1/send-identities/"+identityID+"/grants",
		map[string]any{
			"principal_type": "user",
			"principal_id":   principal.ID.String(),
			"daily_quota":    100,
		}, headers)
	grantData := createResp["data"].(map[string]any)
	grantID := grantData["id"].(string)
	if grantID == "" {
		t.Fatal("expected send-as grant id in response")
	}
	if grantData["daily_quota"].(float64) != 100 {
		t.Fatalf("expected daily_quota=100, got %v", grantData["daily_quota"])
	}

	// 2. List send-as grants for this identity.
	listResp := doJSON(t, router, http.MethodGet,
		"/api/v1/send-identities/"+identityID+"/grants",
		nil, headers)
	items := dataList(t, listResp)
	if len(items) != 1 {
		t.Fatalf("expected 1 send-as grant, got %d", len(items))
	}

	// 3. Delete the send-as grant.
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/send-identities/"+identityID+"/grants/"+grantID, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// 4. List again — should be empty.
	listResp2 := doJSON(t, router, http.MethodGet,
		"/api/v1/send-identities/"+identityID+"/grants",
		nil, headers)
	items2 := dataList(t, listResp2)
	if len(items2) != 0 {
		t.Fatalf("expected 0 send-as grants after delete, got %d", len(items2))
	}
}

func TestSendAsGrants_CrossTenantPrincipal(t *testing.T) {
	st, obj, tenantID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	zoneID := findTenantZone(t, st, tenantID)
	headers := adminHeaders(t, st, tenantID)

	// Create a send identity.
	createSIResp := doJSON(t, router, http.MethodPost, "/api/v1/send-identities",
		map[string]any{
			"zone_id": zoneID.String(),
			"address": "cross-principal@mail.test",
		}, headers)
	identityID := createSIResp["data"].(map[string]any)["id"].(string)

	// Create a user in a different tenant.
	otherTenantID := uuid.New()
	st.SeedTenant(&models.Tenant{ID: otherTenantID, Name: "other-sa", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000010")})
	foreignUser := seedUserForTest(t, st, otherTenantID, models.RoleUser)

	// Try to grant send-as to a cross-tenant user.
	bodyBytes, _ := json.Marshal(map[string]any{
		"principal_type": "user",
		"principal_id":   foreignUser.ID.String(),
		"daily_quota":    50,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/send-identities/"+identityID+"/grants",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-tenant principal, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSendAsGrants_TenantIsolation(t *testing.T) {
	st, obj, tenantAID := seededStores(t)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = rdb.Close() })
	router := testRouter(st, obj, rdb)

	// Create a send identity in tenant A.
	zoneAID := findTenantZone(t, st, tenantAID)
	headersA := adminHeaders(t, st, tenantAID)
	createResp := doJSON(t, router, http.MethodPost, "/api/v1/send-identities",
		map[string]any{
			"zone_id": zoneAID.String(),
			"address": "isolation@mail.test",
		}, headersA)
	identityAID := createResp["data"].(map[string]any)["id"].(string)

	// Create tenant B.
	tenantBID := uuid.New()
	planID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	st.SeedTenant(&models.Tenant{ID: tenantBID, Name: "tenant-b-sa", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         uuid.New(),
		TenantID:   tenantBID,
		Domain:     "sa-b.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=sa-b",
	})

	// Authenticate as tenant B tenant_admin (not platform admin).
	adminB := seedUserForTest(t, st, tenantBID, models.RoleTenantAdmin)
	tokenB := issueAccessTokenForExistingUser(t, adminB)
	headersB := map[string]string{"Authorization": "Bearer " + tokenB}

	// Try to list grants on tenant A's identity — should be 404.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/send-identities/"+identityAID+"/grants", nil)
	for k, v := range headersB {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant identity grants, got %d body=%s", rr.Code, rr.Body.String())
	}
}
