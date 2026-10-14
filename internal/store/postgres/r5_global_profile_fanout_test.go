package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

// This acceptance suite must use an explicitly owned disposable PG instance.
// Commands use the real CAS/assignment ports; SQL observes rows or holds fences.
type r5GlobalFanoutFixture struct {
	f       *companyFixture
	p       *models.PermissionProfile
	super   authz.Actor
	tenants []uuid.UUID
	users   []*models.User
	unused  *models.User
}

func r5GlobalFanoutSeed(t *testing.T, selected, foreign bool) *r5GlobalFanoutFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("global profile fanout requires owned TABMAIL_TEST_DB_DSN; runtime evidence must not skip")
	}
	f := seedCompany(t)
	g := &r5GlobalFanoutFixture{f: f, super: r5EditorSuper(t, f), tenants: []uuid.UUID{f.tenant.ID}, users: []*models.User{f.employee, f.other}}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tenant := &models.Tenant{Name: "Fanout " + uuid.NewString(), PlanID: f.tenant.PlanID}
		must(t, f.st.CreateTenant(ctx, tenant))
		u := r5PermissionAuthorityUser(t, f, tenant.ID, models.RoleUser)
		if i < 2 {
			g.tenants = append(g.tenants, tenant.ID)
			g.users = append(g.users, u)
		} else {
			g.unused = u
		}
	}
	g.p = &models.PermissionProfile{Name: "Global " + uuid.NewString(), Description: "private-permission-description-canary", CanSend: true, DailySendQuota: 31, DailyReceiveQuota: 41, MaxMailboxes: 8, MaxDomains: 2, CanCreateAPIKeys: true}
	must(t, f.st.CreatePermissionProfile(ctx, g.p))
	var err error
	g.p, err = f.st.GetPermissionProfile(ctx, g.p.ID)
	must(t, err)
	for i, u := range g.users {
		if (i < 2 && !selected) || (i >= 2 && !foreign) {
			continue
		}
		r5GlobalFanoutAssign(t, g, u, g.p)
	}
	return g
}

func r5GlobalFanoutAssign(t *testing.T, g *r5GlobalFanoutFixture, u *models.User, p *models.PermissionProfile) {
	t.Helper()
	a := g.super
	a.TenantID = u.TenantID
	s, err := g.f.st.GetPermissionEditorSnapshot(context.Background(), a, u.ID)
	must(t, err)
	cmd := company.PermissionAssignmentCommand{ExpectedRevision: s.Revision}
	if p != nil {
		cmd.ProfileID = &p.ID
		cmd.ProfileRevision = &p.Revision
	}
	_, err = g.f.st.AssignPermissionEditor(context.Background(), a, u.ID, cmd)
	must(t, err)
}

func r5GlobalFanoutMutate(t *testing.T, g *r5GlobalFanoutFixture, op string) error {
	t.Helper()
	if op == "delete" {
		preview, err := g.f.st.GetPermissionProfileDeletionPreview(context.Background(), g.super, g.p.ID)
		if err != nil {
			return err
		}
		return g.f.st.DeletePermissionProfileCAS(context.Background(), g.super, g.p.ID, preview.ProfileRevision, preview.Members)
	}
	desired := *g.p
	desired.CanSend = false
	out, err := g.f.st.UpdatePermissionProfileCAS(context.Background(), g.super, &desired, g.p.Revision)
	if err != nil && out != nil {
		t.Fatal("failed update returned uncommitted profile")
	}
	return err
}

