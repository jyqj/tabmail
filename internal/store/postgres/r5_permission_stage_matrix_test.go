package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func r5StageRequirePG(t *testing.T) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("P1-080 stage acceptance requires an explicit owned TABMAIL_TEST_DB_DSN; missing runtime evidence is not a skip")
	}
}
func r5StageHTTP(t *testing.T, h http.Handler, token string, key bool, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, e := json.Marshal(body)
	must(t, e)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if key {
		req.Header.Set("X-API-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
func r5StageHTTPVerdict(t *testing.T, rr *httptest.ResponseRecorder, allow bool, positive int) {
	t.Helper()
	if allow {
		if rr.Code != positive {
			t.Fatalf("HTTP allow=%d want=%d body=%s", rr.Code, positive, rr.Body.String())
		}
		return
	}
	if rr.Code != 403 && rr.Code != 404 && rr.Code != 401 {
		t.Fatalf("HTTP denial=%d body=%s", rr.Code, rr.Body.String())
	}
}
func r5StagePatch(t *testing.T, f *companyFixture, canSend bool, mode string, ids []uuid.UUID) *company.PermissionEditorSnapshot {
	t.Helper()
	ctx := context.Background()
	old, e := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.employee.ID)
	must(t, e)
	s, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: company.PermissionPatch{CanSend: company.PermissionField[bool]{Present: true, Value: canSend}, DomainAccess: company.PermissionField[company.DomainAccess]{Present: true, Value: company.DomainAccess{Mode: mode, ZoneIDs: ids}}}})
	must(t, e)
	return s
}
func r5StageService(f *companyFixture, port int) *outbound.Service {
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: port, RelayTLS: "none", PollInterval: 5 * time.Millisecond, RetryDelay: time.Millisecond, MaxRetries: 1, BatchSize: 1}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(testutil.NewMemoryObjectStore())
	return svc
}
func r5StageQueued(t *testing.T, f *companyFixture, svc *outbound.Service, mb *models.Mailbox, k *models.TenantAPIKey, recipient string) *models.OutboundJob {
	t.Helper()
	a := f.u
	var keyID *uuid.UUID
	if k != nil {
		keyID = &k.ID
		a = authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, OwnerUserID: k.OwnerUserID, TenantWide: k.OwnerUserID == nil}
	}
	j, e := svc.Submit(context.Background(), outbound.SendRequest{Principal: &a, TenantID: f.tenant.ID, UserID: r5StageSenderUser(f, k), APIKeyID: keyID, SenderMailboxID: &mb.ID, ZoneID: mb.ZoneID, From: mb.FullAddress, To: []string{recipient}, Subject: "Synthetic P1-080 stage control", TextBody: "Synthetic permission stage evidence", IdempotencyKey: uuid.NewString()})
	must(t, e)
	return j
}
func r5StageSenderUser(f *companyFixture, k *models.TenantAPIKey) *uuid.UUID {
	if k != nil {
		return k.OwnerUserID
	}
	return &f.employee.ID
}

// Compare effective behavior only. Raw editor/profile/override presence must
// retain its exact SQL NULL/empty distinction and is never normalized here.
func r5StageSameEffective(a, b *models.EffectivePermission) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := *a, *b
	left.AllowedZoneIDs, right.AllowedZoneIDs = nil, nil
	left.DomainAccessMode, right.DomainAccessMode = "", ""
	if !reflect.DeepEqual(left, right) {
		return false
	}
	restrictA, zonesA := a.ZoneScope()
	restrictB, zonesB := b.ZoneScope()
	if restrictA != restrictB {
		return false
	}
	for _, id := range append(append([]uuid.UUID{uuid.Nil}, zonesA...), zonesB...) {
		if a.AllowsZone(id) != b.AllowsZone(id) {
			return false
		}
	}
	return true
}

func TestR5PermissionStageEffectiveSemanticComparison(t *testing.T) {
	zone, other := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name string
		a, b models.EffectivePermission
		want bool
	}{
		{"legacy-null-empty-all", models.EffectivePermission{}, models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{}}, true},
		{"legacy-explicit-all", models.EffectivePermission{}, models.EffectivePermission{DomainAccessMode: "all"}, true},
		{"none-is-not-all", models.EffectivePermission{DomainAccessMode: "none"}, models.EffectivePermission{}, false},
		{"empty-list-is-not-all", models.EffectivePermission{DomainAccessMode: "list"}, models.EffectivePermission{}, false},
		{"unknown-is-not-all", models.EffectivePermission{DomainAccessMode: "unknown"}, models.EffectivePermission{}, false},
		{"none-stale-zone-stays-denied", models.EffectivePermission{DomainAccessMode: "none", AllowedZoneIDs: []uuid.UUID{zone}}, models.EffectivePermission{DomainAccessMode: "none"}, true},
		{"list-order-semantic", models.EffectivePermission{DomainAccessMode: "list", AllowedZoneIDs: []uuid.UUID{zone, other}}, models.EffectivePermission{DomainAccessMode: "list", AllowedZoneIDs: []uuid.UUID{other, zone}}, true},
		{"list-content-not-equal", models.EffectivePermission{DomainAccessMode: "list", AllowedZoneIDs: []uuid.UUID{zone}}, models.EffectivePermission{DomainAccessMode: "list", AllowedZoneIDs: []uuid.UUID{other}}, false},
		{"scalar-send-not-equal", models.EffectivePermission{CanSend: true}, models.EffectivePermission{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeA, beforeB := tc.a, tc.b
			if got := r5StageSameEffective(&tc.a, &tc.b); got != tc.want {
				t.Fatalf("effective semantic equality=%t want=%t", got, tc.want)
			}
			if !reflect.DeepEqual(beforeA, tc.a) || !reflect.DeepEqual(beforeB, tc.b) {
				t.Fatal("semantic comparison mutated raw permission representation")
			}
		})
	}
}

func r5StageLegacyMailbox(t *testing.T, f *companyFixture, local string) *models.Mailbox {
	t.Helper()
	password, err := bcrypt.GenerateFromPassword([]byte("synthetic-legacy-stage-password"), bcrypt.MinCost)
	must(t, err)
	hash := string(password)
	mb := &models.Mailbox{TenantID: f.tenant.ID, ZoneID: f.zone.ID, LocalPart: local, ResolvedDomain: f.zone.Domain, FullAddress: local + "@" + f.zone.Domain, AccessMode: models.AccessToken, PasswordHash: &hash, Kind: "legacy"}
	must(t, f.st.CreateMailbox(context.Background(), mb))
	return mb
}

