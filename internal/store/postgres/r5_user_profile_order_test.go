package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/app/permissions"
	"tabmail/internal/models"
)

type r5UserProfileFixture struct {
	profile                    *r5InviteProfileFixture
	targetBefore, targetStable string
	targetRevision             int64
	audits, outbox             int
}

func r5UserProfileSeed(t *testing.T) *r5UserProfileFixture {
	t.Helper()
	p := r5InviteProfileSeed(t)
	f := p.f
	s := &r5UserProfileFixture{profile: p}
	// ActivateEmployee legitimately assigns its Company employee profile.
	// Preserve that real initial state, rather than demanding an unprofiled
	// user or clearing a profile merely to make the concurrency fixture fit.
	var initialProfile *uuid.UUID
	must(t, f.pool.QueryRow(context.Background(), `SELECT to_jsonb(u)::text,(to_jsonb(u)-'permission_profile_id'-'updated_at')::text,permission_profile_id,permission_revision FROM users u WHERE id=$1 AND tenant_id=$2 AND role='user' AND is_active`, f.employee.ID, f.tenant.ID).Scan(&s.targetBefore, &s.targetStable, &initialProfile, &s.targetRevision))
	if initialProfile != nil && *initialProfile == p.profile.ID {
		t.Fatal("target already has the selected admin profile; assignment would not exercise a new FK")
	}
	s.audits, s.outbox = r5UserProfileCounters(t, f, f.employee.ID)
	return s
}
func r5UserProfileAssign(s *r5UserProfileFixture, ctx context.Context) (*models.User, error) {
	p := s.profile
	return p.f.st.UpdateUserGuarded(ctx, p.f.a, p.f.tenant.ID, p.f.employee.ID, models.UserAdminPatch{SetPermissionProfile: true, PermissionProfileID: &p.profile.ID})
}
func r5UserProfileDelete(s *r5UserProfileFixture, ctx context.Context) error {
	p := s.profile
	return permissions.New(p.f.st).DeleteProfile(ctx, p.f.a, &p.f.tenant.ID, p.profile.ID)
}

// This is a normal actor-qualified member update and a normal non-system
// profile deletion. Users detach through SET NULL; no invitation references
// this profile, so invitation NO ACTION must not be manufactured into a veto.
func TestR5UserProfileNormalAssignmentAndDelete(t *testing.T) {
	s := r5UserProfileSeed(t)
	ctx := context.Background()
	value, err := r5UserProfileAssign(s, ctx)
	must(t, err)
	r5UserProfileEffects(t, s, value, true, false)
	var assigned string
	var assignedRevision int64
	must(t, s.profile.f.pool.QueryRow(ctx, `SELECT (to_jsonb(u)||jsonb_build_object('permission_profile_id',NULL))::text,permission_revision FROM users u WHERE id=$1`, s.profile.f.employee.ID).Scan(&assigned, &assignedRevision))
	must(t, r5UserProfileDelete(s, ctx))
	r5UserProfileEffects(t, s, value, true, true)
	var detached, expectedDetached string
	var detachedRevision int64
	must(t, s.profile.f.pool.QueryRow(ctx, `SELECT to_jsonb(u)::text,permission_revision,($2::jsonb||jsonb_build_object('permission_revision',u.permission_revision))::text FROM users u WHERE id=$1`, s.profile.f.employee.ID, assigned).Scan(&detached, &detachedRevision, &expectedDetached))
	if detachedRevision <= assignedRevision || detached != expectedDetached {
		t.Fatal("normal SET NULL failed to advance revision or changed fields beyond profile reference/revision")
	}
}

func TestR5UserProfileRequiredAuditFailureRollsBack(t *testing.T) {
	s := r5UserProfileSeed(t)
	f := s.profile.f
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_user_profile_audit_failure CHECK(NOT(action='user.update' AND resource_id='`+f.employee.ID.String()+`'::uuid)) NOT VALID`)
	must(t, err)
	_, err = r5UserProfileAssign(s, ctx)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "r5_user_profile_audit_failure" {
		t.Fatalf("required assignment audit failure not observed: SQLSTATE=%s err=%v", r5SQLState(err), err)
	}
	r5UserProfileEffects(t, s, nil, false, false)
	_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_user_profile_audit_failure`)
	must(t, err)
	value, err := r5UserProfileAssign(s, ctx)
	must(t, err)
	r5UserProfileEffects(t, s, value, true, false)
}

