package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"strconv"
	"strings"
	"tabmail/internal/authz"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// All writes here use real PgStore commands. SQL is limited to observing
// persistent state or holding a lock; it never impersonates a CAS command.
func r5EditorSeed(t *testing.T) (*companyFixture, *models.PermissionProfile) {
	t.Helper()
	f := seedCompany(t)
	p := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "Editor " + uuid.NewString(), CanSend: true, DailySendQuota: 31, DailyReceiveQuota: 41, MaxMailboxes: 8, MaxDomains: 2, AllowedZoneIDs: []uuid.UUID{f.zone.ID}, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	_, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &p.ID})
	must(t, err)
	return f, p
}
func r5EditorRead(t *testing.T, f *companyFixture) *company.PermissionEditorSnapshot {
	t.Helper()
	s, err := f.st.GetPermissionEditorSnapshot(context.Background(), f.a, f.employee.ID)
	must(t, err)
	if s == nil || s.Effective == nil || s.UserID != f.employee.ID || s.TenantID != f.tenant.ID {
		t.Fatalf("invalid editor identity/effective: %+v", s)
	}
	must(t, s.Revision.Validate())
	ep, err := f.st.EffectivePermission(context.Background(), f.employee.ID)
	must(t, err)
	if !reflect.DeepEqual(s.Effective, ep) {
		t.Fatalf("editor diverged from canonical effective: editor=%+v effective=%+v", s.Effective, ep)
	}
	return s
}

func TestR5PermissionEditorRawSources(t *testing.T) {
	for _, kind := range []string{"absent", "null-fields", "false-zero", "same-as-profile"} {
		t.Run(kind, func(t *testing.T) {
			f, p := r5EditorSeed(t)
			if kind != "absent" {
				o := &models.UserPermissionOverride{UserID: f.employee.ID}
				if kind == "false-zero" {
					v, q := false, 0
					o.CanSend = &v
					o.DailySendQuota = &q
				}
				if kind == "same-as-profile" {
					v, q := p.CanSend, p.DailySendQuota
					o.CanSend = &v
					o.DailySendQuota = &q
				}
				must(t, f.st.UpsertUserPermissionOverride(context.Background(), o))
			}
			s := r5EditorRead(t, f)
			if s.Profile == nil || s.Profile.ID != p.ID {
				t.Fatal("raw assigned profile missing")
			}
			if (s.Overrides == nil) != (kind == "absent") {
				t.Fatalf("override presence lost: %s %+v", kind, s.Overrides)
			}
			wantSource := "profile"
			if kind == "false-zero" || kind == "same-as-profile" {
				wantSource = "override"
			}
			for _, field := range []string{"can_send", "daily_send_quota"} {
				if s.FieldSources[field] != wantSource {
					t.Fatalf("%s source=%q want=%q", field, s.FieldSources[field], wantSource)
				}
			}
			if kind == "null-fields" && (s.Overrides.CanSend != nil || s.Overrides.DailySendQuota != nil) {
				t.Fatal("NULL converted to explicit values")
			}
			if kind == "false-zero" && (s.Overrides.CanSend == nil || *s.Overrides.CanSend || s.Overrides.DailySendQuota == nil || *s.Overrides.DailySendQuota != 0 || s.Effective.CanSend || s.Effective.DailySendQuota != 0) {
				t.Fatal("explicit false/zero lost")
			}
			if kind == "same-as-profile" && (s.Overrides.CanSend == nil || s.Overrides.DailySendQuota == nil) {
				t.Fatal("equal inherited value erased explicit override intent")
			}
		})
	}
}

func TestR5PermissionEditorLegacyDomainIntent(t *testing.T) {
	for _, kind := range []string{"null", "empty", "list"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := r5EditorSeed(t)
			o := &models.UserPermissionOverride{UserID: f.employee.ID}
			mode, source := "inherit", "profile"
			if kind == "empty" {
				o.AllowedZoneIDs = []uuid.UUID{}
				mode, source = "all", "override"
			}
			if kind == "list" {
				o.AllowedZoneIDs = []uuid.UUID{f.zone.ID}
				mode, source = "list", "override"
			}
			must(t, f.st.UpsertUserPermissionOverride(context.Background(), o))
			s := r5EditorRead(t, f)
			if s.Overrides == nil || s.Overrides.DomainAccess.Mode != mode || s.FieldSources["domain_access"] != source {
				t.Fatalf("legacy %s mapping lost: %+v", kind, s)
			}
			if (s.Overrides.AllowedZoneIDs == nil) != (kind == "null") {
				t.Fatal("SQL NULL and empty array collapsed")
			}
			if kind == "empty" && !s.Effective.AllowsZone(uuid.New()) {
				t.Fatal("legacy empty allowlist no longer means all")
			}
			if kind != "empty" && s.Effective.AllowsZone(uuid.New()) {
				t.Fatal("inherited/list restriction widened")
			}
		})
	}
}

