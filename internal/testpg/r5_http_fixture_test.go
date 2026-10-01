//go:build r5fixtures

package testpg_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"strconv"
	"strings"
	"tabmail/internal/company"
	"tabmail/internal/testpg"
	"testing"
	"time"
)

func r5HTTPContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func r5RequireStatus(t *testing.T, status, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("actual formal HTTP status=%d target=%d (private response omitted)", status, want)
	}
}
func r5Data[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var envelope struct{ Data T }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal("actual HTTP DTO decode failed")
	}
	return envelope.Data
}
func r5SetSenderProfile(t *testing.T, f *testpg.R5HTTPFixture, quota int) {
	t.Helper()
	ctx := r5HTTPContext(t)
	c := f.Companies[0]
	status, raw := f.Request(t, ctx, f.JWT(0, "admin"), "POST", "/api/v1/admin/permissions", map[string]any{"name": "Fixture sender profile", "can_send": true, "can_create_api_keys": true, "daily_send_quota": quota, "max_mailboxes": 10, "max_domains": 2, "allowed_zone_ids": []uuid.UUID{c.Zone.ID}}, "")
	r5RequireStatus(t, status, 201)
	p := r5Data[struct{ ID uuid.UUID }](t, raw)
	status, _ = f.Request(t, ctx, f.JWT(0, "admin"), "PATCH", "/api/v1/admin/users/"+c.Users["sender"].ID.String(), map[string]any{"permission_profile_id": p.ID}, "")
	r5RequireStatus(t, status, 200)
	f.RefreshJWT(t, 0, "sender")
}
func r5HTTPDraft(t *testing.T, f *testpg.R5HTTPFixture) company.Draft {
	t.Helper()
	c := f.Companies[0]
	d := company.Draft{MailboxID: c.Personal["sender"].ID, Payload: company.DraftPayload{To: []string{"recipient@fixture.test"}, Subject: "Synthetic shared fixture", TextBody: "Synthetic fixture body"}}
	status, raw := f.Request(t, r5HTTPContext(t), f.JWT(0, "sender"), "POST", "/api/v1/company/drafts", d, "")
	r5RequireStatus(t, status, 200)
	return r5Data[company.Draft](t, raw)
}
func TestR5FixtureHTTPJWTAndTenantBoundary(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	status, _ := f.Request(t, ctx, "", "GET", "/api/v1/company/mailboxes", nil, "")
	r5RequireStatus(t, status, 401)
	status, _ = f.Request(t, ctx, f.JWT(0, "reader"), "GET", "/api/v1/company/mailboxes", nil, "")
	r5RequireStatus(t, status, 200)
	status, _ = f.Request(t, ctx, f.JWT(0, "frozen"), "GET", "/api/v1/company/mailboxes", nil, "")
	r5RequireStatus(t, status, 401)
	path := "/api/v1/company/mailboxes/" + f.Companies[0].Shared.ID.String() + "/grants"
	status, _ = f.Request(t, ctx, f.JWT(1, "admin"), "GET", path, nil, "")
	r5RequireStatus(t, status, 404)
	if f.SchemaVersion < 14 || len(f.APIKeyScopeConstraintSHA256) != 64 {
		t.Fatal("actual schema scope catalog premise missing")
	}
}
func TestR5FixtureHTTPQuotaAndRequiredAuditRollback(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	r5SetSenderProfile(t, f, 1)
	draft := r5HTTPDraft(t, f)
	snapshot := func() [5]int64 {
		var state [5]int64
		err := f.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM outbound_recipients WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1),(SELECT count(*) FROM outbox_events WHERE payload->>'tenant_id'=$1::text)`, f.Companies[0].Tenant.ID).Scan(&state[0], &state[1], &state[2], &state[3], &state[4])
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	beforeFailure := snapshot()
	_, err := f.Pool.Exec(ctx, `CREATE FUNCTION r5_fixture_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='outbound.submit' THEN RAISE EXCEPTION 'owned fixture audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER r5_fixture_audit_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION r5_fixture_audit_fault()`)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/company/drafts/" + draft.ID.String() + "/submit"
	status, failureBody := f.Request(t, ctx, f.JWT(0, "sender"), "POST", path, map[string]int{"expected_revision": draft.Revision}, "owned-fixture-submit")
	faultStatus := status
	var faultEnvelope struct {
		Error struct{ Code, Message string }
	}
	if err = json.Unmarshal(failureBody, &faultEnvelope); err != nil {
		t.Fatal("audit fault returned non-envelope response")
	}
	faultReached := strings.Contains(faultEnvelope.Error.Message, "owned fixture audit fault")
	faultSQLState := strings.Contains(faultEnvelope.Error.Message, "P0001")
	faultHash := sha256.Sum256([]byte(faultEnvelope.Error.Message))
	t.Logf("OWNED_AUDIT_DIAGNOSTIC status=%d code=%s owned_fault=%t sqlstate_p0001=%t message_sha256=%x", faultStatus, faultEnvelope.Error.Code, faultReached, faultSQLState, faultHash)

	var jobs, drafts int
	if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1`, f.Companies[0].Tenant.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if snapshot() != beforeFailure {
		t.Fatal("required audit fault changed job/recipient/archive/audit/outbox transaction state")
	}
	if jobs != 0 || drafts != 1 {
		t.Fatal("actual required audit failure consumed draft or committed job")
	}
	_, err = f.Pool.Exec(ctx, `DROP TRIGGER r5_fixture_audit_fault ON audit_log; DROP FUNCTION r5_fixture_audit_fault()`)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = f.Request(t, ctx, f.JWT(0, "sender"), "POST", path, map[string]int{"expected_revision": draft.Revision}, "owned-fixture-submit")
	r5RequireStatus(t, status, 201)
	status, _ = f.Request(t, ctx, f.JWT(0, "sender"), "POST", path, map[string]int{"expected_revision": draft.Revision}, "owned-fixture-submit")
	r5RequireStatus(t, status, 200)
	another := r5HTTPDraft(t, f)
	status, _ = f.Request(t, ctx, f.JWT(0, "sender"), "POST", "/api/v1/company/drafts/"+another.ID.String()+"/submit", map[string]int{"expected_revision": another.Revision}, "owned-fixture-quota")
	r5RequireStatus(t, status, 429)
	if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1`, f.Companies[0].Tenant.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatal("quota/replay did not preserve once effect")
	}
	if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, another.ID).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 {
		t.Fatal("quota rejection consumed the remaining draft")
	}
	if faultStatus != 500 || faultEnvelope.Error.Code != "INTERNAL" {
		t.Errorf("required owned audit failure must be safe 500/INTERNAL; actual=%d/%s owned_fault=%t sqlstate_p0001=%t", faultStatus, faultEnvelope.Error.Code, faultReached, faultSQLState)
	}
}

