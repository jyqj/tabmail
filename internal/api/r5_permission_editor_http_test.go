package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

// Real TCP loopback -> production router -> JWT/API-key middleware -> strict
// decoder -> permission service -> PgStore. No mock responses or SDK shortcut.
func r5EditorHTTPCall(t *testing.T, f *testpg.R5HTTPFixture, token, method, path, raw string, want int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, f.Server.URL+path, strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(token, "tb_") {
		req.Header.Set("X-API-Key", token)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := f.Server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d", method, path, res.StatusCode, want)
	}
	return body
}
func r5EditorHTTPData[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
func r5EditorHTTPPath(id uuid.UUID) string {
	return "/api/v1/admin/users/" + id.String() + "/permission-editor"
}
func r5EditorHTTPRead(t *testing.T, f *testpg.R5HTTPFixture) company.PermissionEditorSnapshot {
	t.Helper()
	c := f.Companies[0]
	s := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "GET", r5EditorHTTPPath(c.Users["sender"].ID), "", 200))
	if s.UserID != c.Users["sender"].ID || s.TenantID != c.Tenant.ID || s.Effective == nil {
		t.Fatal("real GET identity/effective missing")
	}
	if err := s.Revision.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}
func r5EditorHTTPCommand(t *testing.T, r company.PermissionRevision, patch string) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return `{"expected_revision":` + string(b) + `,"patch":` + patch + `}`
}
func r5EditorHTTPState(t *testing.T, f *testpg.R5HTTPFixture) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state string
	err := f.Pool.QueryRow(ctx, `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM users u),'overrides',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM user_permission_overrides o),'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM permission_profiles p),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM outbox_events o))::text`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestR5PermissionEditorHTTPJourneyCASAndLegacyClosure(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := context.Background()
	c := f.Companies[0]
	path := r5EditorHTTPPath(c.Users["sender"].ID)
	token := f.JWT(0, "admin")
	// Real bootstrap legitimately assigned a profile; edit raw intent is absent.
	old := r5EditorHTTPRead(t, f)
	if old.Profile == nil || old.Overrides != nil || old.FieldSources["can_send"] != "profile" {
		t.Fatal("GET reverse-engineered inherited values into overrides")
	}
	same := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, old.Revision, `{"can_send":true}`), 200))
	if same.Overrides == nil || same.Overrides.CanSend == nil || !*same.Overrides.CanSend || same.FieldSources["can_send"] != "override" {
		t.Fatal("same-as-inherited explicit override erased")
	}
	first := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, same.Revision, `{"can_send":false,"daily_send_quota":0,"domain_access":{"mode":"none","zone_ids":[]}}`), 200))
	if first.Effective.CanSend || first.Effective.DailySendQuota != 0 || first.Effective.AllowsZone(c.Zone.ID) || first.Overrides.DomainAccess.Mode != "none" || first.Revision.Equal(same.Revision) {
		t.Fatal("HTTP false/zero/none or durable revision lost")
	}
	quota := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, first.Revision, `{"daily_send_quota":19}`), 200))
	if quota.Effective.CanSend || quota.Effective.AllowsZone(c.Zone.ID) || quota.Effective.DailySendQuota != 19 {
		t.Fatal("HTTP A01 quota-only restored revoked fields")
	}
	stable := r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, old.Revision, `{"can_send":true}`), 409)
	r5EditorHTTPCall(t, f, token, "PATCH", path, `{"patch":{"can_send":true}}`, 400)
	legacy := strings.TrimSuffix(path, "permission-editor") + "permissions"
	r5EditorHTTPCall(t, f, token, "PUT", legacy, `{"can_send":true,"daily_send_quota":31}`, 409)
	r5EditorHTTPCall(t, f, token, "DELETE", legacy, "", 409)
	empty := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, quota.Revision, `{}`), 200))
	if !empty.Revision.Equal(quota.Revision) || r5EditorHTTPState(t, f) != stable {
		t.Fatal("stale/missing/legacy/empty request left effects")
	}
	inherited := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", path, r5EditorHTTPCommand(t, quota.Revision, `{"can_send":null,"domain_access":null}`), 200))
	if inherited.Overrides.CanSend != nil || inherited.Overrides.AllowedZoneIDs != nil || inherited.FieldSources["can_send"] != "profile" || inherited.Overrides.DomainAccess.Mode != "inherit" || inherited.Effective.DailySendQuota != 19 {
		t.Fatal("HTTP inheritance lost raw NULL or touched omission")
	}
	effective, err := f.Store.EffectivePermission(ctx, c.Users["sender"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inherited.Effective, effective) {
		t.Fatal("GET/PATCH effective diverged from canonical PgStore")
	}
	if !inherited.Capabilities.Patch || !inherited.Capabilities.AssignProfile {
		t.Fatal("editor omitted a wired and authorized assignment command")
	}
}

