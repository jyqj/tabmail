package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/enterprise"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/settings"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func TestCompanyGovernanceRealPostgres(t *testing.T) {
	st, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	owner := &models.User{TenantID: tenant.ID, Email: "owner@example.test", DisplayName: "Owner", PasswordHash: "!fixture", Role: models.RoleAdmin, IsActive: true}
	must(t, st.CreateUser(ctx, owner))
	oldAdmin := &models.User{TenantID: tenant.ID, Email: "legacy-platform@example.test", PasswordHash: "!fixture", Role: models.RoleSuperAdmin, IsActive: true}
	must(t, st.CreateUser(ctx, oldAdmin))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "company.example.test", OwnerUserID: &owner.ID, IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	mb := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "historical", ResolvedDomain: zone.Domain, FullAddress: "historical@" + zone.Domain, AccessMode: models.AccessPublic}
	must(t, st.CreateMailbox(ctx, mb))
	obj := testutil.NewMemoryObjectStore()
	raw := []byte("From: customer@example.test\r\nSubject: Private\r\n\r\nconfidential content")
	must(t, obj.Put(ctx, "historical.eml", bytes.NewReader(raw), int64(len(raw))))
	old := &models.Message{TenantID: tenant.ID, ZoneID: zone.ID, MailboxID: mb.ID, Sender: "customer@example.test", Recipients: []string{mb.FullAddress}, Subject: "Private", RawObjectKey: "historical.eml", ExpiresAt: time.Now().Add(-time.Hour)}
	_, err := st.CreateMessageWithQuota(ctx, old, 1000)
	must(t, err)
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { rdb.Close() })
	hub := realtime.NewHub(0, nil)
	svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, zerolog.Nop())
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: obj, Hub: hub, NamingMode: policy.NamingFull, StripPlus: false, JWTSecret: "company-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, Settings: settings.NewManager(st, zerolog.Nop()), HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), OutboundService: svc, Logger: zerolog.Nop()})
	ownerToken, _ := authn.IssueAccessToken("company-test-secret", owner)
	call := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var b bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&b).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, &b)
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, r)
		return rr
	}
	expect := func(rr *httptest.ResponseRecorder, status int) {
		t.Helper()
		if rr.Code != status {
			t.Fatalf("expected %d, got %d: %s", status, rr.Code, rr.Body.String())
		}
	}
	t.Run("activation rejects unscoped legacy webhooks", func(t *testing.T) {
		h := handlers.NewCompanyHandler(st, true, true, zerolog.Nop())
		rr := httptest.NewRecorder()
		h.Enable(rr, httptest.NewRequest("POST", "/api/v1/company", nil))
		expect(rr, 400)
		c, err := st.GetCompany(ctx, tenant.ID)
		must(t, err)
		if c != nil {
			t.Fatal("unsafe integration configuration activated company")
		}
	})
	t.Run("activation locks down existing assets and keeps mail", func(t *testing.T) {
		expect(call("POST", "/api/v1/company", ownerToken, map[string]any{"primary_zone_id": zone.ID, "name": "Company", "confirm_lockdown": true}), 200)
		expect(call("GET", "/api/v1/mailbox/"+mb.FullAddress, "", nil), 403)
		u, err := st.GetUser(ctx, oldAdmin.ID)
		must(t, err)
		if u.Role != models.RoleUser {
			t.Fatal("imported viewer retained legacy platform privileges")
		}
		n, _, err := st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
		must(t, err)
		if n != 0 {
			t.Fatal("company mail expired")
		}
		expect(call("POST", "/api/v1/auth/register", "", map[string]string{"email": "stranger@example.test", "password": "not-a-production-secret"}), 403)
		expect(call("POST", "/api/v1/domains", ownerToken, map[string]string{"domain": "unexpected.test"}), 403)
	})
	provision := func(local, role string) (*models.User, string) {
		rr := call("POST", "/api/v1/company/employees", ownerToken, enterprise.Provision{LocalPart: local, DisplayName: local, Role: role, DailyQuota: 500})
		expect(rr, 200)
		var result struct {
			Data enterprise.Invitation `json:"data"`
		}
		must(t, json.Unmarshal(rr.Body.Bytes(), &result))
		expect(call("POST", "/api/v1/company/activate-account", "", map[string]string{"token": result.Data.Token, "password": "fixture-password-only"}), 200)
		expect(call("POST", "/api/v1/company/activate-account", "", map[string]string{"token": result.Data.Token, "password": "fixture-password-only"}), 400)
		u, err := st.GetUser(ctx, result.Data.Member.UserID)
		must(t, err)
		token, err := authn.IssueAccessToken("company-test-secret", u)
		must(t, err)
		return u, token
	}
	alice, aliceToken := provision("alice", "employee")
	bob, bobToken := provision("bob", "restricted")
	amb, err := st.GetMailboxByAddress(ctx, alice.Email)
	must(t, err)
	bmb, err := st.GetMailboxByAddress(ctx, bob.Email)
	must(t, err)
	msg := &models.Message{TenantID: tenant.ID, ZoneID: zone.ID, MailboxID: amb.ID, Sender: "customer@example.test", Recipients: []string{alice.Email}, Subject: "Private", RawObjectKey: "historical.eml", ExpiresAt: time.Now().Add(-time.Minute)}
	_, err = st.CreateMessageWithQuota(ctx, msg, 1000)
	must(t, err)
	t.Run("same-domain isolation and legacy bypass denial", func(t *testing.T) {
		expect(call("GET", "/api/v1/mailbox/"+alice.Email, aliceToken, nil), 200)
		expect(call("GET", "/api/v1/mailbox/"+alice.Email, bobToken, nil), 403)
		expect(call("GET", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String()+"/source", bobToken, nil), 403)
		expect(call("POST", "/api/v1/send-identities", aliceToken, map[string]any{"zone_id": zone.ID, "address": "owner@" + zone.Domain}), 403)
		expect(call("POST", "/api/v1/keys", aliceToken, map[string]any{"label": "bypass", "scopes": []string{"send:write"}}), 403)
		expect(call("PATCH", "/api/v1/admin/users/"+alice.ID.String(), aliceToken, map[string]string{"role": "admin"}), 403)
		rr := call("GET", "/api/v1/mailboxes", aliceToken, nil)
		expect(rr, 200)
		if strings.Contains(rr.Body.String(), bob.Email) {
			t.Fatal("mailbox list leaks another employee")
		}
		expect(call("PUT", "/api/v1/company/grants", aliceToken, enterprise.Grant{MailboxID: bmb.ID, UserID: alice.ID, Read: true}), 403)
	})
	send := func(from string) map[string]any {
		return map[string]any{"from": from, "to": []string{"recipient@example.test"}, "subject": "Hello", "text_body": "Body"}
	}
	var job models.OutboundJob
	t.Run("exact sender and stored provenance", func(t *testing.T) {
		expect(call("POST", "/api/v1/send", aliceToken, send(bob.Email)), 403)
		expect(call("POST", "/api/v1/send", bobToken, send(bob.Email)), 403)
		rr := call("POST", "/api/v1/send", aliceToken, send(alice.Email))
		expect(rr, 201)
		var res struct{ Data models.OutboundJob }
		must(t, json.Unmarshal(rr.Body.Bytes(), &res))
		job = res.Data
		loaded, err := st.GetOutboundJob(ctx, job.ID)
		must(t, err)
		must(t, st.ValidateCompanyJob(ctx, loaded))
		expect(call("GET", "/api/v1/outbound/"+job.ID.String(), ownerToken, nil), 404)
		expect(call("GET", "/api/v1/outbound/"+job.ID.String(), bobToken, nil), 404)
	})
	t.Run("administrator normal access and mandatory break glass audit", func(t *testing.T) {
		rr := call("GET", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String(), ownerToken, nil)
		expect(rr, 200)
		if !strings.Contains(rr.Body.String(), `"body_redacted":true`) {
			t.Fatal("admin read content without a grant")
		}
		expect(call("POST", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String()+"/break-glass", ownerToken, map[string]string{"reason": "incident test"}), 200)
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_sensitive_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='message.break_glass_read' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER audit_fail BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_sensitive_audit()`)
		must(t, err)
		expect(call("POST", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String()+"/break-glass", ownerToken, map[string]string{"reason": "must not leak"}), 500)
		_, err = pool.Exec(ctx, `DROP TRIGGER audit_fail ON audit_log; DROP FUNCTION fail_sensitive_audit()`)
		must(t, err)
		must(t, st.SetCompanyGrant(ctx, tenant.ID, owner.ID, enterprise.Grant{MailboxID: amb.ID, UserID: owner.ID, Read: true}))
		rr = call("GET", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String(), ownerToken, nil)
		expect(rr, 200)
		if !strings.Contains(rr.Body.String(), "confidential content") {
			t.Fatal("normal admin grant did not allow body")
		}
	})
	tpl := enterprise.Template{Name: "Welcome", Subject: "Hello {{.name}}", TextBody: "{{.company_name}} welcomes {{.name}}", Variables: map[string]int{"name": 80}, MailboxIDs: []uuid.UUID{bmb.ID}}
	rr := call("POST", "/api/v1/company/templates", ownerToken, tpl)
	expect(rr, 200)
	var tr struct{ Data enterprise.Template }
	must(t, json.Unmarshal(rr.Body.Bytes(), &tr))
	tpl = tr.Data
	templateSend := map[string]any{"from": bob.Email, "to": []string{"recipient@example.test"}, "template_id": tpl.ID, "variables": map[string]string{"name": "Customer"}}
	var templateJob models.OutboundJob
	t.Run("immutable published templates and server rendering", func(t *testing.T) {
		expect(call("POST", "/api/v1/send", bobToken, templateSend), 403)
		expect(call("PATCH", "/api/v1/company/templates/"+tpl.ID.String(), ownerToken, map[string]string{"status": "published"}), 200)
		rr := call("POST", "/api/v1/send", bobToken, templateSend)
		expect(rr, 201)
		var res struct{ Data models.OutboundJob }
		must(t, json.Unmarshal(rr.Body.Bytes(), &res))
		templateJob = res.Data
		if templateJob.Subject != "Hello Customer" || !strings.Contains(templateJob.TextBody, "Company welcomes Customer") {
			t.Fatal("server did not render template")
		}
		bad := map[string]any{}
		for k, v := range templateSend {
			bad[k] = v
		}
		bad["text_body"] = "bypass"
		expect(call("POST", "/api/v1/send", bobToken, bad), 400)
		bad["text_body"] = ""
		bad["from"] = alice.Email
		expect(call("POST", "/api/v1/send", aliceToken, bad), 403)
		bad["from"] = bob.Email
		bad["variables"] = map[string]string{"name": "customer", "employee_name": "spoof"}
		expect(call("POST", "/api/v1/send", bobToken, bad), 400)
		expect(call("POST", "/api/v1/company/templates", bobToken, tpl), 403)
	})
	t.Run("retirement and revocation affect queued jobs", func(t *testing.T) {
		loaded, err := st.GetOutboundJob(ctx, templateJob.ID)
		must(t, err)
		must(t, st.ValidateCompanyJob(ctx, loaded))
		expect(call("PATCH", "/api/v1/company/templates/"+tpl.ID.String(), ownerToken, map[string]string{"status": "retired"}), 200)
		if st.ValidateCompanyJob(ctx, loaded) == nil {
			t.Fatal("retired template still allowed")
		}
		_, err = pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, templateJob.ID)
		must(t, err)
		expect(call("POST", "/api/v1/outbound/"+templateJob.ID.String()+"/retry", bobToken, nil), 403)
		_, err = pool.Exec(ctx, `UPDATE outbound_jobs SET text_body='tampered' WHERE id=$1`, job.ID)
		must(t, err)
		changed, err := st.GetOutboundJob(ctx, job.ID)
		must(t, err)
		if st.ValidateCompanyJob(ctx, changed) == nil {
			t.Fatal("changed content passed provenance")
		}
	})
	t.Run("atomic grant mutation and audit rollback", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE FUNCTION fail_grant_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='mailbox.grant.updated' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER grant_audit_fail BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_grant_audit()`)
		must(t, err)
		err = st.SetCompanyGrant(ctx, tenant.ID, owner.ID, enterprise.Grant{MailboxID: bmb.ID, UserID: alice.ID, Read: true})
		if err == nil {
			t.Fatal("audit failure ignored")
		}
		g, err := st.GetCompanyGrant(ctx, tenant.ID, alice.ID, bmb.ID)
		must(t, err)
		if g != nil {
			t.Fatal("grant survived audit failure")
		}
		_, err = pool.Exec(ctx, `DROP TRIGGER grant_audit_fail ON audit_log; DROP FUNCTION fail_grant_audit()`)
		must(t, err)
	})
	t.Run("concurrent quota reservations", func(t *testing.T) {
		quotaUser, quotaToken := provision("quota", "employee")
		must(t, st.UpdateCompanyMember(ctx, tenant.ID, owner.ID, quotaUser.ID, "employee", true, 2))
		var wg sync.WaitGroup
		codes := make(chan int, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); codes <- call("POST", "/api/v1/send", quotaToken, send(quotaUser.Email)).Code }()
		}
		wg.Wait()
		close(codes)
		accepted, denied := 0, 0
		for code := range codes {
			switch code {
			case 201:
				accepted++
			case 429:
				denied++
			default:
				t.Errorf("unexpected quota response: %d", code)
			}
		}
		if accepted != 2 || denied != 6 {
			t.Fatalf("quota race: accepted=%d denied=%d", accepted, denied)
		}
	})
	t.Run("realtime revoked before next event", func(t *testing.T) {
		server := httptest.NewServer(router)
		defer server.Close()
		req, _ := http.NewRequest("GET", server.URL+"/api/v1/mailbox/"+alice.Email+"/events", nil)
		req.Header.Set("Authorization", "Bearer "+aliceToken)
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Do(req)
		must(t, err)
		defer response.Body.Close()
		must(t, st.SetCompanyGrant(ctx, tenant.ID, owner.ID, enterprise.Grant{MailboxID: amb.ID, UserID: alice.ID}))
		hub.Publish(realtime.Event{Type: realtime.EventMessage, Mailbox: alice.Email, Subject: "must not arrive"})
		payload, err := io.ReadAll(response.Body)
		must(t, err)
		if strings.Contains(string(payload), "must not arrive") {
			t.Fatal("revoked stream leaked event")
		}
	})
	t.Run("suspension revokes tokens and queues; owner protected", func(t *testing.T) {
		must(t, st.UpdateCompanyMember(ctx, tenant.ID, owner.ID, bob.ID, "restricted", false, 500))
		expect(call("GET", "/api/v1/company", bobToken, nil), 401)
		if err := st.UpdateCompanyMember(ctx, tenant.ID, owner.ID, owner.ID, "viewer", false, 500); err == nil {
			t.Fatal("owner disabled")
		}
	})

	t.Run("cross-tenant administrators and grants remain isolated", func(t *testing.T) {
		other := &models.Tenant{Name: "Other", PlanID: tenant.PlanID}
		must(t, st.CreateTenant(ctx, other))
		u := &models.User{TenantID: other.ID, Email: "other-owner@example.test", Role: models.RoleAdmin, IsActive: true, PasswordHash: "!fixture"}
		must(t, st.CreateUser(ctx, u))
		z := &models.DomainZone{TenantID: other.ID, Domain: "other.example.test", IsVerified: true, MXVerified: true}
		must(t, st.CreateZone(ctx, z))
		foreignBox := &models.Mailbox{TenantID: other.ID, ZoneID: z.ID, LocalPart: "foreign", ResolvedDomain: z.Domain, FullAddress: "foreign@" + z.Domain, AccessMode: models.AccessAPIKey}
		must(t, st.CreateMailbox(ctx, foreignBox))
		otherToken, err := authn.IssueAccessToken("company-test-secret", u)
		must(t, err)
		expect(call("GET", "/api/v1/mailbox/"+alice.Email, otherToken, nil), 403)
		expect(call("POST", "/api/v1/mailbox/"+alice.Email+"/"+msg.ID.String()+"/break-glass", otherToken, map[string]string{"reason": "not in this company"}), 403)
		expect(call("GET", "/api/v1/mailbox/"+foreignBox.FullAddress, ownerToken, nil), 403)
		expect(call("PUT", "/api/v1/company/grants", ownerToken, enterprise.Grant{UserID: u.ID, MailboxID: amb.ID, Read: true}), 404)
		expect(call("PUT", "/api/v1/company/grants", ownerToken, enterprise.Grant{UserID: alice.ID, MailboxID: foreignBox.ID, Read: true}), 404)
	})
	t.Run("administrator escalation and implicit suspension are rejected", func(t *testing.T) {
		admin, adminToken := provision("department-admin", "admin")
		expect(call("POST", "/api/v1/company/employees", adminToken, enterprise.Provision{LocalPart: "escalation", DisplayName: "Denied", Role: "admin", DailyQuota: 10}), 403)
		expect(call("PUT", "/api/v1/company/grants", adminToken, enterprise.Grant{UserID: admin.ID, MailboxID: amb.ID, Read: true}), 403)
		expect(call("PATCH", "/api/v1/company/employees/"+alice.ID.String(), ownerToken, map[string]any{"company_role": "employee", "daily_send_quota": 500}), 400)
		m, err := st.GetCompanyMember(ctx, tenant.ID, alice.ID)
		must(t, err)
		if !m.Active {
			t.Fatal("missing is_active silently suspended the employee")
		}
		expect(call("PATCH", "/api/v1/company/employees/"+owner.ID.String(), adminToken, map[string]any{"company_role": "viewer", "is_active": false, "daily_send_quota": 500}), 403)
	})
	t.Run("reissued activation code invalidates old code and concurrent consumption is single use", func(t *testing.T) {
		rr := call("POST", "/api/v1/company/employees", ownerToken, enterprise.Provision{LocalPart: "pending", DisplayName: "Pending", Role: "employee", DailyQuota: 10})
		expect(rr, 200)
		var invitation struct {
			Data enterprise.Invitation `json:"data"`
		}
		must(t, json.Unmarshal(rr.Body.Bytes(), &invitation))
		rr = call("POST", "/api/v1/company/employees/"+invitation.Data.Member.UserID.String()+"/invite", ownerToken, nil)
		expect(rr, 200)
		var next struct {
			Data struct {
				Token string `json:"activation_token"`
			} `json:"data"`
		}
		must(t, json.Unmarshal(rr.Body.Bytes(), &next))
		expect(call("POST", "/api/v1/company/activate-account", "", map[string]string{"token": invitation.Data.Token, "password": "fixture-password-only"}), 400)
		var wg sync.WaitGroup
		results := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- call("POST", "/api/v1/company/activate-account", "", map[string]string{"token": next.Data.Token, "password": "fixture-password-only"}).Code
			}()
		}
		wg.Wait()
		close(results)
		var success, denied int
		for code := range results {
			if code == 200 {
				success++
			} else if code == 400 {
				denied++
			} else {
				t.Errorf("activation status %d", code)
			}
		}
		if success != 1 || denied != 1 {
			t.Fatalf("activation race: %d succeeded, %d rejected", success, denied)
		}
	})

	t.Run("restart never replays destructive schema migration", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE TABLE mailbox_grants(marker TEXT); INSERT INTO mailbox_grants VALUES('preserve')`)
		must(t, err)
		restarted, err := postgres.New(ctx, config.DB{DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1})
		must(t, err)
		restarted.Close()
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM mailbox_grants`).Scan(&n))
		if n != 1 {
			t.Fatal("historical grants dropped")
		}
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM tabmail_schema_migrations`).Scan(&n))
		if n != 2 {
			t.Fatal("migration records missing")
		}
		_, err = pool.Exec(ctx, `UPDATE tabmail_schema_migrations SET checksum='tampered' WHERE version=2`)
		must(t, err)
		if bad, err := postgres.New(ctx, config.DB{DSN: dsn, MaxOpenConns: 2}); err == nil {
			bad.Close()
			t.Fatal("migration checksum tampering accepted")
		}
	})
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