// Owner provides the actual applied schema version for this independently
// frozen Key run. Never bypass the nonempty/allowed-scope CHECK to construct it.
func TestR5FixtureHTTPKeyScopeRevocationAndExpiry(t *testing.T) {
	expected, err := strconv.ParseInt(os.Getenv("TABMAIL_R5_KEY_FIXTURE_SCHEMA_VERSION"), 10, 64)
	if err != nil || expected < 14 {
		t.Fatal("Key fixture requires owner's actual schema-version handshake")
	}
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	if f.SchemaVersion != expected {
		t.Fatalf("Key fixture actual schema version %d differs from approved %d", f.SchemaVersion, expected)
	}
	r5SetSenderProfile(t, f, 10)
	type issued struct {
		ID  uuid.UUID
		Key string
	}
	issue := func(scopes []string) issued {
		status, raw := f.Request(t, ctx, f.JWT(0, "sender"), "POST", "/api/v1/keys", map[string]any{"label": "Owned test credential", "scopes": scopes}, "")
		r5RequireStatus(t, status, 201)
		credential := r5Data[issued](t, raw)
		if credential.ID == uuid.Nil || credential.Key == "" {
			t.Fatal("actual formal Key creation missing private credential")
		}
		return credential
	}
	normal := issue([]string{"send:read"})
	status, _ := f.Request(t, ctx, normal.Key, "GET", "/api/v1/outbound", nil, "")
	r5RequireStatus(t, status, 200)
	missing := issue([]string{"messages:read"}) // legal nonempty CHECK input, not an invalid empty scope fixture
	status, _ = f.Request(t, ctx, missing.Key, "GET", "/api/v1/outbound", nil, "")
	r5RequireStatus(t, status, 403)
	status, _ = f.Request(t, ctx, f.JWT(0, "sender"), "DELETE", "/api/v1/keys/"+normal.ID.String(), nil, "")
	r5RequireStatus(t, status, 204)
	status, _ = f.Request(t, ctx, normal.Key, "GET", "/api/v1/outbound", nil, "")
	r5RequireStatus(t, status, 401)
	expired := issue([]string{"send:read"})
	if _, err = f.Pool.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	status, _ = f.Request(t, ctx, expired.Key, "GET", "/api/v1/outbound", nil, "")
	r5RequireStatus(t, status, 401)
	frozen := issue([]string{"send:read"})
	status, _ = f.Request(t, ctx, f.JWT(0, "admin"), "PATCH", "/api/v1/admin/users/"+f.Companies[0].Users["sender"].ID.String(), map[string]any{"is_active": false}, "")
	r5RequireStatus(t, status, 200)
	status, _ = f.Request(t, ctx, frozen.Key, "GET", "/api/v1/outbound", nil, "")
	r5RequireStatus(t, status, 401)
}