// Every row observes the same user/shared mailbox through canonical editor and
// /auth/me display, companyReadTx display, Router/JWT draft submit, the actual
// worker validation entry and the Router/JWT retry command. Inherit resolves a
// real profile, not a synthetic EffectivePermission mode named "inherit".
func TestR5PermissionStagePostgresTruthTable(t *testing.T) {
	r5StageRequirePG(t)
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	p.DailySendQuota = 0
	must(t, f.st.UpdatePermissionProfile(ctx, p))
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)
	otherZone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "other-stage.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, otherZone))
	for _, mode := range []string{"all", "list", "none", "inherit"} {
		for _, canSend := range []bool{false, true} {
			for _, zoneAllows := range []bool{false, true} {
				for _, grant := range []string{"none", "read-only", "send"} {
					t.Run(fmt.Sprintf("%s/send=%t/zone=%t/grant=%s", mode, canSend, zoneAllows, grant), func(t *testing.T) {
						// Real positive-control enqueue precedes every revocation; the old draft
						// and durable job therefore carry exactly the same principal/resource.
						r5StagePatch(t, f, true, "all", nil)
						must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
						draft := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{To: []string{"matrix@stage.test"}, Subject: "Stage matrix", TextBody: "Synthetic matrix"}}, 200))
						first := draftSubmitHTTP(t, h, token, draft.ID.String(), uuid.NewString(), draft.Revision)
						r5StageHTTPVerdict(t, first, true, 201)
						id := r3Data[struct {
							ID uuid.UUID `json:"id"`
						}](t, first).ID
						j, e := f.st.GetOutboundJob(ctx, id)
						must(t, e)
						pending := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", company.Draft{MailboxID: f.shared.ID, Payload: draft.Payload}, 200))
						selected := otherZone.ID
						if zoneAllows {
							selected = f.zone.ID
						}
						p.AllowedZoneIDs = []uuid.UUID{selected}
						must(t, f.st.UpdatePermissionProfile(ctx, p))
						ids := []uuid.UUID(nil)
						if mode == "list" {
							ids = []uuid.UUID{selected}
						}
						snapshot := r5StagePatch(t, f, canSend, mode, ids)
						must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: grant != "none", CanSend: grant == "send"}))
						canonical, e := f.st.EffectivePermission(ctx, f.employee.ID)
						must(t, e)
						if !r5StageSameEffective(canonical, snapshot.Effective) {
							t.Fatal("editor and canonical effective diverged")
						}
						display := r3Data[models.EffectivePermission](t, r3HTTP(t, h, token, "GET", "/api/v1/auth/me/permissions", nil, 200))
						if !r5StageSameEffective(canonical, &display) {
							t.Fatalf("HTTP effective diverged: %+v vs %+v", display, canonical)
						}
						scopeAllows := mode == "all" || mode != "none" && zoneAllows
						if canonical.CanSend != canSend || canonical.AllowsZone(f.zone.ID) != scopeAllows {
							t.Fatalf("canonical merge=%+v", canonical)
						}
						want := scopeAllows && canSend && grant == "send"
						// Pass a stale allow-all Actor to force companyReadTx to reload authority.
						stale := f.u
						stale.Permission = &models.EffectivePermission{CanSend: true}
						access, e := f.st.GetWorkMailbox(ctx, stale, f.shared.ID)
						if e != nil {
							if want {
								t.Fatalf("display denied allowed row: %v", e)
							}
						} else if access.CanSend != want {
							t.Fatalf("company display send=%t want=%t", access.CanSend, want)
						}
						rr := draftSubmitHTTP(t, h, token, pending.ID.String(), uuid.NewString(), pending.Revision)
						r5StageHTTPVerdict(t, rr, want, 201)
						e = outbound.ValidateJobAuthorization(ctx, f.st, f.st, j)
						if (e == nil) != want {
							t.Fatalf("worker verdict=%v want=%t", e, want)
						}
						e = outbound.ValidateRetryRequester(ctx, f.st, stale, j)
						if (e == nil) != want {
							t.Fatalf("stale requester verdict=%v want=%t", e, want)
						}
						_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead',attempts=1 WHERE id=$1`, j.ID)
						must(t, e)
						rr = r5StageHTTP(t, h, token, false, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil)
						r5StageHTTPVerdict(t, rr, want, 200)
						current, e := f.st.GetOutboundJob(ctx, j.ID)
						must(t, e)
						if !want && current.State != models.OutboundDead {
							t.Fatal("denied retry advanced the durable job")
						}
						if !want {
							var revision int
							must(t, f.pool.QueryRow(ctx, `SELECT revision FROM mail_drafts WHERE id=$1`, pending.ID).Scan(&revision))
							if revision != pending.Revision {
								t.Fatal("denied submit consumed or advanced draft")
							}
						}
					})
				}
			}
		}
	}
}