func TestR5PermissionEditorHTTPStrictJSONNoEffects(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	old := r5EditorHTTPRead(t, f)
	path := r5EditorHTTPPath(old.UserID)
	token := f.JWT(0, "admin")
	stable := r5EditorHTTPState(t, f)
	revision, err := json.Marshal(old.Revision)
	if err != nil {
		t.Fatal(err)
	}
	r := string(revision)
	cases := []string{
		`{"expected_revision":` + r + `,"patch":{"unknown":true}}`,
		`{"expected_revision":` + r + `,"patch":{"Can_Send":false}}`,
		`{"EXPECTED_REVISION":` + r + `,"patch":{}}`,
		`{"expected_revision":` + r + `,"patch":{},"extra":0}`,
		`{"expected_revision":` + r + `,"patch":{"can_send":false,"can_send":true}}`,
		`{"expected_revision":` + r + `,"expected_revision":` + r + `,"patch":{}}`,
		`{"expected_revision":` + r + `,"patch":{"domain_access":{"mode":"none","mode":"all","zone_ids":[]}}}`,
		`{"expected_revision":` + r + `,"patch":{"domain_access":{"Mode":"none","zone_ids":[]}}}`,
		`{"expected_revision":` + r + `,"patch":{"domain_access":{"mode":"none","zone_ids":[],"extra":true}}}`,
		r5EditorHTTPCommand(t, old.Revision, `{"daily_send_quota":-1}`),
		r5EditorHTTPCommand(t, old.Revision, `{"daily_send_quota":0.5}`),
		r5EditorHTTPCommand(t, old.Revision, `{"can_send":0}`),
		r5EditorHTTPCommand(t, old.Revision, `{"domain_access":{"mode":"list","zone_ids":[]}}`),
		r5EditorHTTPCommand(t, old.Revision, `{"domain_access":{"mode":"list","zone_ids":["invalid-uuid"]}}`),
		r5EditorHTTPCommand(t, old.Revision, `{"domain_access":{"mode":"all","zone_ids":["`+f.Companies[0].Zone.ID.String()+`"]}}`),
		`{"expected_revision":` + r + `,"patch":null}`,
		`{"expected_revision":` + r + `,"patch":{}} {}`,
		`[]`, `null`,
	}
	for i, raw := range cases {
		r5EditorHTTPCall(t, f, token, "PATCH", path, raw, 400)
		if r5EditorHTTPState(t, f) != stable {
			t.Fatalf("invalid JSON case %d left state/audit/revision effects", i)
		}
	}
	// A valid UUID from another company is a semantic tenant violation, not syntax.
	foreign := r5EditorHTTPCommand(t, old.Revision, `{"domain_access":{"mode":"list","zone_ids":["`+f.Companies[1].Zone.ID.String()+`"]}}`)
	r5EditorHTTPCall(t, f, token, "PATCH", path, foreign, 400)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("foreign domain patch left effects")
	}
}