func TestR5PermissionEditorNoProfileDefaults(t *testing.T) {
	f, _ := r5EditorSeed(t)
	_, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true})
	must(t, err)
	s := r5EditorRead(t, f)
	if s.Profile != nil || s.Overrides != nil || s.Revision.ProfileID != nil || s.Revision.ProfileRevision != nil {
		t.Fatal("absent profile fabricated")
	}
	for field, source := range s.FieldSources {
		if source != "default" {
			t.Fatalf("%s source=%s", field, source)
		}
	}
	p := s.Effective
	if p.CanSend || p.DailySendQuota != 0 || p.DailyReceiveQuota != 500 || p.MaxMailboxes != 10 || p.MaxDomains != 1 || p.CanCreateDomains || p.CanCreateRoutes || !p.CanCreateAPIKeys {
		t.Fatalf("legacy defaults changed: %+v", p)
	}
}

func TestR5PermissionEditorRevisionOwnersAndABA(t *testing.T) {
	f, p := r5EditorSeed(t)
	old := r5EditorRead(t, f)
	initial, _ := strconv.ParseInt(old.Revision.UserRevision, 10, 64)
	allow := true
	for _, op := range []func() error{
		func() error {
			return f.st.UpsertUserPermissionOverride(context.Background(), &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &allow})
		},
		func() error { return f.st.DeleteUserPermissionOverride(context.Background(), f.employee.ID) },
		func() error {
			return f.st.UpsertUserPermissionOverride(context.Background(), &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &allow})
		},
	} {
		must(t, op())
		s := r5EditorRead(t, f)
		next, _ := strconv.ParseInt(s.Revision.UserRevision, 10, 64)
		if next <= initial || old.Revision.Equal(s.Revision) {
			t.Fatal("override insertion/deletion ABA reused revision")
		}
		initial = next
	}
	must(t, f.st.DeleteUserPermissionOverride(context.Background(), f.employee.ID))
	before := r5EditorRead(t, f)
	p.CanSend = false
	must(t, f.st.UpdatePermissionProfile(context.Background(), p))
	after := r5EditorRead(t, f)
	if before.Revision.ProfileRevision == nil || after.Revision.ProfileRevision == nil || *before.Revision.ProfileRevision == *after.Revision.ProfileRevision || before.Revision.Equal(after.Revision) {
		t.Fatal("profile update did not invalidate compound revision")
	}
	for _, id := range []*uuid.UUID{nil, &p.ID} {
		_, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: id})
		must(t, err)
	}
	returned := r5EditorRead(t, f)
	if returned.Revision.Equal(after.Revision) || returned.Revision.UserRevision == after.Revision.UserRevision {
		t.Fatal("profile assignment ABA reused user revision")
	}
	must(t, f.st.DeletePermissionProfile(context.Background(), p.ID, &f.tenant.ID))
	deleted := r5EditorRead(t, f)
	if deleted.Profile != nil || deleted.Revision.UserRevision == returned.Revision.UserRevision {
		t.Fatal("profile deletion did not fence SET NULL inheritance change")
	}
}

func TestR5PermissionEditorBusyProfileFailsClosed(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `SELECT id FROM permission_profiles WHERE id=$1 FOR UPDATE`, p.ID)
	must(t, err)
	s, err := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.employee.ID)
	e, ok := app.As(err)
	if !ok || e.Kind != app.KindConflict || s != nil {
		t.Fatalf("busy profile leaked a mixed editor: snapshot=%+v error=%v", s, err)
	}
	must(t, tx.Rollback(ctx))
	r5EditorRead(t, f)
}

