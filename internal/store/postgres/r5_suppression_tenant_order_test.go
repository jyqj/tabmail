package postgres_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

// This adapter observes terminal errors from real PgStore methods, without
// replacing SQL, transaction ownership, authorization, or handler responses.
type r5SuppressionObservedStore struct {
	*postgres.PgStore
	suppression chan error
	tenant      chan error
}

func (s *r5SuppressionObservedStore) DeleteSuppressionAudited(ctx context.Context, tenant, id uuid.UUID, audit models.AuditEntry) error {
	err := s.PgStore.DeleteSuppressionAudited(ctx, tenant, id, audit)
	s.suppression <- err
	return err
}
func (s *r5SuppressionObservedStore) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	err := s.PgStore.DeleteTenant(ctx, id)
	s.tenant <- err
	return err
}

type r5SuppressionFixture struct {
	f                                 *companyFixture
	observed                          *r5SuppressionObservedStore
	router                            http.Handler
	entry                             *models.SuppressionEntry
	adminToken, userToken, superToken string
}

// The target contains only ordinary users and a suppression entry: no
// mailbox/message/template assets that would make tenant deletion impossible.
// A separate super-admin identity survives deletion of the selected target.
func r5SuppressionOrderFixture(t *testing.T) *r5SuppressionFixture {
	t.Helper()
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	f := &companyFixture{st: st, pool: pool}
	f.tenant = &models.Tenant{Name: "Suppression target", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, f.tenant))
	global := &models.Tenant{Name: "Suppression platform", PlanID: f.tenant.PlanID}
	must(t, st.CreateTenant(ctx, global))
	makeUser := func(tenant uuid.UUID, role models.UserRole) *models.User {
		u := &models.User{TenantID: tenant, Role: role, Email: uuid.NewString() + "@test.invalid", PasswordHash: "test-only-unused-password", IsActive: true}
		must(t, st.CreateUser(ctx, u))
		return u
	}
	f.admin, f.employee = makeUser(f.tenant.ID, models.RoleAdmin), makeUser(f.tenant.ID, models.RoleUser)
	super := makeUser(global.ID, models.RoleSuperAdmin)
	s := &r5SuppressionFixture{f: f, observed: &r5SuppressionObservedStore{PgStore: st, suppression: make(chan error, 1), tenant: make(chan error, 1)}}
	s.entry = &models.SuppressionEntry{TenantID: f.tenant.ID, Address: "target@test.invalid", Reason: "hard_bounce", CreatedAt: time.Now()}
	must(t, st.AddSuppression(ctx, s.entry))
	secret := "r5-suppression-test-jwt"
	var err error
	s.adminToken, err = authn.IssueAccessToken(secret, f.admin)
	must(t, err)
	s.userToken, err = authn.IssueAccessToken(secret, f.employee)
	must(t, err)
	s.superToken, err = authn.IssueAccessToken(secret, super)
	must(t, err)
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	objects := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay"}, st, st, zerolog.Nop())
	svc.SetObjectStore(objects)
	// Official router supplies Auth/current-user, PermissionLoader, scopes,
	// suppression administrator and RequireSuperAdmin checks. No worker runs.
	s.router = api.NewRouter(api.RouterConfig{Store: s.observed, ObjectStore: objects, JWTSecret: secret, MailboxTokenSecret: "r5-unused-mailbox-secret", CompanyOnly: true, CompanyRepository: st, OutboundService: svc, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), Logger: zerolog.Nop()})
	return s
}

func (s *r5SuppressionFixture) request(ctx context.Context, tenantDelete bool, token string) int {
	path, body := "/api/v1/suppression/"+s.entry.ID.String(), `{"reason":"documented suppression removal"}`
	if tenantDelete {
		path = "/api/v1/admin/tenants/" + s.f.tenant.ID.String()
		body = ""
	}
	req := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, req)
	return recorder.Code
}