func TestR5PermissionEditorHTTPAuthorityAndCurrentEpoch(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	c := f.Companies[0]
	old := r5EditorHTTPRead(t, f)
	path := r5EditorHTTPPath(old.UserID)
	raw := r5EditorHTTPCommand(t, old.Revision, `{"can_send":false}`)
	keyBody := r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "POST", "/api/v1/keys", `{"label":"editor-boundary","scopes":["domains:read"]}`, 201)
	key := r5EditorHTTPData[struct {
		Key string `json:"key"`
	}](t, keyBody).Key
	if !strings.HasPrefix(key, "tb_") {
		t.Fatal("actual tenant key was not issued")
	}
	stable := r5EditorHTTPState(t, f)
	for _, tc := range []struct {
		token, path    string
		status         int
		target, tenant uuid.UUID
	}{
		{"", path, 401, old.UserID, c.Tenant.ID}, {f.JWT(0, "sender"), path, 403, old.UserID, c.Tenant.ID}, {f.JWT(0, "frozen"), path, 401, old.UserID, c.Tenant.ID},
		{key, path, 403, old.UserID, c.Tenant.ID}, {f.JWT(1, "admin"), path, 404, old.UserID, f.Companies[1].Tenant.ID},
		{f.JWT(0, "admin"), r5EditorHTTPPath(c.Admin.ID), 403, c.Admin.ID, c.Tenant.ID},
	} {
		// Bind the command to the requested target/context, so this proves actual
		// authorization rather than an earlier observation-identity mismatch.
		r := old.Revision
		r.UserID = tc.target
		r.TenantID = tc.tenant
		for _, method := range []string{"GET", "PATCH"} {
			r5EditorHTTPCall(t, f, tc.token, method, tc.path, r5EditorHTTPCommand(t, r, `{"can_send":false}`), tc.status)
		}
	}
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("denied hierarchy/tenant/key requests modified state")
	}
	oldToken := f.JWT(0, "admin")
	if err := f.Store.UpdateUserPassword(context.Background(), c.Admin.ID, "test-only-current-epoch-password-hash"); err != nil {
		t.Fatal(err)
	}
	stable = r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, oldToken, "GET", path, "", 401)
	r5EditorHTTPCall(t, f, oldToken, "PATCH", path, raw, 401)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("old credential epoch produced effects")
	}
	f.RefreshJWT(t, 0, "admin")
	current := r5EditorHTTPRead(t, f)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "PATCH", path, r5EditorHTTPCommand(t, current.Revision, `{"can_send":false}`), 200)
	// Same credential epoch with an old JWT role must not retain administration.
	user, err := f.Store.GetUser(context.Background(), c.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	user.Role = models.RoleUser
	if err = f.Store.UpdateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	stable = r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "GET", path, "", 403)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "PATCH", path, raw, 403)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("cached JWT admin role retained side effects after demotion")
	}
}

func TestR5PermissionEditorHTTPAuditFailureAtomic(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	old := r5EditorHTTPRead(t, f)
	path := r5EditorHTTPPath(old.UserID)
	stable := r5EditorHTTPState(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := f.Pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT editor_http_audit_failure CHECK(action<>'permission.override.patch') NOT VALID`)
	if err != nil {
		t.Fatal(err)
	}
	raw := r5EditorHTTPCommand(t, old.Revision, `{"can_send":false,"daily_send_quota":0,"domain_access":{"mode":"none","zone_ids":[]}}`)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "PATCH", path, raw, 500)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("HTTP audit failure left full rows/revision/outbox changes")
	}
	reloaded := r5EditorHTTPRead(t, f)
	if !reloaded.Revision.Equal(old.Revision) {
		t.Fatal("rolled-back command consumed persistent revision")
	}
	_, err = f.Pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT editor_http_audit_failure`)
	if err != nil {
		t.Fatal(err)
	}
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "PATCH", path, raw, 200)
	var audit json.RawMessage
	err = f.Pool.QueryRow(ctx, `SELECT details FROM audit_log WHERE action='permission.override.patch' AND resource_id=$1`, old.UserID).Scan(&audit)
	if err != nil {
		t.Fatal(err)
	}
	var details map[string]json.RawMessage
	if err = json.Unmarshal(audit, &details); err != nil {
		t.Fatal(err)
	}
	if len(details) != 3 || details["changed_fields"] == nil || details["before_revision"] == nil || details["after_revision"] == nil {
		t.Fatal("required audit contains wrong detail boundary")
	}
	if bytes.Contains(audit, []byte("password")) {
		t.Fatal("audit leaked secret fields")
	}
}