func TestR5PermissionEditorConcurrentSnapshotNeverMixes(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	// Each real profile update changes the coupled values in one SQL statement.
	// A mixed read would expose CanSend from one state and quota from the other.
	p.CanSend = true
	p.DailySendQuota = 31
	must(t, f.st.UpdatePermissionProfile(ctx, p))
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < 30; i++ {
			v := *p
			v.CanSend = i%2 == 0
			if v.CanSend {
				v.DailySendQuota = 31
			} else {
				v.DailySendQuota = 71
			}
			if err := f.st.UpdatePermissionProfile(ctx, &v); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(start)
	successes := 0
	for i := 0; i < 60; i++ {
		s, err := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.employee.ID)
		if err != nil {
			if e, ok := app.As(err); ok && e.Kind == app.KindConflict {
				continue
			}
			t.Fatal(err)
		}
		successes++
		if s.Profile == nil || s.Overrides != nil || s.Effective.CanSend != s.Profile.CanSend || s.Effective.DailySendQuota != s.Profile.DailySendQuota || (s.Profile.CanSend && s.Profile.DailySendQuota != 31) || (!s.Profile.CanSend && s.Profile.DailySendQuota != 71) {
			t.Fatalf("mixed snapshot: %+v", s)
		}
		must(t, s.Revision.Validate())
	}
	select {
	case err := <-done:
		must(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if successes == 0 {
		t.Fatal("no successful concurrent editor observation")
	}
}

func TestR5PermissionEditorProfileIdentityRecreationABA(t *testing.T) {
	f, p := r5EditorSeed(t)
	before := r5EditorRead(t, f)
	must(t, f.st.DeletePermissionProfile(context.Background(), p.ID, &f.tenant.ID))
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	_, err := f.st.UpdateUserGuarded(context.Background(), f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &p.ID})
	must(t, err)
	after := r5EditorRead(t, f)
	if before.Revision.Equal(after.Revision) || before.Revision.ProfileRevision == nil || after.Revision.ProfileRevision == nil {
		t.Fatal("same UUID recreation restored old observation")
	}
	old, _ := strconv.ParseInt(*before.Revision.ProfileRevision, 10, 64)
	next, _ := strconv.ParseInt(*after.Revision.ProfileRevision, 10, 64)
	if next <= old {
		t.Fatal("profile identity recreation reset durable version")
	}
}

func r5EditorPatch(t *testing.T, raw string) company.PermissionPatch {
	t.Helper()
	var p company.PermissionPatch
	must(t, json.Unmarshal([]byte(raw), &p))
	return p
}
func r5EditorState(t *testing.T, f *companyFixture) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('user',(SELECT to_jsonb(u) FROM users u WHERE id=$1),'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM users u),'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM permission_profiles p),'override',(SELECT to_jsonb(o) FROM user_permission_overrides o WHERE user_id=$1),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM outbox_events o))::text`, f.employee.ID).Scan(&state))
	return state
}
func TestR5PermissionEditorPatchCASAndIntent(t *testing.T) {
	f, _ := r5EditorSeed(t)
	ctx := context.Background()
	old := r5EditorRead(t, f)
	first, err := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: r5EditorPatch(t, `{"can_send":false,"daily_send_quota":0,"domain_access":{"mode":"none","zone_ids":[]}}`)})
	must(t, err)
	if first.Effective.CanSend || first.Effective.DailySendQuota != 0 || first.Effective.AllowsZone(f.zone.ID) || first.Overrides.DomainAccess.Mode != "none" {
		t.Fatal("false/zero/domain-none patch lost intent")
	}
	second, err := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: first.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":19}`)})
	must(t, err)
	if second.Effective.CanSend || second.Effective.DailySendQuota != 19 || second.Effective.AllowsZone(f.zone.ID) || second.Overrides.DomainAccess.Mode != "none" {
		t.Fatal("A01 quota-only patch restored send/domain permissions")
	}
	stable := r5EditorState(t, f)
	for _, tc := range []struct {
		name     string
		revision company.PermissionRevision
		kind     app.ErrorKind
	}{
		{"stale", old.Revision, app.KindConflict}, {"missing", company.PermissionRevision{}, app.KindBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: tc.revision, Patch: r5EditorPatch(t, `{"can_send":true}`)})
			ae, ok := app.As(e)
			if !ok || ae.Kind != tc.kind || got != nil {
				t.Fatalf("bad CAS error: %v %+v", e, got)
			}
			if r5EditorState(t, f) != stable {
				t.Fatal("rejected CAS left side effects including revision/audit")
			}
		})
	}
	empty, err := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: second.Revision})
	must(t, err)
	if !empty.Revision.Equal(second.Revision) || r5EditorState(t, f) != stable {
		t.Fatal("empty patch consumed revision or changed permissions/audit")
	}
	inherited, err := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: second.Revision, Patch: r5EditorPatch(t, `{"can_send":null,"domain_access":null}`)})
	must(t, err)
	if inherited.Overrides.CanSend != nil || inherited.Overrides.AllowedZoneIDs != nil || inherited.Overrides.DomainAccess.Mode != "inherit" || inherited.FieldSources["can_send"] != "profile" || !inherited.Effective.CanSend || !inherited.Effective.AllowsZone(f.zone.ID) || inherited.Effective.DailySendQuota != 19 {
		t.Fatal("explicit inheritance failed or touched omitted quota")
	}
}
func TestR5PermissionEditorAuditFailureRollsBackAll(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			f, _ := r5EditorSeed(t)
			ctx := context.Background()
			if existing {
				deny := false
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny}))
			}
			old := r5EditorRead(t, f)
			before := r5EditorState(t, f)
			_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT editor_audit_failure CHECK(action<>'permission.override.patch') NOT VALID`)
			must(t, err)
			got, err := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":99}`)})
			if err == nil || got != nil {
				t.Fatal("audit failure ignored or uncommitted snapshot returned")
			}
			if r5EditorState(t, f) != before {
				t.Fatal("audit failure changed full user/override/revision/outbox state")
			}
			_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT editor_audit_failure`)
			must(t, err)
			_, err = f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":99}`)})
			must(t, err)
		})
	}
}
func TestR5PermissionEditorAuthorityReload(t *testing.T) {
	for _, kind := range []string{"employee", "peer-admin", "foreign", "api-key", "frozen", "demoted", "missing-user"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := r5EditorSeed(t)
			ctx := context.Background()
			observed := r5EditorRead(t, f)
			actor, target := f.a, f.employee.ID
			switch kind {
			case "employee":
				actor = f.u
			case "peer-admin":
				target = f.admin.ID
			case "foreign":
				other := &models.User{TenantID: uuid.New(), Email: "foreign@test.invalid", Role: models.RoleUser, IsActive: true}
				tenant := &models.Tenant{ID: other.TenantID, Name: "Foreign", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, tenant))
				must(t, f.st.CreateUser(ctx, other))
				target = other.ID
			case "api-key":
				actor.Type = authz.PrincipalAPIKey
				actor.ID = uuid.New()
				actor.OwnerUserID = &f.admin.ID
			case "frozen":
				u, e := f.st.GetUser(ctx, f.admin.ID)
				must(t, e)
				u.IsActive = false
				must(t, f.st.UpdateUser(ctx, u))
			case "demoted":
				u, e := f.st.GetUser(ctx, f.admin.ID)
				must(t, e)
				u.Role = models.RoleUser
				must(t, f.st.UpdateUser(ctx, u))
			case "missing-user":
				target = uuid.New()
			}
			before := r5EditorState(t, f)
			if got, e := f.st.GetPermissionEditorSnapshot(ctx, actor, target); e == nil || got != nil {
				t.Fatal("unauthorized raw editor read succeeded")
			}
			if got, e := f.st.PatchPermissionEditor(ctx, actor, target, company.PermissionEditorCommand{ExpectedRevision: observed.Revision, Patch: r5EditorPatch(t, `{"can_send":true}`)}); e == nil || got != nil {
				t.Fatal("unauthorized/stale-actor permission write succeeded")
			}
			if r5EditorState(t, f) != before {
				t.Fatal("denied read/write left side effects")
			}
		})
	}
}