func r5GlobalFanoutAssert(t *testing.T, g *r5GlobalFanoutFixture, op string, want []uuid.UUID) {
	t.Helper()
	action := "permission.profile." + op
	rows, err := g.f.pool.Query(context.Background(), `SELECT payload FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'=$1 AND payload->'metadata'->>'resource_id'=$2`, action, g.p.ID.String())
	must(t, err)
	got := []string{}
	for rows.Next() {
		var raw []byte
		must(t, rows.Scan(&raw))
		var event map[string]json.RawMessage
		must(t, json.Unmarshal(raw, &event))
		// Exactly the existing Event serializer's envelope and its 3 public keys.
		if len(event) != 5 {
			t.Fatalf("unexpected event envelope: %s", raw)
		}
		for _, key := range []string{"type", "mailbox", "tenant_id", "occurred_at", "metadata"} {
			if _, ok := event[key]; !ok {
				t.Fatalf("missing event key %s: %s", key, raw)
			}
		}
		var typ, tenant, mailbox string
		must(t, json.Unmarshal(event["type"], &typ))
		must(t, json.Unmarshal(event["tenant_id"], &tenant))
		must(t, json.Unmarshal(event["mailbox"], &mailbox))
		var metadata map[string]any
		must(t, json.Unmarshal(event["metadata"], &metadata))
		if typ != "company.admin.changed" || mailbox != "" || len(metadata) != 3 || metadata["action"] != action || metadata["resource_type"] != "permission_profile" || metadata["resource_id"] != g.p.ID.String() {
			t.Fatalf("private/incorrect event: %s", raw)
		}
		got = append(got, tenant)
	}
	must(t, rows.Err())
	rows.Close()
	expected := []string{}
	for _, id := range want {
		expected = append(expected, id.String())
	}
	sort.Strings(got)
	sort.Strings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("fanout recipients=%v want=%v (no duplicate or unreferenced tenant)", got, expected)
	}
	var count int
	var tenant uuid.UUID
	var actor string
	var details []byte
	must(t, g.f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action=$1 AND resource_id=$2`, action, g.p.ID).Scan(&count))
	if count != 1 {
		t.Fatalf("main audit count=%d want=1", count)
	}
	must(t, g.f.pool.QueryRow(context.Background(), `SELECT tenant_id,actor,details FROM audit_log WHERE action=$1 AND resource_id=$2`, action, g.p.ID).Scan(&tenant, &actor, &details))
	if tenant != g.super.TenantID || actor != g.super.AuditLabel() {
		t.Fatalf("main audit moved/duplicated actor=%q tenant=%s", actor, tenant)
	}
	var audit map[string]json.RawMessage
	must(t, json.Unmarshal(details, &audit))
	var fields []string
	must(t, json.Unmarshal(audit["changed_fields"], &fields))
	field := "can_send"
	memberKey := "affected_members"
	revisionKey := "before_revision"
	if op == "delete" {
		field = "profile_presence"
		memberKey = "confirmed_members"
		revisionKey = "profile_revision"
	}
	if len(fields) == 0 || fields[0] != field || audit[revisionKey] == nil || audit[memberKey] == nil {
		t.Fatalf("original audit semantics lost: %s", details)
	}
}

func TestR5GlobalProfileFanoutRecipients(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, op := range []string{"update", "delete"} {
		for _, scope := range []string{"three-tenants-deduped", "selected-not-referenced", "no-references"} {
			t.Run(op+"/"+scope, func(t *testing.T) {
				selected, foreign := scope == "three-tenants-deduped", scope != "no-references"
				g := r5GlobalFanoutSeed(t, selected, foreign)
				want := []uuid.UUID{}
				if selected {
					want = append(want, g.tenants[0])
				}
				if foreign {
					want = append(want, g.tenants[1:]...)
				}
				before := map[uuid.UUID]company.PermissionRevision{}
				for _, u := range g.users {
					a := g.super
					a.TenantID = u.TenantID
					s, err := g.f.st.GetPermissionEditorSnapshot(context.Background(), a, u.ID)
					must(t, err)
					before[u.ID] = s.Revision
				}
				must(t, r5GlobalFanoutMutate(t, g, op))
				r5GlobalFanoutAssert(t, g, op, want)
				for i, u := range g.users {
					affected := (i < 2 && selected) || (i >= 2 && foreign)
					a := g.super
					a.TenantID = u.TenantID
					s, err := g.f.st.GetPermissionEditorSnapshot(context.Background(), a, u.ID)
					must(t, err)
					if affected && before[u.ID].Equal(s.Revision) {
						t.Fatal("affected member reload retained stale compound revision")
					}
					if !affected && !before[u.ID].Equal(s.Revision) {
						t.Fatal("unreferenced member revision changed")
					}
					if affected && op == "update" && (s.Profile == nil || s.Profile.CanSend || s.Effective.CanSend) {
						t.Fatal("recipient authoritative reload did not observe update")
					}
					if affected && op == "delete" && (s.Profile != nil || s.Revision.ProfileID != nil || s.Revision.UserRevision == before[u.ID].UserRevision) {
						t.Fatal("recipient reload did not observe deletion/SET NULL revision")
					}
				}
			})
		}
	}
}

func TestR5GlobalProfileFanoutRequiredRollback(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, op := range []string{"update", "delete"} {
		for _, fault := range []string{"audit", "outbox-0", "outbox-1", "outbox-2"} {
			t.Run(op+"/"+fault, func(t *testing.T) {
				g := r5GlobalFanoutSeed(t, true, true)
				action := "permission.profile." + op
				if fault == "audit" {
					_, err := g.f.pool.Exec(context.Background(), `ALTER TABLE audit_log ADD CONSTRAINT fanout_fault CHECK(action<>'`+action+`') NOT VALID`)
					must(t, err)
				} else {
					ordered := append([]uuid.UUID{}, g.tenants...)
					sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
					index := int(fault[len(fault)-1] - '0')
					_, err := g.f.pool.Exec(context.Background(), `ALTER TABLE outbox_events ADD CONSTRAINT fanout_fault CHECK(payload->'metadata'->>'action'<>'`+action+`' OR payload->>'tenant_id'<>'`+ordered[index].String()+`') NOT VALID`)
					must(t, err)
				}
				stable := r5EditorState(t, g.f)
				if err := r5GlobalFanoutMutate(t, g, op); err == nil {
					t.Fatal("required fault committed")
				}
				if r5EditorState(t, g.f) != stable {
					t.Fatal("required fault did not roll back all profile/user revisions, main audit and every fanout event")
				}
			})
		}
	}
}

func TestR5GlobalProfileFanoutTenantLocalAndNoop(t *testing.T) {
	for _, scope := range []string{"global", "tenant-local"} {
		t.Run(scope, func(t *testing.T) {
			g := r5GlobalFanoutSeed(t, true, true)
			if scope == "tenant-local" {
				g.p = r5EditorSelectedProfile(t, g.f, "Local fanout ")
				r5GlobalFanoutAssign(t, g, g.f.employee, g.p)
			}
			stable := r5PermissionAuthorityState(t, g.f)
			out, err := g.f.st.UpdatePermissionProfileCAS(context.Background(), g.super, g.p, g.p.Revision)
			must(t, err)
			if out == nil || out.Revision != g.p.Revision || r5PermissionAuthorityState(t, g.f) != stable {
				t.Fatal("no-op consumed revision or created audit/event")
			}
			desired := *g.p
			desired.CanSend = !g.p.CanSend
			_, err = g.f.st.UpdatePermissionProfileCAS(context.Background(), g.super, &desired, g.p.Revision)
			must(t, err)
			want := g.tenants
			if scope == "tenant-local" {
				want = g.tenants[:1]
			}
			// Local profile starts CanSend=false, so this helper's audit check is still the same field.
			r5GlobalFanoutAssert(t, g, "update", want)
		})
	}
}

func TestR5GlobalProfileFanoutAuthority(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, kind := range []string{"admin", "forged-super", "foreign-selected-admin", "api-key", "inactive", "stale-session", "no-selected-tenant"} {
		t.Run(kind, func(t *testing.T) {
			g := r5GlobalFanoutSeed(t, true, true)
			a := g.super
			switch kind {
			case "admin":
				a = g.f.a
			case "forged-super":
				a = g.f.a
				a.Role = models.RoleSuperAdmin
				a.IsSuperAdmin = true
			case "foreign-selected-admin":
				a = g.f.a
				a.TenantID = g.tenants[1]
			case "api-key":
				a.Type = authz.PrincipalAPIKey
			case "inactive":
				u, err := g.f.st.GetUser(context.Background(), a.ID)
				must(t, err)
				u.IsActive = false
				must(t, g.f.st.UpdateUser(context.Background(), u))
			case "stale-session":
				v := int64(999999)
				a.SessionVersion = &v
			case "no-selected-tenant":
				a.TenantID = uuid.Nil
			}
			stable := r5PermissionAuthorityState(t, g.f)
			desired := *g.p
			desired.CanSend = false
			out, err := g.f.st.UpdatePermissionProfileCAS(context.Background(), a, &desired, g.p.Revision)
			if err == nil || out != nil {
				t.Fatal("invalid current selected authority updated global profile")
			}
			if r5PermissionAuthorityState(t, g.f) != stable {
				t.Fatal("authority denial changed revision/audit/outbox")
			}
			// No selected tenant retains the existing not-found tenant-lock result;
			// other failures must come from identity/scope, not a malformed CAS.
			if kind != "no-selected-tenant" {
				if kind == "admin" || kind == "forged-super" {
					r5EditorRequireKind(t, err, app.KindNotFound)
				} else {
					r5EditorRequireKind(t, err, app.KindForbidden)
				}
			}
		})
	}
}

func TestR5GlobalProfileFanoutCurrentMembership(t *testing.T) {
	t.Run("committed-reassignment-removes-recipient", func(t *testing.T) {
		g := r5GlobalFanoutSeed(t, true, true)
		r5GlobalFanoutAssign(t, g, g.users[2], nil)
		must(t, r5GlobalFanoutMutate(t, g, "update"))
		r5GlobalFanoutAssert(t, g, "update", []uuid.UUID{g.tenants[0], g.tenants[2]})
	})
	for _, op := range []string{"update", "delete"} {
		t.Run(op+"/busy-existing-member", func(t *testing.T) {
			g := r5GlobalFanoutSeed(t, true, true)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			preview, err := g.f.st.GetPermissionProfileDeletionPreview(ctx, g.super, g.p.ID)
			must(t, err)
			tx, err := g.f.pool.Begin(ctx)
			must(t, err)
			defer tx.Rollback(context.Background())
			_, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, g.users[2].ID)
			must(t, err)
			stable := r5PermissionAuthorityState(t, g.f)
			desired := *g.p
			desired.CanSend = false
			if op == "update" {
				_, err = g.f.st.UpdatePermissionProfileCAS(ctx, g.super, &desired, g.p.Revision)
			} else {
				err = g.f.st.DeletePermissionProfileCAS(ctx, g.super, g.p.ID, g.p.Revision, preview.Members)
			}
			r5EditorRequireKind(t, err, app.KindConflict)
			if r5PermissionAuthorityState(t, g.f) != stable {
				t.Fatal("member fence conflict changed durable state")
			}
		})
	}
}

// The mutation is paused inside its first real outbox INSERT, after profile
// and member fences. A never-referenced tenant tries the formal assignment
// port and must get NOWAIT conflict rather than becoming an omitted phantom.
func TestR5GlobalProfileFanoutNoAssignmentPhantom(t *testing.T) {
	g := r5GlobalFanoutSeed(t, true, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	barrier, err := g.f.pool.Begin(ctx)
	must(t, err)
	defer barrier.Rollback(context.Background())
	var blocker uint32
	must(t, barrier.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	const key int64 = 519530211
	_, err = barrier.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key)
	must(t, err)
	_, err = g.f.pool.Exec(ctx, `CREATE FUNCTION fanout_pause() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.payload->'metadata'->>'action'='permission.profile.update' THEN PERFORM pg_advisory_xact_lock(519530211); END IF; RETURN NEW; END $$; CREATE TRIGGER fanout_pause BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION fanout_pause()`)
	must(t, err)
	done := make(chan error, 1)
	go func() {
		desired := *g.p
		desired.CanSend = false
		_, e := g.f.st.UpdatePermissionProfileCAS(ctx, g.super, &desired, g.p.Revision)
		done <- e
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var waiter uint32
	for waiter == 0 {
		must(t, g.f.pool.QueryRow(ctx, `SELECT COALESCE((SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE 'INSERT INTO outbox_events%' LIMIT 1),0)`, int32(blocker)).Scan(&waiter))
		if waiter == 0 {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case e := <-done:
				t.Fatalf("mutation did not reach event barrier: %v", e)
			case <-ticker.C:
			}
		}
	}
	must(t, testpg.R5WaitBlockedBy(ctx, g.f.pool, waiter, blocker))
	a := g.super
	a.TenantID = g.unused.TenantID
	before, err := g.f.st.GetPermissionEditorSnapshot(ctx, a, g.unused.ID)
	must(t, err)
	_, err = g.f.st.AssignPermissionEditor(ctx, a, g.unused.ID, company.PermissionAssignmentCommand{ExpectedRevision: before.Revision, ProfileID: &g.p.ID, ProfileRevision: &g.p.Revision})
	r5EditorRequireKind(t, err, app.KindConflict)
	must(t, barrier.Rollback(ctx))
	must(t, <-done)
	r5GlobalFanoutAssert(t, g, "update", g.tenants)
	after, err := g.f.st.GetPermissionEditorSnapshot(ctx, a, g.unused.ID)
	must(t, err)
	if !before.Revision.Equal(after.Revision) {
		t.Fatal("phantom assignment changed unused member revision")
	}
	_, err = g.f.pool.Exec(ctx, `DROP TRIGGER fanout_pause ON outbox_events; DROP FUNCTION fanout_pause()`)
	must(t, err)
}