func TestR5PermissionEditorHTTPABAStaleObservation(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	old := r5EditorHTTPRead(t, f)
	c := f.Companies[0]
	ctx := context.Background()
	value := true
	if err := f.Store.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: old.UserID, CanSend: &value}); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DeleteUserPermissionOverride(ctx, old.UserID); err != nil {
		t.Fatal(err)
	}
	now := r5EditorHTTPRead(t, f)
	if now.Revision.Equal(old.Revision) {
		t.Fatal("raw override absence ABA restored revision")
	}
	stable := r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "PATCH", r5EditorHTTPPath(c.Users["sender"].ID), r5EditorHTTPCommand(t, old.Revision, `{"can_send":false}`), 409)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("HTTP ABA stale write left effects")
	}
}

func TestR5PermissionEditorHTTPRawNullEmptyAndDefault(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	c := f.Companies[0]
	u := c.Users["sender"]
	ctx := context.Background()
	// Legacy state fixtures are installed through actual store commands, not an
	// invented HTTP assignment/CAS protocol. They only prepare observable rows.
	p := &models.PermissionProfile{TenantID: &c.Tenant.ID, Name: "HTTP editor raw " + uuid.NewString(), CanSend: true, DailySendQuota: 31, DailyReceiveQuota: 41, MaxMailboxes: 8, MaxDomains: 2, AllowedZoneIDs: []uuid.UUID{c.Zone.ID}, CanCreateAPIKeys: true}
	if err := f.Store.CreatePermissionProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store.UpdateUserGuarded(ctx, c.Actor, c.Tenant.ID, u.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &p.ID}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"null", "empty"} {
		override := &models.UserPermissionOverride{UserID: u.ID}
		if kind == "empty" {
			override.AllowedZoneIDs = []uuid.UUID{}
		}
		if err := f.Store.UpsertUserPermissionOverride(ctx, override); err != nil {
			t.Fatal(err)
		}
		body := r5EditorHTTPCall(t, f, f.JWT(0, "admin"), "GET", r5EditorHTTPPath(u.ID), "", 200)
		s := r5EditorHTTPData[company.PermissionEditorSnapshot](t, body)
		var wire struct {
			Data struct {
				Overrides map[string]json.RawMessage `json:"overrides"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		allowed := bytes.TrimSpace(wire.Data.Overrides["allowed_zone_ids"])
		if kind == "null" {
			if !bytes.Equal(allowed, []byte("null")) || s.Overrides.DomainAccess.Mode != "inherit" || s.FieldSources["domain_access"] != "profile" || s.Effective.AllowsZone(uuid.New()) {
				t.Fatal("raw SQL NULL/inherited domain serialized as explicit all")
			}
		} else {
			if !bytes.Equal(allowed, []byte("[]")) || s.Overrides.DomainAccess.Mode != "all" || s.FieldSources["domain_access"] != "override" || !s.Effective.AllowsZone(uuid.New()) {
				t.Fatal("legacy explicit [] collapsed to NULL/inheritance")
			}
		}
	}
	if err := f.Store.DeleteUserPermissionOverride(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store.UpdateUserGuarded(ctx, c.Actor, c.Tenant.ID, u.ID, models.UserAdminPatch{SetPermissionProfile: true}); err != nil {
		t.Fatal(err)
	}
	s := r5EditorHTTPRead(t, f)
	if s.Profile != nil || s.Overrides != nil || s.Revision.ProfileID != nil || s.Revision.ProfileRevision != nil || s.Effective.CanSend || s.Effective.DailySendQuota != 0 || s.Effective.DailyReceiveQuota != 500 || s.Effective.MaxMailboxes != 10 || s.Effective.MaxDomains != 1 || !s.Effective.CanCreateAPIKeys {
		t.Fatal("HTTP unprofiled default or raw absence changed")
	}
	for field, source := range s.FieldSources {
		if source != "default" {
			t.Fatalf("unprofiled %s source=%s", field, source)
		}
	}
}

func r5EditorHTTPJSON(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

// Internal PermissionField structs are presence-aware decoder state, not wire
// DTOs. An untouched assignment patch is the explicit JSON object {}, while
// profile identity and revision must both be present even when nullable.
func r5EditorHTTPAssignmentJSON(t *testing.T, cmd company.PermissionAssignmentCommand) string {
	t.Helper()
	if !cmd.Patch.Empty() {
		t.Fatal("assignment fixture requires an explicitly serialized field patch")
	}
	return r5EditorHTTPJSON(t, map[string]any{
		"expected_revision": cmd.ExpectedRevision,
		"profile_id":        cmd.ProfileID,
		"profile_revision":  cmd.ProfileRevision,
		"patch":             map[string]any{},
	})
}
func TestR5PermissionEditorHTTPFormalProfileAssignmentAndDelete(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	token := f.JWT(0, "admin")
	c := f.Companies[0]
	old := r5EditorHTTPRead(t, f)
	base := r5EditorHTTPPath(old.UserID)
	created := r5EditorHTTPData[models.PermissionProfile](t, r5EditorHTTPCall(t, f, token, "POST", "/api/v1/admin/permissions", `{"name":"HTTP formal profile","can_send":true,"daily_send_quota":17,"daily_receive_quota":51,"max_mailboxes":3,"max_domains":1,"can_create_api_keys":true}`, 201))
	if created.Revision == "" {
		t.Fatal("created profile lacks CAS revision")
	}
	profilePath := "/api/v1/admin/permissions/" + created.ID.String()
	first := r5EditorHTTPData[models.PermissionProfile](t, r5EditorHTTPCall(t, f, token, "PATCH", profilePath, r5EditorHTTPJSON(t, map[string]any{"expected_revision": created.Revision, "can_send": false}), 200))
	if first.CanSend || first.Revision == created.Revision {
		t.Fatal("formal HTTP profile CAS did not consume revision")
	}
	stable := r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, token, "PATCH", profilePath, r5EditorHTTPJSON(t, map[string]any{"expected_revision": created.Revision, "can_send": true}), 409)
	r5EditorHTTPCall(t, f, token, "PATCH", profilePath, `{"can_send":true}`, 409)
	r5EditorHTTPCall(t, f, token, "DELETE", profilePath, "", 400)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("old incomplete profile commands left effects")
	}
	assignment := company.PermissionAssignmentCommand{ExpectedRevision: old.Revision, ProfileID: &first.ID, ProfileRevision: &first.Revision}
	assigned := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "POST", base+"/assignment", r5EditorHTTPAssignmentJSON(t, assignment), 200))
	if assigned.Profile == nil || assigned.Profile.ID != first.ID || assigned.Effective.CanSend || assigned.Revision.Equal(old.Revision) {
		t.Fatal("formal HTTP assignment lost selected revision/state")
	}
	stable = r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, token, "POST", base+"/assignment", r5EditorHTTPAssignmentJSON(t, assignment), 409)
	assignment.ExpectedRevision = assigned.Revision
	assignment.ProfileRevision = &created.Revision
	r5EditorHTTPCall(t, f, token, "POST", base+"/assignment", r5EditorHTTPAssignmentJSON(t, assignment), 409)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("stale member/selected-profile assignment left effects")
	}
	preview := r5EditorHTTPData[company.PermissionProfileDeletionPreview](t, r5EditorHTTPCall(t, f, token, "GET", profilePath+"/deletion-preview", "", 200))
	if len(preview.Members) != 1 || len(preview.Changes) != 1 || !preview.Members[0].Equal(assigned.Revision) || !reflect.DeepEqual(preview.Changes[0].Before, assigned.Effective) {
		t.Fatal("HTTP actual deletion preview missing effect scope")
	}
	// A raw override changes deletion's inherited outcome without changing P.
	changed := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5EditorHTTPCall(t, f, token, "PATCH", base, r5EditorHTTPCommand(t, assigned.Revision, `{"can_send":true}`), 200))
	stable = r5EditorHTTPState(t, f)
	deletion := map[string]any{"expected_revision": preview.ProfileRevision, "confirmed_members": preview.Members}
	r5EditorHTTPCall(t, f, token, "DELETE", profilePath, r5EditorHTTPJSON(t, deletion), 409)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("old preview deleted changed member")
	}
	current := r5EditorHTTPData[company.PermissionProfileDeletionPreview](t, r5EditorHTTPCall(t, f, token, "GET", profilePath+"/deletion-preview", "", 200))
	deletion = map[string]any{"expected_revision": current.ProfileRevision, "confirmed_members": current.Members}
	r5EditorHTTPCall(t, f, token, "DELETE", profilePath, r5EditorHTTPJSON(t, deletion), 204)
	detached := r5EditorHTTPRead(t, f)
	if detached.Profile != nil || !reflect.DeepEqual(detached.Effective, current.Changes[0].After) || detached.Revision.UserRevision == changed.Revision.UserRevision || detached.UserID != c.Users["sender"].ID {
		t.Fatal("HTTP confirmed deletion disagrees with actual preview")
	}
	stable = r5EditorHTTPState(t, f)
	r5EditorHTTPCall(t, f, token, "DELETE", profilePath, r5EditorHTTPJSON(t, deletion), 404)
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("missing profile delete left effects")
	}
}

func TestR5PermissionEditorHTTPFormalCommandStrictness(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	token := f.JWT(0, "admin")
	s := r5EditorHTTPRead(t, f)
	r := r5EditorHTTPJSON(t, s.Revision)
	base := r5EditorHTTPPath(s.UserID)
	stable := r5EditorHTTPState(t, f)
	for _, raw := range []string{
		`{"expected_revision":` + r + `,"patch":{}}`,
		`{"expected_revision":` + r + `,"profile_id":null,"patch":{}}`,
		`{"expected_revision":` + r + `,"profile_id":null,"profile_revision":"1","patch":{}}`,
		`{"expected_revision":` + r + `,"profile_id":null,"profile_revision":null}`,
		`{"expected_revision":` + r + `,"profile_id":null,"profile_revision":null,"patch":{},"unknown":false}`,
		`{"expected_revision":` + r + `,"profile_id":null,"profile_id":null,"profile_revision":null,"patch":{}}`,
		`{"expected_revision":` + r + `,"Profile_ID":null,"profile_revision":null,"patch":{}}`,
	} {
		r5EditorHTTPCall(t, f, token, "POST", base+"/assignment", raw, 400)
	}
	profilePath := "/api/v1/admin/permissions/" + s.Profile.ID.String()
	for _, raw := range []string{
		`{"expected_revision":"` + s.Profile.Revision + `","can_send":false,"can_send":true}`,
		`{"expected_revision":"` + s.Profile.Revision + `","Can_Send":false}`,
		`{"expected_revision":"` + s.Profile.Revision + `","unknown":true}`,
	} {
		r5EditorHTTPCall(t, f, token, "PATCH", profilePath, raw, 400)
	}
	for _, raw := range []string{
		`{"expected_revision":"` + s.Profile.Revision + `"}`,
		`{"expected_revision":"` + s.Profile.Revision + `","confirmed_members":null}`,
		`{"expected_revision":"` + s.Profile.Revision + `","confirmed_members":[],"unknown":true}`,
		`{"expected_revision":"` + s.Profile.Revision + `","confirmed_members":[],"confirmed_members":[]}`,
	} {
		r5EditorHTTPCall(t, f, token, "DELETE", profilePath, raw, 400)
	}
	if r5EditorHTTPState(t, f) != stable {
		t.Fatal("incomplete/ambiguous formal command left effects")
	}
}