func TestR5PermissionEditorABARejectsOldCAS(t *testing.T) {
	for _, kind := range []string{"override", "profile", "assignment"} {
		t.Run(kind, func(t *testing.T) {
			f, p := r5EditorSeed(t)
			ctx := context.Background()
			old := r5EditorRead(t, f)
			switch kind {
			case "override":
				v := true
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &v}))
				must(t, f.st.DeleteUserPermissionOverride(ctx, f.employee.ID))
			case "profile":
				must(t, f.st.DeletePermissionProfile(ctx, p.ID, &f.tenant.ID))
				must(t, f.st.CreatePermissionProfile(ctx, p))
				_, e := f.st.UpdateUserGuarded(ctx, f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &p.ID})
				must(t, e)
			case "assignment":
				for _, id := range []*uuid.UUID{nil, &p.ID} {
					_, e := f.st.UpdateUserGuarded(ctx, f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: id})
					must(t, e)
				}
			}
			stable := r5EditorState(t, f)
			got, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":99}`)})
			ae, ok := app.As(e)
			if !ok || ae.Kind != app.KindConflict || got != nil {
				t.Fatalf("ABA accepted old CAS: %v", e)
			}
			if r5EditorState(t, f) != stable {
				t.Fatal("stale ABA command left effects")
			}
		})
	}
}