// Administrative JWT management is intentionally distinct from an API key
// owned by the same administrator. The latter always uses the owner's profile.
func TestR5PermissionStagePostgresAdminOwnedKeyBoundary(t *testing.T) {
	r5StageRequirePG(t)
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	p.DailySendQuota = 0
	must(t, f.st.UpdatePermissionProfile(ctx, p))
	f.admin.Role = models.RoleSuperAdmin
	must(t, f.st.UpdateUser(ctx, f.admin))
	f.a.Role = models.RoleSuperAdmin
	f.a.IsSuperAdmin = true
	// r5EditorSeed assigns the profile through a real command, but its older
	// f.employee observation predates that assignment. Reload before changing
	// the role: UpdateUser persists the complete PermissionProfileID field.
	currentEmployee, err := f.st.GetUser(ctx, f.employee.ID)
	must(t, err)
	f.employee = currentEmployee
	if f.employee.PermissionProfileID == nil || *f.employee.PermissionProfileID != p.ID {
		t.Fatal("admin-owned key fixture did not retain its assigned matrix profile")
	}
	f.employee.Role = models.RoleAdmin
	must(t, f.st.UpdateUser(ctx, f.employee))
	f.u.Role = models.RoleAdmin
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)
	k, _ := r5LegacyKey(t, f)
	_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read","send:write"]'::jsonb WHERE id=$1`, k.ID)
	must(t, e)
	k.Scopes = []string{"send:read", "send:write"}
	svc := r5StageService(f, 1)
	other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "admin-other-stage.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, other))
	for _, mode := range []string{"all", "list", "none", "inherit"} {
		for _, canSend := range []bool{false, true} {
			for _, zoneAllows := range []bool{false, true} {
				for _, grant := range []string{"none", "read-only", "send"} {
					t.Run(fmt.Sprintf("%s/send=%t/zone=%t/grant=%s", mode, canSend, zoneAllows, grant), func(t *testing.T) {
						r5StagePatch(t, f, true, "all", nil)
						must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
						baseline := r5StageQueued(t, f, svc, f.shared, k, "key-baseline@stage.test")
						selected := other.ID
						if zoneAllows {
							selected = f.zone.ID
						}
						p.AllowedZoneIDs = []uuid.UUID{selected}
						must(t, f.st.UpdatePermissionProfile(ctx, p))
						ids := []uuid.UUID(nil)
						if mode == "list" {
							ids = []uuid.UUID{selected}
						}
						editor := r5StagePatch(t, f, canSend, mode, ids)
						if editor.Profile == nil || editor.Profile.ID != p.ID {
							t.Fatalf("role fixture overwrote profile assignment: expected=%s actual=%+v", p.ID, editor.Profile)
						}
						must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: grant != "none", CanSend: grant == "send"}))
						adminWant := grant == "send"
						// A JWT admin's unlimited display does not invent an exact send_as grant.
						display := r3Data[models.EffectivePermission](t, r3HTTP(t, h, token, "GET", "/api/v1/auth/me/permissions", nil, 200))
						if !display.CanSend || !display.AllowsZone(f.zone.ID) || display.DailySendQuota != 0 {
							t.Fatalf("JWT admin effective display did not match credential exemption: %+v", display)
						}
						access, e := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
						must(t, e)
						if access.CanSend != adminWant {
							t.Fatal("JWT admin exact-mailbox boundary changed")
						}
						a := f.u
						j, e := svc.Submit(ctx, outbound.SendRequest{Principal: &a, TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.shared.ID, ZoneID: f.zone.ID, From: f.shared.FullAddress, To: []string{"admin@stage.test"}, Subject: "Admin boundary", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
						if (e == nil) != adminWant {
							t.Fatalf("JWT admin submit=%v want=%t", e, adminWant)
						}
						if j != nil {
							must(t, outbound.ValidateJobAuthorization(ctx, f.st, f.st, j))
							_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, j.ID)
							must(t, e)
							r5StageHTTPVerdict(t, r5StageHTTP(t, h, token, false, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil), true, 200)
						}
						keyActor := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, OwnerUserID: &f.employee.ID, Permission: &models.EffectivePermission{CanSend: true}}
						keyWant := canSend && (mode == "all" || mode != "none" && zoneAllows) && grant == "send"
						// Use the actual middleware with real PG for Key authority; /company and
						// /auth/me are deliberately JWT-only, not an invented Key display route.
						var observed authz.Actor
						authHandler := middleware.Auth(middleware.NewCachedAuthStore(f.st, nil), companyTestJWT, f.tenant.ID.String())(middleware.PermissionLoader(f.st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							observed = middleware.ActorFromContext(r.Context())
							w.WriteHeader(204)
						})))
						rr := r5StageHTTP(t, authHandler, "tm_content_"+k.ID.String(), true, "GET", "/", nil)
						if rr.Code != 204 || observed.Permission == nil || observed.Permission.CanSend != canSend || observed.Permission.AllowsZone(f.zone.ID) != (mode == "all" || mode != "none" && zoneAllows) || observed.IsTenantAdmin() {
							currentKey, keyErr := f.st.GetAPIKey(ctx, k.ID)
							must(t, keyErr)
							currentUser, userErr := f.st.GetUser(ctx, f.employee.ID)
							must(t, userErr)
							canonical, permissionErr := f.st.EffectivePermission(ctx, f.employee.ID)
							must(t, permissionErr)
							t.Fatalf("real Key authority=%+v permission=%+v status=%d expected send=%t zone=%t; owner=%+v key=%+v rawProfile=%+v rawOverrides=%+v canonical=%+v", observed, observed.Permission, rr.Code, canSend, mode == "all" || mode != "none" && zoneAllows, currentUser, currentKey, editor.Profile, editor.Overrides, canonical)
						}
						for _, err := range []error{outbound.ValidateJobAuthorization(ctx, f.st, f.st, baseline), outbound.ValidateRetryRequester(ctx, f.st, keyActor, baseline)} {
							if (err == nil) != keyWant {
								t.Fatalf("admin-owned key stale authority=%v want=%t", err, keyWant)
							}
						}
						_, e = svc.Submit(ctx, outbound.SendRequest{Principal: &keyActor, TenantID: f.tenant.ID, UserID: &f.employee.ID, APIKeyID: &k.ID, SenderMailboxID: &f.shared.ID, ZoneID: f.zone.ID, From: f.shared.FullAddress, To: []string{"key@stage.test"}, Subject: "Owned key", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
						if (e == nil) != keyWant {
							t.Fatalf("admin-owned key enqueue=%v want=%t", e, keyWant)
						}
						_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, baseline.ID)
						must(t, e)
						r5StageHTTPVerdict(t, r5StageHTTP(t, h, "tm_content_"+k.ID.String(), true, "POST", "/api/v1/outbound/"+baseline.ID.String()+"/retry", nil), keyWant, 200)
					})
				}
			}
		}
	}
}