func TestR5SuppressionTenantNormalControls(t *testing.T) {
	for _, kind := range []string{"tenant-cascade", "suppression-delete", "non-admin-denied", "audit-failure"} {
		t.Run(kind, func(t *testing.T) {
			s := r5SuppressionOrderFixture(t)
			ctx := context.Background()
			before := r5SuppressionSnapshot(t, s)
			if kind == "tenant-cascade" {
				status := s.request(ctx, true, s.superToken)
				err := r5SuppressionTerminal(t, s, true)
				if err != nil {
					description := r5TenantDeleteConstraint(t, s.f, err)
					if r5SuppressionSnapshot(t, s) != before {
						t.Fatal("normal cascade refusal retained a prefix")
					}
					t.Logf("normal S-only cascade refused by actual catalog: %s", description)
					return
				}
				if status != http.StatusNoContent {
					t.Fatalf("normal super-admin tenant route status=%d", status)
				}
				r5SuppressionState(t, s, 0, 0, 0, 1)
				return
			}
			if kind == "non-admin-denied" {
				if status := s.request(ctx, false, s.userToken); status != http.StatusForbidden {
					t.Fatalf("normal user suppression route status=%d want403", status)
				}
				if len(s.observed.suppression) != 0 || r5SuppressionSnapshot(t, s) != before {
					t.Fatal("actor denial reached writer or changed state")
				}
				return
			}
			if kind == "audit-failure" {
				_, err := s.f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_suppression_audit_failure CHECK(action<>'suppression.delete') NOT VALID`)
				must(t, err)
			}
			status := s.request(ctx, false, s.adminToken)
			err := r5SuppressionTerminal(t, s, false)
			if kind == "audit-failure" {
				var pg *pgconn.PgError
				if status != http.StatusInternalServerError || !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatalf("required audit refusal status=%d SQLSTATE=%s err=%v", status, r5SQLState(err), err)
				}
				if r5SuppressionSnapshot(t, s) != before {
					t.Fatal("required audit failure retained suppression delete or effects")
				}
				return
			}
			must(t, err)
			if status != http.StatusNoContent {
				t.Fatalf("normal admin suppression route status=%d", status)
			}
			r5SuppressionState(t, s, 1, 0, 1, 0)
		})
	}
}

func TestR5SuppressionTenantCompleteCommandsOrder(t *testing.T) {
	for _, order := range []string{"suppression-first", "tenant-first"} {
		t.Run(order, func(t *testing.T) {
			probe := r5SuppressionOrderFixture(t)
			probeBefore := r5SuppressionSnapshot(t, probe)
			status := probe.request(context.Background(), true, probe.superToken)
			err := r5SuppressionTerminal(t, probe, true)
			if err != nil {
				description := r5TenantDeleteConstraint(t, probe.f, err)
				if r5SuppressionSnapshot(t, probe) != probeBefore {
					t.Fatal("viability refusal changed data")
				}
				t.Skipf("S-only normal tenant route not viable; no invented concurrency: %s", description)
			}
			if status != http.StatusNoContent {
				t.Fatalf("normal viability status=%d", status)
			}
			s := r5SuppressionOrderFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			metadata := r5SuppressionMetadata(t, s)
			name := "r5_supp_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			key := "r5-supp-gate:" + s.entry.ID.String()
			var gateSQL string
			if order == "suppression-first" {
				gateSQL = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_id='` + s.entry.ID.String() + `'::uuid AND NEW.action='suppression.delete' THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER ` + name + ` BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION ` + name + `() `
			} else {
				gateSQL = `CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='` + s.f.tenant.ID.String() + `'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('` + key + `',0)); END IF; RETURN OLD; END $$; CREATE TRIGGER ` + name + ` BEFORE DELETE ON tenants FOR EACH ROW EXECUTE FUNCTION ` + name + `() `
			}
			_, err = s.f.pool.Exec(ctx, gateSQL)
			must(t, err)
			hold, err := s.f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
			must(t, err)
			type terminal struct {
				status int
				err    error
			}
			suppressionDone, tenantDone := make(chan terminal, 1), make(chan terminal, 1)
			removeSuppression := func() {
				status := s.request(ctx, false, s.adminToken)
				select {
				case e := <-s.observed.suppression:
					suppressionDone <- terminal{status, e}
				default:
					suppressionDone <- terminal{status, errors.New("suppression handler did not reach real writer")}
				}
			}
			removeTenant := func() {
				status := s.request(ctx, true, s.superToken)
				select {
				case e := <-s.observed.tenant:
					tenantDone <- terminal{status, e}
				default:
					tenantDone <- terminal{status, errors.New("tenant handler did not reach real writer")}
				}
			}
			firstQuery := "audit_log"
			if order == "suppression-first" {
				go removeSuppression()
			} else {
				firstQuery = "DELETE FROM tenants"
				go removeTenant()
			}
			firstPID := r5WaitBlockedBy(t, s.f, ctx, hold.Conn().PgConn().PID(), firstQuery)
			if order == "suppression-first" {
				go removeTenant()
			} else {
				go removeSuppression()
			}
			// An updated command may wait at its parent instead of after S.
			// Observe the actual blocker/query, never assume a downstream edge.
			secondPID := r5WaitBlockedBy(t, s.f, ctx, firstPID, "")
			r5MaintenanceTrace(t, s.f, ctx, hold.Conn().PgConn().PID(), firstPID, secondPID)
			must(t, hold.Rollback(ctx))
			var suppression, tenant terminal
			select {
			case suppression = <-suppressionDone:
			case <-ctx.Done():
				t.Fatal("suppression terminal missing; timeout is not deadlock evidence")
			}
			select {
			case tenant = <-tenantDone:
			case <-ctx.Done():
				t.Fatal("tenant terminal missing; timeout is not deadlock evidence")
			}
			suppDead, tenantDead := r5SQLState(suppression.err) == "40P01", r5SQLState(tenant.err) == "40P01"
			// If the actual tenant DELETE wins, a command waiting at the parent
			// can no longer commit its required audit. Accept only that precise
			// typed parent-absent refusal and the established HTTP NotFound wire.
			// Unknown DB errors and audit faults must remain rejected separately.
			parentAbsent := false
			if order == "tenant-first" && tenant.err == nil && tenant.status == http.StatusNoContent && suppression.status == http.StatusNotFound {
				if e, ok := app.As(suppression.err); ok && e.Kind == app.KindNotFound {
					parentAbsent = true
				}
			}
			if suppression.err != nil && !suppDead && !parentAbsent {
				t.Fatalf("unexpected suppression status=%d SQLSTATE=%s err=%v", suppression.status, r5SQLState(suppression.err), suppression.err)
			}
			if tenant.err != nil && !tenantDead {
				t.Fatalf("unexpected tenant status=%d SQLSTATE=%s err=%v", tenant.status, r5SQLState(tenant.err), tenant.err)
			}
			if suppression.err == nil && suppression.status != http.StatusNoContent {
				t.Fatal("successful suppression writer lost HTTP effect receipt")
			}
			if tenant.err == nil && tenant.status != http.StatusNoContent {
				t.Fatal("successful tenant writer lost HTTP effect receipt")
			}
			if tenant.err == nil {
				suppEffects := 0
				if suppression.err == nil {
					suppEffects = 1
				}
				r5SuppressionState(t, s, 0, 0, suppEffects, 1)
				if parentAbsent {
					t.Log("verified tenant-first parent absent: typed NotFound, HTTP 404, T/S gone, suppression audit/effects 0, tenant audit 1")
				}
			} else if suppression.err == nil {
				r5SuppressionState(t, s, 1, 0, 1, 0)
				if r5SuppressionMetadata(t, s) != metadata {
					t.Fatal("tenant victim changed surviving tenant/user metadata")
				}
			} else {
				t.Fatal("neither complete command committed")
			}
			if suppDead || tenantDead {
				t.Errorf("actual formal suppression/tenant 40P01; source/tenant/users/audit/effects rollback verified: suppression=%s tenant=%s", r5SQLState(suppression.err), r5SQLState(tenant.err))
			}
		})
	}
}