func TestR5PermissionEditorConcurrentCASSingleWinner(t *testing.T) {
	f, _ := r5EditorSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	observed := r5EditorRead(t, f)
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, raw := range []string{`{"daily_send_quota":11}`, `{"daily_send_quota":22}`} {
		p := r5EditorPatch(t, raw)
		go func() {
			<-start
			_, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: observed.Revision, Patch: p})
			done <- e
		}()
	}
	close(start)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case e := <-done:
			if e == nil {
				success++
			} else if ae, ok := app.As(e); ok && ae.Kind == app.KindConflict {
				conflict++
			} else {
				t.Fatal(e)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS winners=%d conflicts=%d", success, conflict)
	}
	var audits int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='permission.override.patch' AND resource_id=$1`, f.employee.ID).Scan(&audits))
	if audits != 1 {
		t.Fatalf("CAS audit count=%d", audits)
	}
	s := r5EditorRead(t, f)
	if s.Effective.DailySendQuota != 11 && s.Effective.DailySendQuota != 22 {
		t.Fatal("no winner persisted")
	}
}

func TestR5PermissionEditorDomainModesAndInvalidZone(t *testing.T) {
	f, _ := r5EditorSeed(t)
	ctx := context.Background()
	for _, mode := range []string{"none", "all", "list", "inherit"} {
		old := r5EditorRead(t, f)
		ids := []uuid.UUID{}
		if mode == "list" {
			ids = []uuid.UUID{f.zone.ID}
		}
		patch := company.PermissionPatch{DomainAccess: company.PermissionField[company.DomainAccess]{Present: true, Value: company.DomainAccess{Mode: mode, ZoneIDs: ids}}}
		got, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: patch})
		must(t, e)
		if got.Overrides.DomainAccess.Mode != mode {
			t.Fatalf("mode %s collapsed", mode)
		}
		switch mode {
		case "none":
			if got.Effective.AllowsZone(f.zone.ID) || got.Effective.AllowsZone(uuid.New()) {
				t.Fatal("none widened to all")
			}
		case "all":
			if !got.Effective.AllowsZone(uuid.New()) {
				t.Fatal("all not unrestricted")
			}
		case "list", "inherit":
			if !got.Effective.AllowsZone(f.zone.ID) || got.Effective.AllowsZone(uuid.New()) {
				t.Fatal("list/inherit lost canonical zone membership")
			}
		}
		if (got.Overrides.AllowedZoneIDs == nil) != (mode == "inherit") {
			t.Fatal("mode storage NULL/empty distinction lost")
		}
	}
	old := r5EditorRead(t, f)
	before := r5EditorState(t, f)
	invalid := company.PermissionPatch{DomainAccess: company.PermissionField[company.DomainAccess]{Present: true, Value: company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{uuid.New()}}}}
	got, e := f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: old.Revision, Patch: invalid})
	ae, ok := app.As(e)
	if !ok || ae.Kind != app.KindBadRequest || got != nil || r5EditorState(t, f) != before {
		t.Fatal("unavailable zone accepted or left side effects")
	}
}

func r5EditorRequireKind(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	if authz.IsAuthzError(err) {
		err = app.FromAuthz(err)
	}
	e, ok := app.As(err)
	if !ok || e.Kind != kind {
		t.Fatalf("error=%v want kind=%s", err, kind)
	}
}
func r5EditorSelectedProfile(t *testing.T, f *companyFixture, name string) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: name + uuid.NewString(), CanSend: false, DailySendQuota: 7, DailyReceiveQuota: 51, MaxMailboxes: 3, MaxDomains: 1, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	got, e := f.st.GetPermissionProfile(context.Background(), p.ID)
	must(t, e)
	if got == nil || got.Revision == "" {
		t.Fatal("selected profile lacks persisted observation")
	}
	return got
}
func r5EditorAssignment(t *testing.T, f *companyFixture, r company.PermissionRevision, p *models.PermissionProfile, patch company.PermissionPatch) *company.PermissionEditorSnapshot {
	t.Helper()
	cmd := company.PermissionAssignmentCommand{ExpectedRevision: r, Patch: patch}
	if p != nil {
		cmd.ProfileID = &p.ID
		cmd.ProfileRevision = &p.Revision
	}
	got, e := f.st.AssignPermissionEditor(context.Background(), f.a, f.employee.ID, cmd)
	must(t, e)
	if got == nil {
		t.Fatal("assignment missing committed editor")
	}
	return got
}

func TestR5PermissionEditorFormalProfileCASLifecycle(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	before := r5EditorRead(t, f)
	desired := *before.Profile
	desired.CanSend = false
	desired.DailySendQuota = 17
	changed, e := f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, before.Profile.Revision)
	must(t, e)
	if changed == nil || changed.Revision == before.Profile.Revision {
		t.Fatal("formal profile CAS failed to advance version")
	}
	after := r5EditorRead(t, f)
	if after.Effective.CanSend || after.Effective.DailySendQuota != 17 {
		t.Fatal("formal profile CAS did not reach effective")
	}
	stable := r5EditorState(t, f)
	desired.CanSend = true
	_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, before.Profile.Revision)
	r5EditorRequireKind(t, e, app.KindConflict)
	_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, "")
	r5EditorRequireKind(t, e, app.KindBadRequest)
	if r5EditorState(t, f) != stable {
		t.Fatal("stale/missing profile CAS left effects")
	}
	preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	if preview == nil || preview.ProfileID != p.ID || preview.ProfileRevision != changed.Revision || len(preview.Members) != 1 || len(preview.Changes) != 1 || !preview.Members[0].Equal(after.Revision) || !reflect.DeepEqual(preview.Changes[0].Before, after.Effective) {
		t.Fatal("formal preview failed to report actual member/effective observation")
	}
	must(t, f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members))
	detached := r5EditorRead(t, f)
	if detached.Profile != nil || !reflect.DeepEqual(detached.Effective, preview.Changes[0].After) || detached.Revision.UserRevision == after.Revision.UserRevision {
		t.Fatal("confirmed deletion differs from preview or reused assignment version")
	}
	stable = r5EditorState(t, f)
	e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
	r5EditorRequireKind(t, e, app.KindNotFound)
	if r5EditorState(t, f) != stable {
		t.Fatal("missing deletion reported effects")
	}
}

func TestR5PermissionEditorFormalAssignmentCASAndABA(t *testing.T) {
	f, _ := r5EditorSeed(t)
	ctx := context.Background()
	a := r5EditorRead(t, f)
	b := r5EditorSelectedProfile(t, f, "B ")
	assigned := r5EditorAssignment(t, f, a.Revision, b, r5EditorPatch(t, `{"can_send":false,"domain_access":{"mode":"none","zone_ids":[]}}`))
	if assigned.Profile.ID != b.ID || assigned.Effective.CanSend || assigned.Effective.AllowsZone(f.zone.ID) {
		t.Fatal("assignment+override did not commit together")
	}
	stable := r5EditorState(t, f)
	_, e := f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: a.Revision, ProfileID: &a.Profile.ID, ProfileRevision: &a.Profile.Revision})
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("stale assignment left partial changes")
	}
	returned := r5EditorAssignment(t, f, assigned.Revision, a.Profile, company.PermissionPatch{})
	if returned.Revision.Equal(a.Revision) || returned.Profile.ID != a.Profile.ID {
		t.Fatal("A→B→A formal assignment reused observation")
	}
	stable = r5EditorState(t, f)
	_, e = f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: a.Revision, Patch: r5EditorPatch(t, `{"can_send":true}`)})
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("A→B→A stale patch restored old permissions")
	}
	// Selected profile has its own observation, independently of target revision.
	updated := *b
	updated.CanSend = true
	newB, e := f.st.UpdatePermissionProfileCAS(ctx, f.a, &updated, b.Revision)
	must(t, e)
	stable = r5EditorState(t, f)
	_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: returned.Revision, ProfileID: &b.ID, ProfileRevision: &b.Revision})
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("stale selected profile produced half-assignment")
	}
	r5EditorAssignment(t, f, returned.Revision, newB, r5EditorPatch(t, `{"can_send":null,"domain_access":null}`))
}

func TestR5PermissionEditorFormalDeletionRejectsPreviewDrift(t *testing.T) {
	for _, kind := range []string{"override", "new-member", "profile-update"} {
		t.Run(kind, func(t *testing.T) {
			f, p := r5EditorSeed(t)
			ctx := context.Background()
			preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
			must(t, e)
			switch kind {
			case "override":
				s := r5EditorRead(t, f)
				_, e = f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: s.Revision, Patch: r5EditorPatch(t, `{"daily_send_quota":7}`)})
				must(t, e)
			case "new-member":
				s, e := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.other.ID)
				must(t, e)
				profile, e := f.st.GetPermissionProfile(ctx, p.ID)
				must(t, e)
				_, e = f.st.AssignPermissionEditor(ctx, f.a, f.other.ID, company.PermissionAssignmentCommand{ExpectedRevision: s.Revision, ProfileID: &profile.ID, ProfileRevision: &profile.Revision})
				must(t, e)
			case "profile-update":
				profile, e := f.st.GetPermissionProfile(ctx, p.ID)
				must(t, e)
				profile.CanSend = false
				_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, profile, profile.Revision)
				must(t, e)
			}
			stable := r5EditorState(t, f)
			e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
			r5EditorRequireKind(t, e, app.KindConflict)
			if r5EditorState(t, f) != stable {
				t.Fatal("stale member-set/profile preview deleted or changed state")
			}
			current, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
			must(t, e)
			must(t, f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, current.ProfileRevision, current.Members))
		})
	}
}

func TestR5PermissionEditorFormalCommandsAuditRollback(t *testing.T) {
	for _, command := range []string{"update", "delete", "assign"} {
		t.Run(command, func(t *testing.T) {
			f, p := r5EditorSeed(t)
			ctx := context.Background()
			observed := r5EditorRead(t, f)
			b := r5EditorSelectedProfile(t, f, "audit B ")
			preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
			must(t, e)
			action := map[string]string{"update": "permission.profile.update", "delete": "permission.profile.delete", "assign": "permission.profile.assign"}[command]
			_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT formal_editor_audit_failure CHECK(action<>'`+action+`') NOT VALID`)
			must(t, e)
			stable := r5EditorState(t, f)
			switch command {
			case "update":
				desired := *observed.Profile
				desired.CanSend = false
				got, err := f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, observed.Profile.Revision)
				e = err
				if got != nil {
					t.Fatal("uncommitted profile returned")
				}
			case "delete":
				e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
			case "assign":
				got, err := f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: observed.Revision, ProfileID: &b.ID, ProfileRevision: &b.Revision, Patch: r5EditorPatch(t, `{"can_send":false,"daily_send_quota":0}`)})
				e = err
				if got != nil {
					t.Fatal("uncommitted assigned editor returned")
				}
			}
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != "formal_editor_audit_failure" {
				t.Fatalf("required audit constraint error missing: %v", e)
			}
			if r5EditorState(t, f) != stable {
				t.Fatal("formal command audit failure changed users/profile/override/revisions/audit/outbox")
			}
			_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT formal_editor_audit_failure`)
			must(t, e)
			// A rollback must preserve reusable observations and release all locks.
			switch command {
			case "update":
				desired := *observed.Profile
				desired.CanSend = false
				_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, observed.Profile.Revision)
			case "delete":
				e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
			case "assign":
				_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: observed.Revision, ProfileID: &b.ID, ProfileRevision: &b.Revision, Patch: r5EditorPatch(t, `{"can_send":false}`)})
			}
			must(t, e)
		})
	}
}

func TestR5PermissionEditorFormalSameUUIDRecreation(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	old := r5EditorRead(t, f)
	preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	must(t, f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members))
	must(t, f.st.CreatePermissionProfile(ctx, p))
	recreated, e := f.st.GetPermissionProfile(ctx, p.ID)
	must(t, e)
	if recreated.Revision == old.Profile.Revision {
		t.Fatal("same UUID recreated old profile version")
	}
	detached := r5EditorRead(t, f)
	stable := r5EditorState(t, f)
	_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: detached.Revision, ProfileID: &p.ID, ProfileRevision: &old.Profile.Revision})
	r5EditorRequireKind(t, e, app.KindConflict)
	desired := *recreated
	desired.CanSend = false
	_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, old.Profile.Revision)
	r5EditorRequireKind(t, e, app.KindConflict)
	e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("same UUID stale profile consumer changed state")
	}
	r5EditorAssignment(t, f, detached.Revision, recreated, company.PermissionPatch{})
}

func r5EditorSuper(t *testing.T, f *companyFixture) authz.Actor {
	t.Helper()
	u := &models.User{TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, Email: uuid.NewString() + "@test.invalid", IsActive: true, PasswordHash: "test-only-super-hash"}
	must(t, f.st.CreateUser(context.Background(), u))
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsAdmin: true, IsSuperAdmin: true}
}
func TestR5PermissionEditorFormalProfileScopeAndHierarchy(t *testing.T) {
	for _, kind := range []string{"foreign", "global-admin-denied", "system", "affected-peer-admin"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := r5EditorSeed(t)
			ctx := context.Background()
			p := r5EditorSelectedProfile(t, f, "scope ")
			super := r5EditorSuper(t, f)
			want := app.KindForbidden
			switch kind {
			case "foreign":
				tenant := &models.Tenant{Name: "foreign", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, tenant))
				p.TenantID = &tenant.ID
				other := &models.PermissionProfile{TenantID: &tenant.ID, Name: "foreign " + uuid.NewString(), CanCreateAPIKeys: true}
				must(t, f.st.CreatePermissionProfile(ctx, other))
				p, e := f.st.GetPermissionProfile(ctx, other.ID)
				must(t, e)
				_ = p
				// Use the actual persisted foreign profile for all three operations.
				got, e := f.st.GetPermissionProfile(ctx, other.ID)
				must(t, e)
				p = got
				want = app.KindNotFound
				stable := r5EditorState(t, f)
				_, e = f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
				r5EditorRequireKind(t, e, want)
				_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, p, p.Revision)
				r5EditorRequireKind(t, e, want)
				e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, p.Revision, nil)
				r5EditorRequireKind(t, e, want)
				if r5EditorState(t, f) != stable {
					t.Fatal("foreign profile scope rejection changed state")
				}
				return
			case "global-admin-denied":
				global := &models.PermissionProfile{Name: "global " + uuid.NewString(), CanCreateAPIKeys: true}
				must(t, f.st.CreatePermissionProfile(ctx, global))
				p, e := f.st.GetPermissionProfile(ctx, global.ID)
				must(t, e)
				_ = p
				got, e := f.st.GetPermissionProfile(ctx, global.ID)
				must(t, e)
				p = got
				want = app.KindNotFound
				stable := r5EditorState(t, f)
				_, e = f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
				r5EditorRequireKind(t, e, want)
				_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, p, p.Revision)
				r5EditorRequireKind(t, e, want)
				e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, p.Revision, nil)
				r5EditorRequireKind(t, e, want)
				if r5EditorState(t, f) != stable {
					t.Fatal("tenant admin mutated global profile")
				}
				return
			case "system":
				system := &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "system " + uuid.NewString(), IsSystem: true, CanCreateAPIKeys: true}
				must(t, f.st.CreatePermissionProfile(ctx, system))
				got, e := f.st.GetPermissionProfile(ctx, system.ID)
				must(t, e)
				p = got
			case "affected-peer-admin":
				admin, e := f.st.GetPermissionEditorSnapshot(ctx, super, f.admin.ID)
				must(t, e)
				_, e = f.st.AssignPermissionEditor(ctx, super, f.admin.ID, company.PermissionAssignmentCommand{ExpectedRevision: admin.Revision, ProfileID: &p.ID, ProfileRevision: &p.Revision})
				must(t, e)
			}
			stable := r5EditorState(t, f)
			_, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
			r5EditorRequireKind(t, e, want)
			_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, p, p.Revision)
			r5EditorRequireKind(t, e, want)
			e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, p.Revision, nil)
			r5EditorRequireKind(t, e, want)
			if r5EditorState(t, f) != stable {
				t.Fatal("system/peer-member rejection changed state")
			}
		})
	}
}

func TestR5PermissionEditorFormalGlobalSuperAdminEffects(t *testing.T) {
	f, _ := r5EditorSeed(t)
	ctx := context.Background()
	super := r5EditorSuper(t, f)
	tenant := &models.Tenant{Name: "global member tenant", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, tenant))
	user := &models.User{TenantID: tenant.ID, Email: uuid.NewString() + "@test.invalid", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
	must(t, f.st.CreateUser(ctx, user))
	global := &models.PermissionProfile{Name: "global super " + uuid.NewString(), CanSend: true, DailySendQuota: 17, DailyReceiveQuota: 500, MaxMailboxes: 10, MaxDomains: 1, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(ctx, global))
	global, e := f.st.GetPermissionProfile(ctx, global.ID)
	must(t, e)
	for _, target := range []struct{ id, tenant uuid.UUID }{{f.employee.ID, f.tenant.ID}, {user.ID, tenant.ID}} {
		actor := super
		actor.TenantID = target.tenant
		s, e := f.st.GetPermissionEditorSnapshot(ctx, actor, target.id)
		must(t, e)
		_, e = f.st.AssignPermissionEditor(ctx, actor, target.id, company.PermissionAssignmentCommand{ExpectedRevision: s.Revision, ProfileID: &global.ID, ProfileRevision: &global.Revision})
		must(t, e)
	}
	desired := *global
	desired.CanSend = false
	updated, e := f.st.UpdatePermissionProfileCAS(ctx, super, &desired, global.Revision)
	must(t, e)
	preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, super, global.ID)
	must(t, e)
	if len(preview.Members) != 2 || len(preview.Changes) != 2 || preview.ProfileRevision != updated.Revision {
		t.Fatal("global preview lost cross-company effects")
	}
	for _, change := range preview.Changes {
		if change.Before.CanSend || change.After.CanSend {
			t.Fatal("global canonical before/after semantics wrong")
		}
	}
	must(t, f.st.DeletePermissionProfileCAS(ctx, super, global.ID, preview.ProfileRevision, preview.Members))
	for _, r := range preview.Members {
		p, e := f.st.EffectivePermission(ctx, r.UserID)
		must(t, e)
		if p.CanSend {
			t.Fatal("cross-company global deletion restored send")
		}
	}
}

func TestR5PermissionEditorFormalDeletionInvitationFKNoEffects(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx := context.Background()
	preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	// Insert the new FK through the legitimate invitation command AFTER preview.
	// An invitation is not a detachable user and cannot be silently ignored.
	_, e = f.st.InviteEmployee(ctx, f.a, company.InvitationInput{Email: "phantom@contact.test", LocalPart: "phantom", DisplayName: "phantom", PermissionProfileID: &p.ID}, company.Hash("formal-phantom-invitation"))
	must(t, e)
	stable := r5EditorState(t, f)
	e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("invitation FK deletion failure detached users or lost profile/audit")
	}
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM employee_invitations WHERE permission_profile_id=$1`, p.ID).Scan(&count))
	if count != 1 {
		t.Fatal("failed deletion destroyed the invitation dependency")
	}
}