func TestR5PermissionStagePostgresWorkerRevocation(t *testing.T) {
	r5StageRequirePG(t)
	for _, mutation := range []string{"can-send", "domain-none", "grant-send", "key-scope", "key-zone", "key-expire-at-db-now", "key-owner-reassign", "mailbox-owner-reassign"} {
		t.Run(mutation, func(t *testing.T) {
			f, _ := r5EditorSeed(t)
			ctx := context.Background()
			r5StagePatch(t, f, true, "all", nil)
			smtp := newLocalSMTP(t)
			port := smtp.ln.Addr().(*net.TCPAddr).Port
			svc := r5StageService(f, port)
			mb := f.personal
			if mutation == "grant-send" {
				mb = f.shared
				must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			}
			var key *models.TenantAPIKey
			if strings.HasPrefix(mutation, "key-") {
				key, _ = r5LegacyKey(t, f)
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read","send:write"]'::jsonb WHERE id=$1`, key.ID)
				must(t, e)
				key.Scopes = []string{"send:read", "send:write"}
			}
			positive := r5StageQueued(t, f, svc, mb, key, "positive@stage.test")
			workerCtx, cancel := context.WithCancel(ctx)
			svc.StartWorker(workerCtx)
			var positiveStopOnce sync.Once
			stopPositive := func() { positiveStopOnce.Do(func() { cancel(); svc.Stop() }) }
			if mutation == "key-zone" {
				// Registered after Start, before waitJob/DB assertions can Fatal.
				// LIFO cleanup runs cancel -> Stop (synchronous worker join)
				// before the earlier SMTP close/join and owned DB close/drop.
				// Stop has no deadline API: the outer runtime driver must bound
				// a stuck drain; do not detach it and falsely claim cleanup.
				t.Cleanup(stopPositive)
			}
			delivered := waitJob(t, f, positive.ID)
			if mutation == "key-zone" {
				stopPositive()
			} else {
				cancel()
				svc.Stop()
			}
			if delivered.State != models.OutboundSent {
				t.Fatalf("actual loopback positive state=%s error=%s", delivered.State, delivered.LastError)
			}
			smtp.mu.Lock()
			positiveCount := len(smtp.messages["positive@stage.test"])
			smtp.mu.Unlock()
			if positiveCount != 1 {
				t.Fatalf("positive SMTP DATA=%d", positiveCount)
			}
			queued := r5StageQueued(t, f, svc, mb, key, "revoked@stage.test")
			switch mutation {
			case "can-send":
				r5StagePatch(t, f, false, "all", nil)
			case "domain-none":
				r5StagePatch(t, f, true, "none", nil)
			case "grant-send":
				must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mb.ID, UserID: f.employee.ID, CanRead: true}))
			case "key-scope":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read"]'::jsonb WHERE id=$1`, key.ID)
				must(t, e)
			case "key-zone":
				other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "shrink-stage.test"}
				must(t, f.st.CreateZone(ctx, other))
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, key.ID, []uuid.UUID{other.ID})
				must(t, e)
			case "key-expire-at-db-now":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp() WHERE id=$1`, key.ID)
				must(t, e)
			case "key-owner-reassign":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$2 WHERE id=$1`, key.ID, f.other.ID)
				must(t, e)
			case "mailbox-owner-reassign":
				_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2 WHERE id=$1`, mb.ID, f.other.ID)
				must(t, e)
			}
			revokedWorker := r5StageService(f, port)
			revokedCtx, revokeCancel := context.WithCancel(ctx)
			revokedWorker.StartWorker(revokedCtx)
			var revokedStopOnce sync.Once
			stopRevoked := func() { revokedStopOnce.Do(func() { revokeCancel(); revokedWorker.Stop() }) }
			if mutation == "key-zone" {
				// Same guarantee for the second sequential role; successful-path
				// Stop and Fatal cleanup share one once-guard, never a second drain.
				t.Cleanup(stopRevoked)
			}
			current := waitJob(t, f, queued.ID)
			if mutation == "key-zone" {
				stopRevoked()
			} else {
				revokeCancel()
				revokedWorker.Stop()
			}
			if current.State != models.OutboundFailed && current.State != models.OutboundDead {
				t.Fatalf("revoked job state=%s", current.State)
			}
			if !strings.Contains(current.LastError, "authorization revoked") {
				t.Fatalf("non-authz terminal failure is not evidence: %s", current.LastError)
			}
			smtp.mu.Lock()
			attempted := smtp.rcpt["revoked@stage.test"]
			bodyCount := len(smtp.messages["revoked@stage.test"])
			smtp.mu.Unlock()
			if attempted != 0 || bodyCount != 0 {
				t.Fatalf("revoked worker reached SMTP: RCPT=%d DATA=%d", attempted, bodyCount)
			}
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), revokedWorker)
			token := r3Token(t, f.employee)
			isKey := key != nil
			if isKey {
				token = "tm_content_" + key.ID.String()
			}
			r5StageHTTPVerdict(t, r5StageHTTP(t, h, token, isKey, "POST", "/api/v1/outbound/"+queued.ID.String()+"/retry", nil), false, 200)
		})
	}
}

// Patch/assignment must use the existing companyAudit event protocol; no
// private effective/raw permission data belongs in the public event metadata.
func TestR5PermissionStagePostgresRequiredOutbox(t *testing.T) {
	r5StageRequirePG(t)
	for _, command := range []string{"patch", "assign", "profile-update", "profile-delete"} {
		for _, failure := range []string{"none", "audit", "outbox"} {
			t.Run(command+"/"+failure, func(t *testing.T) {
				f, p := r5EditorSeed(t)
				ctx := context.Background()
				before := r5EditorRead(t, f)
				selected := r5EditorSelectedProfile(t, f, "Outbox selected ")
				action := map[string]string{"patch": "permission.override.patch", "assign": "permission.profile.assign", "profile-update": "permission.profile.update", "profile-delete": "permission.profile.delete"}[command]
				if failure == "audit" {
					_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT stage_audit_failure CHECK(action<>'`+action+`') NOT VALID`)
					must(t, e)
				}
				if failure == "outbox" {
					_, e := f.pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT stage_outbox_failure CHECK(event_type<>'company.admin.changed' OR payload->'metadata'->>'action'<>'`+action+`') NOT VALID`)
					must(t, e)
				}
				stable := r5EditorState(t, f)
				var auditBefore, eventBefore int
				must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log WHERE action=$1),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'=$1)`, action).Scan(&auditBefore, &eventBefore))
				var e error
				switch command {
				case "patch":
					_, e = f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: before.Revision, Patch: r5EditorPatch(t, `{"can_send":false,"domain_access":{"mode":"none","zone_ids":[]}}`)})
				case "assign":
					_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: before.Revision, ProfileID: &selected.ID, ProfileRevision: &selected.Revision, Patch: r5EditorPatch(t, `{"can_send":false}`)})
				case "profile-update":
					desired := *before.Profile
					desired.CanSend = false
					_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, desired.Revision)
				case "profile-delete":
					preview, err := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
					must(t, err)
					e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
				}
				if failure != "none" {
					if e == nil {
						t.Fatal("required " + failure + " failure committed")
					}
					if r5EditorState(t, f) != stable {
						t.Fatal("required " + failure + " failure changed data/revision/audit/outbox")
					}
					return
				}
				must(t, e)
				var auditAfter, eventAfter int
				must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log WHERE action=$1),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'=$1)`, action).Scan(&auditAfter, &eventAfter))
				if auditAfter != auditBefore+1 || eventAfter != eventBefore+1 {
					t.Fatalf("success audit/event increments=%d/%d", auditAfter-auditBefore, eventAfter-eventBefore)
				}
				var raw []byte
				must(t, f.pool.QueryRow(ctx, `SELECT payload->'metadata' FROM outbox_events WHERE payload->'metadata'->>'action'=$1 ORDER BY created_at DESC LIMIT 1`, action).Scan(&raw))
				var metadata map[string]any
				must(t, json.Unmarshal(raw, &metadata))
				target := f.employee.ID.String()
				kind := "user"
				if strings.HasPrefix(command, "profile-") {
					target = p.ID.String()
					kind = "permission_profile"
				}
				if len(metadata) != 3 || metadata["action"] != action || metadata["resource_type"] != kind || metadata["resource_id"] != target {
					t.Fatalf("event private/incorrect metadata=%s", raw)
				}
			})
		}
	}
}

