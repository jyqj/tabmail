package api_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

// This is the shipping TCP/router/JWT/service/PgStore path, not a mock port.
func TestR5PermissionProfileCreateHTTPRevisionAuditAndDuplicate(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := context.Background()
	c := f.Companies[0]
	name := "HTTP guarded create " + uuid.NewString()
	raw, err := json.Marshal(map[string]any{"name": name, "tenant_id": f.Companies[1].Tenant.ID, "can_send": false, "daily_send_quota": 0, "allowed_zone_ids": []uuid.UUID{c.Zone.ID}})
	if err != nil {
		t.Fatal(err)
	}
	out := r5EditorHTTPData[models.PermissionProfile](t, r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/admin/permissions", string(raw), 201))
	n, err := strconv.ParseInt(out.Revision, 10, 64)
	if err != nil || n <= 0 || out.IsSystem || out.TenantID == nil || *out.TenantID != c.Tenant.ID || out.CanSend || out.DailySendQuota != 0 {
		t.Fatalf("invalid persisted HTTP create: %+v", out)
	}
	var revision string
	var audit, event int
	if err = f.Pool.QueryRow(ctx, `SELECT permission_revision::text FROM permission_profiles WHERE id=$1 AND tenant_id=$2`, out.ID, c.Tenant.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != out.Revision {
		t.Fatal("201 returned a synthetic/nonpersisted revision")
	}
	if err = f.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log WHERE action='permission.profile.create' AND resource_id=$1),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='permission.profile.create' AND payload->'metadata'->>'resource_id'=$1::text)`, out.ID).Scan(&audit, &event); err != nil {
		t.Fatal(err)
	}
	if audit != 1 || event != 1 {
		t.Fatalf("required create audit/event=%d/%d", audit, event)
	}
	before := r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/admin/permissions", string(raw), 409)
	if before != r5EditorHTTPState(t, f) {
		t.Fatal("duplicate POST committed extra data/audit/events")
	}
}
func TestR5PermissionProfileCreateHTTPRefusalsAndRequiredEffects(t *testing.T) {
	for _, mode := range []string{"employee", "foreign-zone", "system-field", "audit", "outbox"} {
		t.Run(mode, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			ctx := context.Background()
			token := f.JWT(0, "admin")
			want := 400
			body := map[string]any{"name": "HTTP rejected create " + uuid.NewString()}
			switch mode {
			case "employee":
				token = f.JWT(0, "sender")
				want = 403
			case "foreign-zone":
				body["allowed_zone_ids"] = []uuid.UUID{f.Companies[1].Zone.ID}
			case "system-field":
				body["is_system"] = true
			case "audit":
				want = 500
				_, err := f.Pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT create_http_audit_failure CHECK(action<>'permission.profile.create') NOT VALID`)
				if err != nil {
					t.Fatal(err)
				}
			case "outbox":
				want = 500
				_, err := f.Pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT create_http_outbox_failure CHECK(event_type<>'company.admin.changed' OR payload->'metadata'->>'action'<>'permission.profile.create') NOT VALID`)
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			before := r5EditorHTTPState(t, f)
			response := r5EditorHTTPCall(t, f, token, "POST", "/api/v1/admin/permissions", string(raw), want)
			if before != r5EditorHTTPState(t, f) {
				t.Fatal("refused HTTP creation left transactional effects")
			}
			var envelope map[string]json.RawMessage
			if err = json.Unmarshal(response, &envelope); err != nil {
				t.Fatal(err)
			}
			if data, ok := envelope["data"]; ok && string(data) != "null" {
				t.Fatal("failed HTTP create released profile data")
			}
		})
	}
}
func TestR5PermissionProfileCreateHTTPGlobalHasAuditNotInventedAudience(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := context.Background()
	c := f.Companies[0]
	if _, err := f.Pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, c.Admin.ID); err != nil {
		t.Fatal(err)
	}
	f.RefreshJWT(t, 0, "admin")
	raw, err := json.Marshal(map[string]any{"name": "HTTP global create " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	out := r5EditorHTTPData[models.PermissionProfile](t, r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/admin/permissions", string(raw), 201))
	if out.TenantID != nil || out.IsSystem || out.Revision == "" {
		t.Fatal("global ordinary profile contract lost")
	}
	var audit, event int
	if err = f.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log WHERE action='permission.profile.create' AND resource_id=$1),(SELECT count(*) FROM outbox_events WHERE payload->'metadata'->>'action'='permission.profile.create' AND payload->'metadata'->>'resource_id'=$1::text)`, out.ID).Scan(&audit, &event); err != nil {
		t.Fatal(err)
	}
	if audit != 1 || event != 0 {
		t.Fatalf("global create invented audience or omitted audit: %d/%d", audit, event)
	}
}