func TestR5PermissionEditorFormalBusyProfileConflict(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old := r5EditorRead(t, f)
	selected := r5EditorSelectedProfile(t, f, "busy selected ")
	preview, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	tx, e := f.pool.Begin(ctx)
	must(t, e)
	defer tx.Rollback(context.Background())
	_, e = tx.Exec(ctx, `SELECT id FROM permission_profiles WHERE id=$1 OR id=$2 FOR UPDATE`, p.ID, selected.ID)
	must(t, e)
	stable := r5EditorState(t, f)
	_, e = f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	r5EditorRequireKind(t, e, app.KindConflict)
	_, e = f.st.UpdatePermissionProfileCAS(ctx, f.a, old.Profile, old.Profile.Revision)
	r5EditorRequireKind(t, e, app.KindConflict)
	e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members)
	r5EditorRequireKind(t, e, app.KindConflict)
	_, e = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: old.Revision, ProfileID: &selected.ID, ProfileRevision: &selected.Revision})
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("busy-profile conflicts changed state")
	}
	must(t, tx.Rollback(ctx))
	r5EditorAssignment(t, f, old.Revision, selected, company.PermissionPatch{})
}

func TestR5PermissionEditorFormalPreviewAssignmentOverlap(t *testing.T) {
	f, p := r5EditorSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	selected, e := f.st.GetPermissionProfile(ctx, p.ID)
	must(t, e)
	other, e := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.other.ID)
	must(t, e)
	previous, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	// A test-only trigger PAUSES a real AssignPermissionEditor after it acquired
	// selected-profile SHARE/FK authority, before writing the new membership.
	// The trigger does not substitute a write or confirmation for a formal command.
	name := "editor_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	key := uuid.NewString()
	_, e = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='`+f.other.ID.String()+`'::uuid AND NEW.permission_profile_id='`+p.ID.String()+`'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+key+`',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
	must(t, e)
	gate, e := f.pool.Begin(ctx)
	must(t, e)
	defer gate.Rollback(context.Background())
	_, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
	must(t, e)
	done := make(chan error, 1)
	go func() {
		_, e := f.st.AssignPermissionEditor(ctx, f.a, f.other.ID, company.PermissionAssignmentCommand{ExpectedRevision: other.Revision, ProfileID: &selected.ID, ProfileRevision: &selected.Revision})
		done <- e
	}()
	r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "UPDATE users SET permission_profile_id")
	got, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	r5EditorRequireKind(t, e, app.KindConflict)
	if got != nil {
		t.Fatal("preview returned incomplete member set across in-flight assignment")
	}
	must(t, gate.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	stable := r5EditorState(t, f)
	e = f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, previous.ProfileRevision, previous.Members)
	r5EditorRequireKind(t, e, app.KindConflict)
	if r5EditorState(t, f) != stable {
		t.Fatal("pre-assignment preview deleted an unconfirmed new member")
	}
	refreshed, e := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
	must(t, e)
	if len(refreshed.Members) != 2 || len(refreshed.Changes) != 2 {
		t.Fatal("refreshed exact-set preview lost committed new membership")
	}
	must(t, f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, refreshed.ProfileRevision, refreshed.Members))
}