func r5SuppressionMetadata(t *testing.T, s *r5SuppressionFixture) string {
	t.Helper()
	var metadata string
	must(t, s.f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('tenant',(SELECT to_jsonb(t) FROM tenants t WHERE id=$1),'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE tenant_id=$1))::text`, s.f.tenant.ID).Scan(&metadata))
	return metadata
}
func r5SuppressionSnapshot(t *testing.T, s *r5SuppressionFixture) string {
	t.Helper()
	var state string
	must(t, s.f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('tenant',(SELECT to_jsonb(t) FROM tenants t WHERE id=$1),'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE tenant_id=$1),'suppression',(SELECT to_jsonb(x) FROM suppression_list x WHERE id=$2),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM outbox_events e))::text`, s.f.tenant.ID, s.entry.ID).Scan(&state))
	return state
}
func r5SuppressionState(t *testing.T, s *r5SuppressionFixture, tenant, suppression, suppAudits, tenantAudits int) {
	t.Helper()
	var got [8]int
	must(t, s.f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM tenants WHERE id=$1),(SELECT count(*) FROM suppression_list WHERE id=$2),(SELECT count(*) FROM audit_log WHERE action='suppression.delete' AND resource_id=$2),(SELECT count(*) FROM audit_log WHERE action='tenant.delete' AND resource_id=$1),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM messages WHERE tenant_id=$1)+(SELECT count(*) FROM mail_documents WHERE tenant_id=$1)+(SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1),(SELECT count(*) FROM mailbox_event_log WHERE tenant_id=$1),(SELECT count(*) FROM users WHERE tenant_id=$1)`, s.f.tenant.ID, s.entry.ID).Scan(&got[0], &got[1], &got[2], &got[3], &got[4], &got[5], &got[6], &got[7]))
	want := [8]int{tenant, suppression, suppAudits, tenantAudits, 0, 0, 0, 2 * tenant}
	if got != want {
		t.Fatalf("tenant/S/audit/outbox/content refs/events effects: got=%v want=%v", got, want)
	}
}

func r5SuppressionTerminal(t *testing.T, s *r5SuppressionFixture, tenant bool) error {
	t.Helper()
	done := s.observed.suppression
	if tenant {
		done = s.observed.tenant
	}
	select {
	case err := <-done:
		return err
	default:
		t.Fatal("HTTP fixture did not reach the real PgStore command")
		return nil
	}
}