func TestR5PermissionProfileCreateHTTPKeyPrincipalsCannotManage(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := context.Background()
	c := f.Companies[0]
	// Issue credentials through the actual API; then exercise both an owner-
	// bound admin key and the persisted ownerless-key credential envelope.
	issued := r5EditorHTTPData[struct {
		Key string    `json:"key"`
		ID  uuid.UUID `json:"id"`
	}](t, r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/keys", `{"label":"profile-create-boundary","scopes":["domains:read","domains:write"]}`, 201))
	if issued.Key == "" {
		t.Fatal("key issuance omitted actual credential")
	}
	var id uuid.UUID
	if err := f.Pool.QueryRow(ctx, `SELECT id FROM tenant_api_keys WHERE tenant_id=$1 AND label='profile-create-boundary'`, c.Tenant.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == uuid.Nil {
		t.Fatal("owned credential lacks persisted identity")
	}
	before := r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, issued.Key, "POST", "/api/v1/admin/permissions", `{"name":"HTTP owned key rejected create"}`, 403)
	if before != r5EditorHTTPState(t, f) {
		t.Fatal("owned admin key mutated profile creation state")
	}
	ownerless := r5EditorHTTPData[struct {
		Key string `json:"key"`
	}](t, r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/keys", `{"label":"profile-create-ownerless","scopes":["domains:read","domains:write"]}`, 201))
	// Remove ownership before this new credential is ever authenticated, so an
	// owned-key middleware cache entry cannot masquerade as the ownerless case.
	if _, err := f.Pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=NULL WHERE tenant_id=$1 AND label='profile-create-ownerless'`, c.Tenant.ID); err != nil {
		t.Fatal(err)
	}
	var owner *uuid.UUID
	if err := f.Pool.QueryRow(ctx, `SELECT owner_user_id FROM tenant_api_keys WHERE tenant_id=$1 AND label='profile-create-ownerless'`, c.Tenant.ID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != nil || ownerless.Key == "" {
		t.Fatal("ownerless credential preparation failed")
	}
	before = r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, ownerless.Key, "POST", "/api/v1/admin/permissions", `{"name":"HTTP ownerless key rejected create"}`, 403)
	if before != r5EditorHTTPState(t, f) {
		t.Fatal("ownerless key mutated profile creation state")
	}
}
func TestR5PermissionProfileCreateHTTPSuppressedInsertDoesNotReturn201Null(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := context.Background()
	if _, err := f.Pool.Exec(ctx, `CREATE FUNCTION create_http_suppress_row() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NULL; END$$; CREATE TRIGGER create_http_suppress_row BEFORE INSERT ON permission_profiles FOR EACH ROW EXECUTE FUNCTION create_http_suppress_row()`); err != nil {
		t.Fatal(err)
	}
	before := r5EditorHTTPState(t, f)
	response := r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/admin/permissions", `{"name":"HTTP suppressed persisted profile"}`, 500)
	if before != r5EditorHTTPState(t, f) {
		t.Fatal("suppressed HTTP INSERT left data/audit/outbox")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if data, ok := envelope["data"]; ok && string(data) != "null" {
		t.Fatal("suppressed HTTP INSERT released profile payload")
	}
}