func TestR5PermissionStagePostgresNoopProducesNoEvent(t *testing.T) {
	r5StageRequirePG(t)
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	before := r5EditorRead(t, f)
	stable := r5EditorState(t, f)
	_, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: before.Revision})
	must(t, e)
	if r5EditorState(t, f) != stable {
		t.Fatal("empty patch produced mutation/event")
	}
	_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: before.Revision, ProfileID: &p.ID, ProfileRevision: &before.Profile.Revision})
	must(t, e)
	if r5EditorState(t, f) != stable {
		t.Fatal("same-profile empty assignment produced mutation/event")
	}
}

func TestR5PermissionStagePostgresOwnerlessKeyScope(t *testing.T) {
	r5StageRequirePG(t)
	f, _ := r5EditorSeed(t)
	ctx := context.Background()
	svc := r5StageService(f, 1)
	legacy := r5StageLegacyMailbox(t, f, "legacy-stage")
	other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "ownerless-other.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, other))
	key := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "ownerless scope fixture", Scopes: []string{"send:read", "send:write"}}
	key.KeyHash = company.Hash("tm_ownerless_" + key.ID.String())
	must(t, f.st.CreateAPIKey(ctx, key))
	a := authz.Actor{Type: authz.PrincipalAPIKey, ID: key.ID, TenantID: f.tenant.ID, TenantWide: true}
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	for _, scope := range []string{"all", "matching-zone", "different-zone", "read-only"} {
		for _, kind := range []string{"legacy", "personal", "shared"} {
			t.Run(scope+"/"+kind, func(t *testing.T) {
				ids := []uuid.UUID(nil)
				if scope == "matching-zone" {
					ids = []uuid.UUID{f.zone.ID}
				}
				if scope == "different-zone" {
					ids = []uuid.UUID{other.ID}
				}
				scopes := []string{"send:read", "send:write"}
				if scope == "read-only" {
					scopes = []string{"send:read"}
				}
				raw, _ := json.Marshal(scopes)
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes=$2,allowed_zone_ids=$3 WHERE id=$1`, key.ID, raw, ids)
				must(t, e)
				mb := legacy
				if kind == "personal" {
					mb = f.personal
				}
				if kind == "shared" {
					mb = f.shared
				}
				want := kind == "legacy" && scope != "different-zone" && scope != "read-only"
				resolved, resolveErr := outbound.ResolveSendAuthorization(ctx, f.st, a, f.tenant.ID, mb.FullAddress, false)
				must(t, resolveErr)
				if resolved.Mailbox == nil || resolved.Mailbox.ID != mb.ID {
					t.Fatal("ownerless fixture did not resolve its real canonical address")
				}
				// Ownerless legacy-address submissions have no employee mailbox provenance.
				// This is the existing integration boundary, not permission to impersonate
				// a company personal/shared mailbox (even with a manufactured grant).
				j, e := svc.Submit(ctx, outbound.SendRequest{Principal: &a, TenantID: f.tenant.ID, APIKeyID: &key.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"ownerless@stage.test"}, Subject: "Ownerless scope", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
				if (e == nil) != want {
					t.Fatalf("ownerless enqueue=%v want=%t", e, want)
				}
				probe := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, SenderKeyID: &key.ID, MailFrom: mb.FullAddress}
				if err := outbound.ValidateJobAuthorization(ctx, f.st, f.st, probe); (err == nil) != want {
					t.Fatalf("ownerless worker=%v want=%t", err, want)
				}
				if j != nil {
					must(t, outbound.ValidateRetryRequester(ctx, f.st, a, j))
					_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, j.ID)
					must(t, e)
					r5StageHTTPVerdict(t, r5StageHTTP(t, h, "tm_ownerless_"+key.ID.String(), true, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil), true, 200)
					if scope == "all" && kind == "legacy" {
						smtp := newLocalSMTP(t)
						worker := r5StageService(f, smtp.ln.Addr().(*net.TCPAddr).Port)
						workerCtx, cancel := context.WithCancel(ctx)
						worker.StartWorker(workerCtx)
						sent := waitJob(t, f, j.ID)
						cancel()
						worker.Stop()
						if sent.State != models.OutboundSent {
							t.Fatalf("ownerless legacy loopback positive failed: %s %s", sent.State, sent.LastError)
						}
						smtp.mu.Lock()
						n := len(smtp.messages["ownerless@stage.test"])
						smtp.mu.Unlock()
						if n != 1 {
							t.Fatalf("ownerless legacy SMTP DATA=%d", n)
						}
					}
				}
			})
		}
	}
}

// This originated as a real RED contract witness; the expectation stays strict.
// The canonical resolver supplies the real legacy mailbox ID; no normal job is
// edited to erase provenance. A trusted integration enqueue is an existing
// Service entry, and both worker/requester checks read the persisted generation.
func TestR5PermissionStagePostgresOwnerlessSenderProvenanceConsistency(t *testing.T) {
	r5StageRequirePG(t)
	f := seedCompany(t)
	ctx := context.Background()
	mb := r5StageLegacyMailbox(t, f, "provenance-stage")
	k := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "Ownerless provenance witness", Scopes: []string{"send:read", "send:write"}}
	k.KeyHash = company.Hash("tm_provenance_" + k.ID.String())
	must(t, f.st.CreateAPIKey(ctx, k))
	actor := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, TenantWide: true}
	res, e := outbound.ResolveSendAuthorization(ctx, f.st, actor, f.tenant.ID, mb.FullAddress, false)
	must(t, e)
	must(t, res.WorkerFailure())
	if res.Mailbox == nil || res.Mailbox.ID != mb.ID {
		t.Fatal("formal resolver lost provenance")
	}
	smtp := newLocalSMTP(t)
	svc := r5StageService(f, smtp.ln.Addr().(*net.TCPAddr).Port)
	j, e := svc.Submit(ctx, outbound.SendRequest{TenantID: f.tenant.ID, APIKeyID: &k.ID, SenderMailboxID: &res.Mailbox.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"provenance@stage.test"}, Subject: "Ownerless provenance witness", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
	must(t, e)
	persisted, e := f.st.GetOutboundJob(ctx, j.ID)
	must(t, e)
	if persisted.SenderMailboxID == nil || *persisted.SenderMailboxID != mb.ID || persisted.SenderUserID != nil || persisted.SenderKeyID == nil || *persisted.SenderKeyID != k.ID {
		t.Fatal("fixture did not preserve canonical ownerless provenance")
	}
	workerErr := outbound.ValidateJobAuthorization(ctx, f.st, f.st, persisted)
	requesterErr := outbound.ValidateRetryRequester(ctx, f.st, actor, persisted)
	workerCtx, cancel := context.WithCancel(ctx)
	svc.StartWorker(workerCtx)
	delivered := waitJob(t, f, j.ID)
	cancel()
	svc.Stop()
	if delivered.State != models.OutboundSent {
		t.Fatalf("actual ownerless provenance worker positive failed: %s %s", delivered.State, delivered.LastError)
	}
	smtp.mu.Lock()
	dataCount := len(smtp.messages["provenance@stage.test"])
	smtp.mu.Unlock()
	if dataCount != 1 {
		t.Fatalf("actual ownerless provenance SMTP DATA=%d", dataCount)
	}
	// The retry witness has pending recipient facts, not a forged retry of
	// already-accepted recipients. It preserves the same canonical provenance.
	retryJob, e := svc.Submit(ctx, outbound.SendRequest{TenantID: f.tenant.ID, APIKeyID: &k.ID, SenderMailboxID: &res.Mailbox.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"provenance@stage.test"}, Subject: "Ownerless provenance witness", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, retryJob.ID)
	must(t, e)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	retryHTTP := r5StageHTTP(t, h, "tm_provenance_"+k.ID.String(), true, "POST", "/api/v1/outbound/"+retryJob.ID.String()+"/retry", nil)
	if (workerErr == nil) != (requesterErr == nil) {
		t.Fatalf("PRINCIPAL-OWNERLESS-SENDER-PROVENANCE-DIVERGENCE: persisted same-key/same-legacy-mailbox worker=%v retry=%v actual SMTP DATA=%d retry HTTP=%d body=%s", workerErr, requesterErr, dataCount, retryHTTP.Code, retryHTTP.Body.String())
	}
	if workerErr != nil {
		t.Fatalf("ownerless legacy positive control failed: %v", workerErr)
	}
	if retryHTTP.Code != http.StatusOK {
		t.Fatalf("requester permitted but formal retry refused: %d %s", retryHTTP.Code, retryHTTP.Body.String())
	}
	// A successful retry returns only the ordinary full-ledger aggregate.
	// Ownerless sending authority never becomes content/recipient authority.
	receipt := r3Data[company.OutboundReceipt](t, retryHTTP)
	if receipt.ID != retryJob.ID || receipt.TenantID == nil || *receipt.TenantID != f.tenant.ID || receipt.State != models.OutboundPending || receipt.Progress.Completeness != "known" || receipt.Progress.Counts == nil || *receipt.Progress.Counts != (company.OutboundReceiptCounts{Total: 1, Pending: 1}) || receipt.Capabilities == nil || receipt.Capabilities.ViewContent || receipt.Capabilities.Retry {
		t.Fatal("ownerless retry lost safe aggregate or gained content authority")
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	must(t, json.Unmarshal(retryHTTP.Body.Bytes(), &envelope))
	allowed := map[string]bool{"id": true, "tenant_id": true, "state": true, "status": true, "progress": true, "created_at": true, "updated_at": true, "attempt_count": true, "next_retry": true, "delivery_uncertain": true, "capabilities": true}
	for name := range envelope.Data {
		if !allowed[name] {
			t.Fatal("ownerless ordinary receipt outside strict whitelist", name)
		}
	}
	for _, spec := range []struct {
		name    string
		allowed map[string]bool
	}{{"progress", map[string]bool{"completeness": true, "counts": true}}, {"capabilities", map[string]bool{"view_content": true, "retry": true, "retry_block_reason": true}}} {
		if raw, ok := envelope.Data[spec.name]; ok {
			var nested map[string]json.RawMessage
			must(t, json.Unmarshal(raw, &nested))
			for name := range nested {
				if !spec.allowed[name] {
					t.Fatal("ownerless receipt nested field outside whitelist", spec.name, name)
				}
			}
			if counts, ok := nested["counts"]; ok {
				var aggregate map[string]json.RawMessage
				must(t, json.Unmarshal(counts, &aggregate))
				for name := range aggregate {
					switch name {
					case "total", "accepted", "pending", "temporary", "permanent", "uncertain":
					default:
						t.Fatal("ownerless receipt count field outside whitelist", name)
					}
				}
			}
		}
	}
	r5SubmissionPrivacyJSON(t, receipt, "Synthetic", "provenance@stage.test", mb.FullAddress, "Ownerless provenance witness")
	retryWorker := r5StageService(f, smtp.ln.Addr().(*net.TCPAddr).Port)
	retryCtx, retryCancel := context.WithCancel(ctx)
	retryWorker.StartWorker(retryCtx)
	retried := waitJob(t, f, retryJob.ID)
	retryCancel()
	retryWorker.Stop()
	if retried.State != models.OutboundSent || retried.SenderMailboxID == nil || *retried.SenderMailboxID != mb.ID {
		t.Fatalf("shipping retry lost same-ID legacy authority: %+v", retried)
	}
	smtp.mu.Lock()
	retryDataCount := len(smtp.messages["provenance@stage.test"])
	smtp.mu.Unlock()
	if retryDataCount != dataCount+1 {
		t.Fatalf("shipping retry DATA=%d want exactly %d separate durable jobs", retryDataCount, dataCount+1)
	}
}

func r5StageOwnerlessRetryState(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('job',(SELECT to_jsonb(j) FROM outbound_jobs j WHERE j.id=$1),'recipients',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.address) FROM outbound_recipients r WHERE r.job_id=$1),'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM outbound_attempts a WHERE a.job_id=$1),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a WHERE a.resource_id=$1),'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM outbox_events o WHERE o.payload->'metadata'->>'resource_id'=$1::text))::text`, id).Scan(&state))
	return state
}

// All negative transports retain the actual non-nil sender mailbox ID. State
// evidence includes the complete job, recipient ledger, attempts and audit/outbox.
func TestR5PermissionStagePostgresOwnerlessRetryRejections(t *testing.T) {
	r5StageRequirePG(t)
	for _, mutation := range []string{"different-key", "key-delete", "key-owner-added", "scope-shrink", "zone-shrink", "key-expire-at-db-now", "cross-tenant-key", "mailbox-owned", "mailbox-shared", "mailbox-address-reused", "mailbox-zone-change", "mailbox-expire-at-db-now", "zone-unverify", "zone-mx-unverify", "disabled-policy", "template-required-policy", "template-provenance", "in-flight-domain", "uncertain-recipient"} {
		t.Run(mutation, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			mb := r5StageLegacyMailbox(t, f, "negative-stage")
			key := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "Ownerless negative control", Scopes: []string{"send:read", "send:write"}}
			rawKey := "tm_negative_" + key.ID.String()
			key.KeyHash = company.Hash(rawKey)
			must(t, f.st.CreateAPIKey(ctx, key))
			actor := authz.Actor{Type: authz.PrincipalAPIKey, ID: key.ID, TenantID: f.tenant.ID, TenantWide: true}
			svc := r5StageService(f, 1)
			j, e := svc.Submit(ctx, outbound.SendRequest{Principal: &actor, TenantID: f.tenant.ID, APIKeyID: &key.ID, SenderMailboxID: &mb.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"negative@stage.test"}, Subject: "Synthetic negative control", TextBody: "OWNERLESS_PRIVATE_BODY", IdempotencyKey: uuid.NewString()})
			must(t, e)
			must(t, outbound.ValidateRetryRequester(ctx, f.st, actor, j))
			_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, j.ID)
			must(t, e)
			switch mutation {
			case "different-key":
				second := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "Different current Key", Scopes: key.Scopes}
				rawKey = "tm_different_" + second.ID.String()
				second.KeyHash = company.Hash(rawKey)
				must(t, f.st.CreateAPIKey(ctx, second))
			case "key-delete":
				must(t, f.st.DeleteAPIKey(ctx, key.ID))
			case "key-owner-added":
				_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$2 WHERE id=$1`, key.ID, f.employee.ID)
				must(t, e)
			case "scope-shrink":
				_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read"]'::jsonb WHERE id=$1`, key.ID)
				must(t, e)
			case "zone-shrink":
				other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "negative-other.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, other))
				_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, key.ID, []uuid.UUID{other.ID})
				must(t, e)
			case "key-expire-at-db-now":
				_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp() WHERE id=$1`, key.ID)
				must(t, e)
			case "cross-tenant-key":
				tenant := &models.Tenant{Name: "Foreign negative tenant", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, tenant))
				second := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenant.ID, KeyPrefix: "fixture", Label: "Foreign current Key", Scopes: key.Scopes}
				rawKey = "tm_foreign_" + second.ID.String()
				second.KeyHash = company.Hash(rawKey)
				must(t, f.st.CreateAPIKey(ctx, second))
			case "mailbox-owned":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2 WHERE id=$1`, mb.ID, f.employee.ID)
				must(t, e)
			case "mailbox-shared":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET mailbox_kind='shared' WHERE id=$1`, mb.ID)
				must(t, e)
			case "mailbox-address-reused":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET full_address='retired-negative@'||resolved_domain,local_part='retired-negative' WHERE id=$1`, mb.ID)
				must(t, e)
				replacement := r5StageLegacyMailbox(t, f, "negative-stage")
				if replacement.ID == mb.ID {
					t.Fatal("address reuse erased sender provenance")
				}
			case "mailbox-zone-change":
				other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "negative-zone.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, other))
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET zone_id=$2 WHERE id=$1`, mb.ID, other.ID)
				must(t, e)
			case "mailbox-expire-at-db-now":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, mb.ID)
				must(t, e)
			case "zone-unverify":
				_, e = f.pool.Exec(ctx, `UPDATE domain_zones SET is_verified=false WHERE id=$1`, f.zone.ID)
				must(t, e)
			case "zone-mx-unverify":
				_, e = f.pool.Exec(ctx, `UPDATE domain_zones SET mx_verified=false WHERE id=$1`, f.zone.ID)
				must(t, e)
			case "disabled-policy":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET send_policy='disabled' WHERE id=$1`, mb.ID)
				must(t, e)
			case "template-required-policy":
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET send_policy='template_required' WHERE id=$1`, mb.ID)
				must(t, e)
			case "template-provenance":
				_, version := r5VersionOrderFixture(t, f)
				_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET template_version_id=$2 WHERE id=$1`, j.ID, version.ID)
				must(t, e)
			case "in-flight-domain":
				_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='stage.test' WHERE id=$1`, j.ID)
				must(t, e)
			case "uncertain-recipient":
				_, e = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain' WHERE job_id=$1`, j.ID)
				must(t, e)
			}
			stable := r5StageOwnerlessRetryState(t, f, j.ID)
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			rr := r5StageHTTP(t, h, rawKey, true, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil)
			if mutation == "in-flight-domain" || mutation == "uncertain-recipient" {
				if rr.Code != 409 || !strings.Contains(rr.Body.String(), "delivery_uncertain") {
					t.Fatalf("uncertain retry=%d body=%s", rr.Code, rr.Body.String())
				}
			} else {
				r5StageHTTPVerdict(t, rr, false, 200)
			}
			if r5StageOwnerlessRetryState(t, f, j.ID) != stable {
				t.Fatal("denied retry changed job/recipients/attempts/audit/outbox")
			}
		})
	}
}