func TestR5UserProfileCompleteCommandsLockOrder(t *testing.T) {
	for _, order := range []string{"assignment-first", "profile-delete-first"} {
		t.Run(order, func(t *testing.T) {
			probe := r5UserProfileSeed(t)
			v, err := r5UserProfileAssign(probe, context.Background())
			must(t, err)
			must(t, r5UserProfileDelete(probe, context.Background()))
			r5UserProfileEffects(t, probe, v, true, true)
			s := r5UserProfileSeed(t)
			p := s.profile
			f := p.f
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			name := "r5_user_profile_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			key := "r5-user-profile:" + p.profile.ID.String()
			var sql string
			if order == "assignment-first" {
				sql = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='` + f.employee.ID.String() + `'::uuid AND NEW.permission_profile_id='` + p.profile.ID.String() + `'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER ` + name + ` BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION ` + name + `()`
			} else {
				sql = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='` + p.profile.ID.String() + `'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN OLD; END $$; CREATE TRIGGER ` + name + ` BEFORE DELETE ON permission_profiles FOR EACH ROW EXECUTE FUNCTION ` + name + `()`
			}
			_, err = f.pool.Exec(ctx, sql)
			must(t, err)
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
			must(t, err)
			type assigned struct {
				value *models.User
				err   error
			}
			assignedDone := make(chan assigned, 1)
			deleteDone := make(chan error, 1)
			assign := func() { value, e := r5UserProfileAssign(s, ctx); assignedDone <- assigned{value, e} }
			remove := func() { deleteDone <- r5UserProfileDelete(s, ctx) }
			if order == "assignment-first" {
				go assign()
				firstPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "UPDATE users SET display_name")
				go remove()
				secondPID := r5WaitBlockedBy(t, f, ctx, firstPID, "DELETE FROM permission_profiles")
				r5MaintenanceTrace(t, f, ctx, gate.Conn().PgConn().PID(), firstPID, secondPID)
			} else {
				go remove()
				firstPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "DELETE FROM permission_profiles")
				go assign()
				secondPID := r5UserProfileWaitOrDone(t, f, ctx, firstPID, assignedDone)
				r5MaintenanceTrace(t, f, ctx, gate.Conn().PgConn().PID(), firstPID, secondPID)
			}
			must(t, gate.Rollback(ctx))
			deleteErr := r5ConcurrentResult(t, ctx, deleteDone)
			var result assigned
			select {
			case result = <-assignedDone:
			case <-ctx.Done():
				t.Fatal("assignment terminal missing; timeout is not target evidence")
			}
			assignmentDead, deleteDead := r5SQLState(result.err) == "40P01", r5SQLState(deleteErr) == "40P01"
			if deleteErr != nil && !deleteDead {
				t.Fatalf("viable normal profile deletion failed unexpectedly: SQLSTATE=%s err=%v", r5SQLState(deleteErr), deleteErr)
			}
			if result.err != nil && !assignmentDead {
				e, ok := app.As(app.FromAuthz(result.err))
				if !ok || (e.Kind != app.KindConflict && e.Kind != app.KindForbidden) {
					t.Fatalf("unknown assignment refusal: SQLSTATE=%s err=%v", r5SQLState(result.err), result.err)
				}
			}
			if result.err != nil && deleteErr != nil {
				t.Fatal("neither complete command committed")
			}
			r5UserProfileEffects(t, s, result.value, result.err == nil, deleteErr == nil)
			if assignmentDead || deleteDead {
				t.Errorf("actual guarded-assignment/profile-delete 40P01; P/actor/target/audit/outbox victim rollback verified: assignment=%s delete=%s", r5SQLState(result.err), r5SQLState(deleteErr))
			}
		})
	}
}

func r5UserProfileWaitOrDone[T any](t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, done chan T) uint32 {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case result := <-done:
			done <- result
			t.Log("assignment terminal before controller release; exact refusal and zero-prefix checked below")
			return 0
		default:
		}
		var pid uint32
		err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("assignment response or real SQL blocker missing; not deadlock evidence")
		case <-tick.C:
		}
	}
}
func r5UserProfileCounters(t *testing.T, f *companyFixture, target uuid.UUID) (int, int) {
	t.Helper()
	var audits, outbox int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='user.update' AND resource_id=$1),(SELECT count(*) FROM outbox_events)`, target).Scan(&audits, &outbox))
	return audits, outbox
}
func r5UserProfileEffects(t *testing.T, s *r5UserProfileFixture, receipt *models.User, assigned, deleted bool) {
	t.Helper()
	p := s.profile
	f := p.f
	ctx := context.Background()
	// The shared normal helper checks exact P bytes and the actor's full U
	// bytes, including the legal users SET NULL result after P deletion.
	r5InviteProfileEffects(t, p, "", false, deleted)
	var target, stable, expectedStable string
	var revision int64
	var profile *uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(u)::text,(to_jsonb(u)-'permission_profile_id'-'updated_at')::text,permission_profile_id,permission_revision,($2::jsonb||jsonb_build_object('permission_revision',u.permission_revision))::text FROM users u WHERE id=$1`, f.employee.ID, s.targetStable).Scan(&target, &stable, &profile, &revision, &expectedStable))
	if !assigned {
		expectedStable = s.targetStable
	}
	if stable != expectedStable {
		t.Fatal("assignment/profile deletion rewrote target role/active/SV/identity/body fields")
	}
	if !assigned && target != s.targetBefore {
		t.Fatal("failed assignment retained a target profile/updated_at prefix")
	}
	if assigned {
		if revision <= s.targetRevision {
			t.Fatal("successful assignment/detach did not advance persistent permission revision")
		}
		if receipt == nil || receipt.ID != f.employee.ID || receipt.PermissionProfileID == nil || *receipt.PermissionProfileID != p.profile.ID {
			t.Fatal("successful assignment receipt does not identify its target and selected profile")
		}
		if deleted {
			if profile != nil {
				t.Fatal("normal deleted profile did not detach assigned target")
			}
		} else if profile == nil || *profile != p.profile.ID {
			t.Fatal("successful assignment did not retain current profile reference")
		}
	}
	// Refused assignment's full-byte check above includes preservation of
	// the target's original (possibly non-NULL) activation profile.
	audits, outbox := r5UserProfileCounters(t, f, f.employee.ID)
	want := s.audits
	if assigned {
		want++
	}
	if audits != want || outbox != s.outbox {
		t.Fatal("assignment audit/outbox ownership or full victim rollback is inconsistent")
	}
}
