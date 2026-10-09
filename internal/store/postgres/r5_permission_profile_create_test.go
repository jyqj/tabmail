package postgres_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type r5CreateProfileFixture struct {
	*companyFixture
	foreignTenant *models.Tenant
	foreignZone   *models.DomainZone
}

func r5CreateProfileSeed(t *testing.T) *r5CreateProfileFixture {
	t.Helper()
	f := &r5CreateProfileFixture{companyFixture: r5PermissionAuthoritySeed(t)}
	f.foreignTenant = &models.Tenant{Name: "Create foreign " + uuid.NewString(), PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(context.Background(), f.foreignTenant))
	f.foreignZone = &models.DomainZone{TenantID: f.foreignTenant.ID, Domain: uuid.NewString() + ".create.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(context.Background(), f.foreignZone))
	return f
}

func r5CreateProfileInput(t *testing.T) *models.PermissionProfile {
	t.Helper()
	return &models.PermissionProfile{Name: "Guarded create " + uuid.NewString(), CanSend: true, DailySendQuota: 19, DailyReceiveQuota: 21, MaxMailboxes: 3, MaxDomains: 2, CanCreateAPIKeys: true}
}
func r5CreateProfileCounts(t *testing.T, f *r5CreateProfileFixture) (int, int) {
	t.Helper()
	var audit, event int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='permission.profile.create'),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='permission.profile.create')`).Scan(&audit, &event))
	return audit, event
}
func TestR5PermissionProfileCreateFreshScopeAndRevision(t *testing.T) {
	for _, mode := range []string{"admin-stale-super-hint", "fresh-super-stale-admin-hint", "global-no-selected", "global-selected"} {
		t.Run(mode, func(t *testing.T) {
			f := r5CreateProfileSeed(t)
			ctx := context.Background()
			a := f.a
			p := r5CreateProfileInput(t)
			selected := &f.tenant.ID
			target := f.tenant.ID
			global := false
			if mode == "admin-stale-super-hint" {
				a.IsSuperAdmin = true
				p.TenantID = &f.foreignTenant.ID
			} else {
				_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
				must(t, err)
				a.IsSuperAdmin = false
				a.IsAdmin = false
				a.Role = models.RoleUser
				if mode == "fresh-super-stale-admin-hint" {
					target = f.foreignTenant.ID
					p.TenantID = &target
				} else {
					global = true
					if mode == "global-no-selected" {
						selected = nil
					}
				}
			}
			beforeA, beforeE := r5CreateProfileCounts(t, f)
			out, err := f.st.CreatePermissionProfileGuarded(ctx, a, selected, p)
			must(t, err)
			if out == nil || out.IsSystem || (global && out.TenantID != nil) || (!global && (out.TenantID == nil || *out.TenantID != target)) {
				t.Fatalf("wrong fresh-authority scope: %+v", out)
			}
			n, err := strconv.ParseInt(out.Revision, 10, 64)
			must(t, err)
			if n <= 0 {
				t.Fatal("nonpositive durable revision")
			}
			stored, err := f.st.GetPermissionProfile(ctx, out.ID)
			must(t, err)
			if !reflect.DeepEqual(stored, out) {
				t.Fatal("returned profile differs from actual persisted profile")
			}
			afterA, afterE := r5CreateProfileCounts(t, f)
			wantE := 1
			if global {
				wantE = 0
			}
			if afterA-beforeA != 1 || afterE-beforeE != wantE {
				t.Fatalf("audit/event delta=%d/%d", afterA-beforeA, afterE-beforeE)
			}
			if !global {
				var raw []byte
				must(t, f.pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE payload->'metadata'->>'action'='permission.profile.create' ORDER BY created_at DESC LIMIT 1`).Scan(&raw))
				var event struct {
					TenantID string         `json:"tenant_id"`
					Metadata map[string]any `json:"metadata"`
				}
				must(t, json.Unmarshal(raw, &event))
				if event.TenantID != target.String() || len(event.Metadata) != 3 || event.Metadata["action"] != "permission.profile.create" || event.Metadata["resource_type"] != "permission_profile" || event.Metadata["resource_id"] != out.ID.String() {
					t.Fatalf("creation event scope/envelope=%s", raw)
				}
			}
			stable := r5EditorState(t, f.companyFixture)
			repeated, err := f.st.CreatePermissionProfileGuarded(ctx, a, selected, p)
			r5EditorRequireKind(t, err, app.KindConflict)
			if repeated != nil || stable != r5EditorState(t, f.companyFixture) {
				t.Fatal("duplicate create released payload or committed extra side effects")
			}
		})
	}
}
func TestR5PermissionProfileCreateAuthorityAndZoneFailures(t *testing.T) {
	for _, mode := range []string{"employee", "owned-admin-key", "freeze", "session-version", "selected-foreign", "missing-selected-admin", "foreign-zone", "duplicate-zone", "global-zones", "system"} {
		t.Run(mode, func(t *testing.T) {
			f := r5CreateProfileSeed(t)
			ctx := context.Background()
			a := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
			p := r5CreateProfileInput(t)
			selected := &f.tenant.ID
			switch mode {
			case "employee":
				a.ID = f.employee.ID
				a.SessionVersion = nil
				a.IsSuperAdmin = true
			case "owned-admin-key":
				a.Type = authz.PrincipalAPIKey
				a.OwnerUserID = &f.admin.ID
			case "freeze":
				_, err := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
				must(t, err)
			case "session-version":
				_, err := f.pool.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.admin.ID)
				must(t, err)
			case "selected-foreign":
				selected = &f.foreignTenant.ID
				a.IsSuperAdmin = true
			case "missing-selected-admin":
				selected = nil
			case "foreign-zone":
				p.AllowedZoneIDs = []uuid.UUID{f.foreignZone.ID}
			case "duplicate-zone":
				p.AllowedZoneIDs = []uuid.UUID{f.zone.ID, f.zone.ID}
			case "global-zones":
				_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
				must(t, err)
				p.AllowedZoneIDs = []uuid.UUID{f.zone.ID}
			case "system":
				p.IsSystem = true
			}
			before := r5PermissionAuthorityState(t, f.companyFixture)
			out, err := f.st.CreatePermissionProfileGuarded(ctx, a, selected, p)
			if err == nil || out != nil {
				t.Fatal("rejected creation succeeded or exposed a profile")
			}
			if before != r5PermissionAuthorityState(t, f.companyFixture) {
				t.Fatal("authority/zone denial left durable effects or consumed a revision")
			}
		})
	}
}
func TestR5PermissionProfileCreateRequiredAuditOutboxRollback(t *testing.T) {
	for _, failure := range []string{"audit", "outbox"} {
		t.Run(failure, func(t *testing.T) {
			f := r5CreateProfileSeed(t)
			ctx := context.Background()
			p := r5CreateProfileInput(t)
			sql := `ALTER TABLE audit_log ADD CONSTRAINT create_failure CHECK(action<>'permission.profile.create') NOT VALID`
			if failure == "outbox" {
				sql = `ALTER TABLE outbox_events ADD CONSTRAINT create_failure CHECK(event_type<>'company.admin.changed' OR payload->'metadata'->>'action'<>'permission.profile.create') NOT VALID`
			}
			_, err := f.pool.Exec(ctx, sql)
			must(t, err)
			before := r5EditorState(t, f.companyFixture)
			out, err := f.st.CreatePermissionProfileGuarded(ctx, f.a, &f.tenant.ID, p)
			if err == nil || out != nil || r5EditorState(t, f.companyFixture) != before {
				t.Fatal("required audit/outbox failure returned payload or changed transactional state")
			}
		})
	}
}
func TestR5PermissionProfileCreateLockWaitRechecksAuthority(t *testing.T) {
	for _, gateKind := range []string{"tenant", "actor"} {
		for _, change := range []string{"demote", "freeze", "session-version"} {
			t.Run(gateKind+"/"+change, func(t *testing.T) {
				f := r5CreateProfileSeed(t)
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				a := r5PermissionAuthorityActor(f.admin, f.tenant.ID)
				p := r5CreateProfileInput(t)
				gate, err := f.pool.Begin(ctx)
				must(t, err)
				defer gate.Rollback(context.Background())
				sql := `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`
				id := f.tenant.ID
				if gateKind == "actor" {
					sql = `SELECT id FROM users WHERE id=$1 FOR UPDATE`
					id = f.admin.ID
				}
				_, err = gate.Exec(ctx, sql, id)
				must(t, err)
				done := make(chan error, 1)
				go func() {
					out, err := f.st.CreatePermissionProfileGuarded(ctx, a, &f.tenant.ID, p)
					if out != nil {
						done <- app.BadRequest("unexpected created payload")
						return
					}
					done <- err
				}()
				r5PermissionAuthorityWait(t, f.companyFixture, ctx, gate.Conn().PgConn().PID(), done)
				mutation := `UPDATE users SET role='user' WHERE id=$1`
				if change == "freeze" {
					mutation = `UPDATE users SET is_active=false WHERE id=$1`
				}
				if change == "session-version" {
					mutation = `UPDATE users SET session_version=session_version+1 WHERE id=$1`
				}
				if gateKind == "actor" {
					_, err = gate.Exec(ctx, mutation, f.admin.ID)
				} else {
					_, err = f.pool.Exec(ctx, mutation, f.admin.ID)
				}
				must(t, err)
				must(t, gate.Commit(ctx))
				select {
				case err := <-done:
					r5EditorRequireKind(t, err, app.KindForbidden)
				case <-ctx.Done():
					t.Fatal("creation authority wait did not terminate")
				}
				var count int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM permission_profiles WHERE name=$1`, p.Name).Scan(&count))
				audit, event := r5CreateProfileCounts(t, f)
				if count != 0 || audit != 0 || event != 0 {
					t.Fatal("post-wait authority denial created data/audit/events")
				}
			})
		}
	}
}
func TestR5PermissionProfileCreateZoneBusyAndShippingService(t *testing.T) {
	f := r5CreateProfileSeed(t)
	ctx := context.Background()
	gate, err := f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(ctx)
	_, err = gate.Exec(ctx, `SELECT id FROM domain_zones WHERE id=$1 FOR UPDATE`, f.zone.ID)
	must(t, err)
	p := r5CreateProfileInput(t)
	p.AllowedZoneIDs = []uuid.UUID{f.zone.ID}
	before := r5PermissionAuthorityState(t, f.companyFixture)
	out, err := f.st.CreatePermissionProfileGuarded(ctx, f.a, &f.tenant.ID, p)
	r5EditorRequireKind(t, err, app.KindConflict)
	if out != nil || before != r5PermissionAuthorityState(t, f.companyFixture) {
		t.Fatal("busy zone created side effects")
	}
	must(t, gate.Commit(ctx))
	out, err = permissions.New(f.st).CreateProfile(ctx, f.a, &f.tenant.ID, permissions.CreateInput{Name: p.Name, AllowedZoneIDs: p.AllowedZoneIDs, CanSend: true})
	must(t, err)
	if out == nil || out.Revision == "" || out.TenantID == nil || *out.TenantID != f.tenant.ID {
		t.Fatal("shipping service did not consume guarded creation")
	}
}

func TestR5PermissionProfileCreateGlobalAnchorIsPersistent(t *testing.T) {
	f := r5CreateProfileSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
	must(t, err)
	// Even a forged selected-home hint does not choose the no-selected audit anchor.
	actor := f.a
	actor.TenantID = f.foreignTenant.ID
	actor.IsSuperAdmin = false
	out, err := f.st.CreatePermissionProfileGuarded(ctx, actor, nil, r5CreateProfileInput(t))
	must(t, err)
	var anchor uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT tenant_id FROM audit_log WHERE action='permission.profile.create' AND resource_id=$1`, out.ID).Scan(&anchor))
	if anchor != f.tenant.ID || out.TenantID != nil {
		t.Fatal("no-selected global create trusted stale actor tenant hint")
	}
	gate, err := f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(context.Background())
	_, err = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, err)
	p := r5CreateProfileInput(t)
	done := make(chan error, 1)
	go func() {
		got, err := f.st.CreatePermissionProfileGuarded(ctx, actor, nil, p)
		if got != nil {
			done <- app.BadRequest("unexpected global payload")
			return
		}
		done <- err
	}()
	r5PermissionAuthorityWait(t, f.companyFixture, ctx, gate.Conn().PgConn().PID(), done)
	_, err = f.pool.Exec(ctx, `UPDATE users SET tenant_id=$2 WHERE id=$1`, f.admin.ID, f.foreignTenant.ID)
	must(t, err)
	stable := r5PermissionAuthorityState(t, f.companyFixture)
	must(t, gate.Commit(ctx))
	select {
	case err := <-done:
		r5EditorRequireKind(t, err, app.KindConflict)
	case <-ctx.Done():
		t.Fatal("global home-anchor recheck stalled")
	}
	if stable != r5PermissionAuthorityState(t, f.companyFixture) {
		t.Fatal("changed persistent anchor caused creation/audit/events")
	}
}

func TestR5PermissionProfileCreateSuppressedInsertFailsClosed(t *testing.T) {
	f := r5CreateProfileSeed(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION create_suppress_row() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NULL; END$$; CREATE TRIGGER create_suppress_row BEFORE INSERT ON permission_profiles FOR EACH ROW EXECUTE FUNCTION create_suppress_row()`)
	must(t, err)
	before := r5EditorState(t, f.companyFixture)
	out, err := f.st.CreatePermissionProfileGuarded(ctx, f.a, &f.tenant.ID, r5CreateProfileInput(t))
	r5EditorRequireKind(t, err, app.KindInternal)
	if out != nil || before != r5EditorState(t, f.companyFixture) {
		t.Fatal("suppressed INSERT released a profile or audit/event effects")
	}
}
func TestR5PermissionProfileCreateInvalidValuesAndMissingHome(t *testing.T) {
	for _, mode := range []string{"nil-request", "blank-name", "negative-send", "negative-receive", "negative-mailboxes", "negative-domains", "overflow-quota", "zero-target-super", "zero-zone", "zero-selected", "missing-home"} {
		t.Run(mode, func(t *testing.T) {
			f := r5CreateProfileSeed(t)
			ctx := context.Background()
			a := f.a
			p := r5CreateProfileInput(t)
			selected := &f.tenant.ID
			switch mode {
			case "nil-request":
				p = nil
			case "blank-name":
				p.Name = "  "
			case "negative-send":
				p.DailySendQuota = -1
			case "negative-receive":
				p.DailyReceiveQuota = -1
			case "negative-mailboxes":
				p.MaxMailboxes = -1
			case "negative-domains":
				p.MaxDomains = -1
			case "overflow-quota":
				p.DailySendQuota = int(2147483648)
			case "zero-target-super":
				_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
				must(t, err)
				zero := uuid.Nil
				p.TenantID = &zero
			case "zero-zone":
				p.AllowedZoneIDs = []uuid.UUID{uuid.Nil}
			case "zero-selected":
				zero := uuid.Nil
				selected = &zero
			case "missing-home":
				a.ID = uuid.New()
				a.IsSuperAdmin = true
				selected = nil
			}
			before := r5PermissionAuthorityState(t, f.companyFixture)
			out, err := f.st.CreatePermissionProfileGuarded(ctx, a, selected, p)
			if err == nil || out != nil || before != r5PermissionAuthorityState(t, f.companyFixture) {
				t.Fatal("invalid creation consumed allocator/persisted data or returned payload")
			}
		})
	}
}