func TestR5PermissionStagePostgresOwnerlessRetryPoliciesBlockSMTP(t *testing.T) {
	r5StageRequirePG(t)
	for _, policy := range []string{"disabled", "template_required"} {
		t.Run(policy, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			mb := r5StageLegacyMailbox(t, f, "policy-stage")
			k := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "Ownerless policy control", Scopes: []string{"send:read", "send:write"}}
			k.KeyHash = company.Hash("tm_policy_" + k.ID.String())
			must(t, f.st.CreateAPIKey(ctx, k))
			smtp := newLocalSMTP(t)
			svc := r5StageService(f, smtp.ln.Addr().(*net.TCPAddr).Port)
			a := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, TenantWide: true}
			j, e := svc.Submit(ctx, outbound.SendRequest{Principal: &a, TenantID: f.tenant.ID, APIKeyID: &k.ID, SenderMailboxID: &mb.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"policy@stage.test"}, Subject: "Ownerless policy", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
			must(t, e)
			_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET send_policy=$2 WHERE id=$1`, mb.ID, policy)
			must(t, e)
			workerCtx, cancel := context.WithCancel(ctx)
			svc.StartWorker(workerCtx)
			terminal := waitJob(t, f, j.ID)
			cancel()
			svc.Stop()
			if terminal.State != models.OutboundFailed && terminal.State != models.OutboundDead || !strings.Contains(terminal.LastError, "authorization revoked") {
				t.Fatalf("policy job=%s error=%s", terminal.State, terminal.LastError)
			}
			smtp.mu.Lock()
			rcpts := smtp.rcpt["policy@stage.test"]
			bodies := len(smtp.messages["policy@stage.test"])
			smtp.mu.Unlock()
			if rcpts != 0 || bodies != 0 {
				t.Fatalf("blocked policy reached SMTP: RCPT=%d DATA=%d", rcpts, bodies)
			}
			before := r5StageOwnerlessRetryState(t, f, j.ID)
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			r5StageHTTPVerdict(t, r5StageHTTP(t, h, "tm_policy_"+k.ID.String(), true, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil), false, 200)
			if r5StageOwnerlessRetryState(t, f, j.ID) != before {
				t.Fatal("blocked policy retry changed durable effects")
			}
		})
	}
}

func TestR5PermissionStagePostgresOwnerlessRetryRetainsAcceptedRecipients(t *testing.T) {
	r5StageRequirePG(t)
	f := seedCompany(t)
	ctx := context.Background()
	mb := r5StageLegacyMailbox(t, f, "accepted-stage")
	k := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "Ownerless accepted-recipient control", Scopes: []string{"send:read", "send:write"}}
	k.KeyHash = company.Hash("tm_accepted_" + k.ID.String())
	must(t, f.st.CreateAPIKey(ctx, k))
	a := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, TenantWide: true}
	smtp := newLocalSMTP(t)
	port := smtp.ln.Addr().(*net.TCPAddr).Port
	svc := r5StageService(f, port)
	j, e := svc.Submit(ctx, outbound.SendRequest{Principal: &a, TenantID: f.tenant.ID, APIKeyID: &k.ID, SenderMailboxID: &mb.ID, ZoneID: f.zone.ID, From: mb.FullAddress, To: []string{"accepted@stage.test", "bad@stage.test"}, Subject: "Accepted recipient boundary", TextBody: "Synthetic", IdempotencyKey: uuid.NewString()})
	must(t, e)
	workerCtx, cancel := context.WithCancel(ctx)
	svc.StartWorker(workerCtx)
	failed := waitJob(t, f, j.ID)
	cancel()
	svc.Stop()
	if failed.State != models.OutboundFailed && failed.State != models.OutboundDead {
		t.Fatalf("partial terminal state=%s", failed.State)
	}
	var acceptedBefore, ledgerAfter string
	var attemptsBefore, attemptsAfter int
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM outbound_recipients r WHERE r.job_id=$1 AND address='accepted@stage.test' AND state='accepted'`, j.ID).Scan(&acceptedBefore))
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_attempts WHERE job_id=$1`, j.ID).Scan(&attemptsBefore))
	smtp.mu.Lock()
	initialAccepted := len(smtp.messages["accepted@stage.test"])
	initialGoodRCPT := smtp.rcpt["accepted@stage.test"]
	initialBadRCPT := smtp.rcpt["bad@stage.test"]
	smtp.mu.Unlock()
	if initialAccepted != 1 || initialGoodRCPT != 1 || initialBadRCPT != 1 {
		t.Fatal("real SMTP did not establish one accepted and one permanent recipient")
	}
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	r5StageHTTPVerdict(t, r5StageHTTP(t, h, "tm_accepted_"+k.ID.String(), true, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil), true, 200)
	retryWorker := r5StageService(f, port)
	retryCtx, retryCancel := context.WithCancel(ctx)
	retryWorker.StartWorker(retryCtx)
	terminal := waitJob(t, f, j.ID)
	retryCancel()
	retryWorker.Stop()
	if terminal.State != models.OutboundFailed && terminal.State != models.OutboundDead {
		t.Fatalf("permanent recipient unexpectedly replayed/erased: %s", terminal.State)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(r)::text FROM outbound_recipients r WHERE r.job_id=$1 AND address='accepted@stage.test' AND state='accepted'`, j.ID).Scan(&ledgerAfter))
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_attempts WHERE job_id=$1`, j.ID).Scan(&attemptsAfter))
	smtp.mu.Lock()
	accepted := len(smtp.messages["accepted@stage.test"])
	goodRCPT := smtp.rcpt["accepted@stage.test"]
	badRCPT := smtp.rcpt["bad@stage.test"]
	smtp.mu.Unlock()
	if accepted != 1 || goodRCPT != initialGoodRCPT || badRCPT != initialBadRCPT || acceptedBefore != ledgerAfter || attemptsBefore != attemptsAfter {
		t.Fatalf("retry resent terminal recipient or changed acceptance/attempt evidence: DATA=%d RCPT=%d/%d attempts=%d/%d", accepted, goodRCPT, badRCPT, attemptsBefore, attemptsAfter)
	}
}
