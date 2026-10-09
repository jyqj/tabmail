package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

type r5AuthorityResult struct {
	status   int
	code     string
	err      error
	observed bool
}
type r5AuthorityKey struct {
	id    uuid.UUID
	raw   string
	owned bool
}

// Adding the new port observation in this exclusively owned file leaves the
// frozen AB source untouched. Original handlers never call it; candidate
// handlers must reach the actual PgStore implementation, never an auth stub.
func (s *r5SuppressionObservedStore) DeleteSuppressionAuthorized(ctx context.Context, actor authz.Actor, id uuid.UUID, audit models.AuditEntry) error {
	writer, ok := any(s.PgStore).(interface {
		DeleteSuppressionAuthorized(context.Context, authz.Actor, uuid.UUID, models.AuditEntry) error
	})
	if !ok {
		return errors.New("HTTP fixture called an authorized port missing from this original PgStore")
	}
	err := writer.DeleteSuppressionAuthorized(ctx, actor, id, audit)
	s.suppression <- err
	return err
}

type r5AuthorityKeyObservedStore struct {
	*r5SuppressionObservedStore
	createAttempted bool
	createErr       error
}

func (s *r5AuthorityKeyObservedStore) CreateAPIKey(ctx context.Context, key *models.TenantAPIKey) error {
	s.createAttempted = true
	s.createErr = s.PgStore.CreateAPIKey(ctx, key)
	return s.createErr
}

var r5AuthorityKeyObservers sync.Map

func r5AuthorityFixture(t *testing.T) *r5SuppressionFixture {
	t.Helper()
	s := r5SuppressionOrderFixture(t)
	// Demoting/freezing the target must not trip the last-administrator guard.
	spare := &models.User{TenantID: s.f.tenant.ID, Role: models.RoleAdmin, Email: uuid.NewString() + "@test.invalid", PasswordHash: "test-only-spare-hash", IsActive: true}
	must(t, s.f.st.CreateUser(context.Background(), spare))
	// Same official router and prerequisites, with an exact error-only key
	// INSERT observer. Never report generated raw credentials, hashes or rows.
	observed := &r5AuthorityKeyObservedStore{r5SuppressionObservedStore: s.observed}
	r5AuthorityKeyObservers.Store(s, observed)
	t.Cleanup(func() { r5AuthorityKeyObservers.Delete(s) })
	rdbServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: rdbServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	objects := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay"}, s.f.st, s.f.st, zerolog.Nop())
	svc.SetObjectStore(objects)
	s.router = api.NewRouter(api.RouterConfig{Store: observed, ObjectStore: objects, JWTSecret: "r5-suppression-test-jwt", MailboxTokenSecret: "r5-unused-mailbox-secret", CompanyOnly: true, CompanyRepository: s.f.st, OutboundService: svc, RateLimiter: middleware.NewRateLimiter(rdb, s.f.st, 10000, nil), Logger: zerolog.Nop()})
	return s
}

func r5AuthorityHTTP(s *r5SuppressionFixture, ctx context.Context, method, path, body, token, key string) r5AuthorityResult {
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	var envelope struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	r := r5AuthorityResult{status: w.Code}
	if envelope.Error != nil {
		r.code = envelope.Error.Code
	}
	// A credential-writer HTTP response must never consume the concurrent
	// suppression command's terminal receipt from the observing adapter.
	if strings.HasPrefix(path, "/api/v1/suppression/") {
		select {
		case r.err = <-s.observed.suppression:
			r.observed = true
		default:
		}
	}
	return r
}

func r5AuthoritySuppression(s *r5SuppressionFixture, ctx context.Context, token, key string) r5AuthorityResult {
	return r5AuthorityHTTP(s, ctx, http.MethodDelete, "/api/v1/suppression/"+s.entry.ID.String(), `{"reason":"documented authority wait removal"}`, token, key)
}

func r5AuthorityIssueKey(t *testing.T, s *r5SuppressionFixture, owned bool, scope string) r5AuthorityKey {
	t.Helper()
	ctx := context.Background()
	path, token := "/api/v1/admin/tenants/"+s.f.tenant.ID.String()+"/keys", s.superToken
	if owned {
		path, token = "/api/v1/keys", s.userToken
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"label":"AC authority fixture","scopes":["`+scope+`"]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		value, _ := r5AuthorityKeyObservers.Load(s)
		observed, _ := value.(*r5AuthorityKeyObservedStore)
		var pg *pgconn.PgError
		if observed != nil && errors.As(observed.createErr, &pg) {
			definition := ""
			if pg.ConstraintName != "" {
				must(t, s.f.pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1 AND connamespace='public'::regnamespace`, pg.ConstraintName).Scan(&definition))
			}
			t.Fatalf("normal API-key POST prerequisite failed: status=%d owned=%v SQLSTATE=%s table=%s column=%s constraint=%s catalog=%s; key/hash/detail omitted; authority race not executed", w.Code, owned, pg.Code, pg.TableName, pg.ColumnName, pg.ConstraintName, definition)
		}
		attempted := observed != nil && observed.createAttempted
		t.Fatalf("normal API-key creation precondition failed: status=%d owned=%v PGInsertObserved=%v; not an authority race result", w.Code, owned, attempted)
	}
	var response struct {
		Data struct {
			ID  uuid.UUID `json:"id"`
			Key string    `json:"key"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &response))
	if response.Data.ID == uuid.Nil || response.Data.Key == "" {
		t.Fatal("key creation did not return a fixture credential")
	}
	k, err := s.f.st.GetAPIKey(ctx, response.Data.ID)
	must(t, err)
	if k == nil || (owned && (k.OwnerUserID == nil || *k.OwnerUserID != s.f.employee.ID)) || (!owned && k.OwnerUserID != nil) {
		t.Fatal("key creation has wrong principal ownership")
	}
	return r5AuthorityKey{response.Data.ID, response.Data.Key, owned}
}

func TestR5SuppressionAuthorityNormalContracts(t *testing.T) {
	for _, kind := range []string{"jwt-admin", "jwt-regular-denied", "ownerless-manage-key", "regular-owned-manage-key", "read-only-key-denied"} {
		t.Run(kind, func(t *testing.T) {
			s := r5AuthorityFixture(t)
			before := r5AuthoritySource(t, s)
			ctx := context.Background()
			token, key, allowed := s.adminToken, "", true
			var principalKey *r5AuthorityKey
			if kind == "jwt-regular-denied" {
				token, allowed = s.userToken, false
			}
			if strings.Contains(kind, "key") {
				scope := "suppression:manage"
				if kind == "read-only-key-denied" {
					scope = "suppression:read"
					allowed = false
				}
				k := r5AuthorityIssueKey(t, s, kind == "regular-owned-manage-key", scope)
				principalKey = &k
				token, key = "", k.raw
			}
			result := r5AuthoritySuppression(s, ctx, token, key)
			if allowed {
				if result.status != http.StatusNoContent || result.err != nil {
					t.Fatalf("normal admission failed: status=%d code=%s", result.status, result.code)
				}
				r5AuthorityEffect(t, s, before, true)
				if principalKey != nil {
					var actor string
					must(t, s.f.pool.QueryRow(ctx, `SELECT actor FROM audit_log WHERE action='suppression.delete' AND resource_id=$1`, s.entry.ID).Scan(&actor))
					want := (authz.Actor{Type: authz.PrincipalAPIKey, ID: principalKey.id}).AuditLabel()
					if actor != want {
						t.Fatal("owned key became its owner's JWT principal in the audit")
					}
				}
			} else {
				if result.status != http.StatusForbidden || result.observed {
					t.Fatalf("normal denial reached writer or wrong wire: status=%d observed=%v", result.status, result.observed)
				}
				r5AuthorityEffect(t, s, before, false)
			}
		})
	}
}

// A: the real guarded member command owns T/U before this request reaches
// its T fence. Middleware may see the old MVCC role/session; that is not a
// final write authorization after the guarded command commits.
func TestR5SuppressionJWTGuardedAuthorityAfterParentWait(t *testing.T) {
	for _, change := range []string{"role-demotion", "active-freeze"} {
		t.Run(change, func(t *testing.T) {
			s := r5AuthorityFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			before := r5AuthoritySource(t, s)
			claims, err := authn.VerifyAccessToken("r5-suppression-test-jwt", s.superToken)
			must(t, err)
			version := claims.SessionVersion
			actor := authz.Actor{Type: authz.PrincipalUser, ID: claims.UserID, TenantID: s.f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, IsAdmin: true, SessionVersion: &version}
			patch := models.UserAdminPatch{}
			if change == "role-demotion" {
				role := models.RoleUser
				patch.Role = &role
			} else {
				active := false
				patch.IsActive = &active
			}
			gate := r5AuthorityAuditGate(t, s, ctx, s.f.admin.ID, "user.update")
			defer gate.Rollback(context.Background())
			writerDone := make(chan r5AuthorityResult, 1)
			go func() {
				_, e := s.f.st.UpdateUserGuarded(ctx, actor, s.f.tenant.ID, s.f.admin.ID, patch)
				writerDone <- r5AuthorityResult{err: e}
			}()
			writerPID := r5WaitBlockedBy(t, s.f, ctx, gate.Conn().PgConn().PID(), "audit_log")
			sourceDone := make(chan r5AuthorityResult, 1)
			go func() { sourceDone <- r5AuthoritySuppression(s, ctx, s.adminToken, "") }()
			pid, early := r5AuthorityWaitOrDone(t, s, ctx, writerPID, sourceDone)
			if !early {
				r5MaintenanceTrace(t, s.f, ctx, gate.Conn().PgConn().PID(), writerPID, pid)
			}
			must(t, gate.Rollback(ctx))
			writer := r5AuthorityAwait(t, ctx, writerDone)
			must(t, writer.err)
			current, err := s.f.st.GetUser(ctx, s.f.admin.ID)
			must(t, err)
			if current == nil || current.SessionVersion != s.f.admin.SessionVersion+1 || (change == "role-demotion" && current.Role != models.RoleUser) || (change == "active-freeze" && current.IsActive) {
				t.Fatal("guarded credential writer did not commit its own intended effects")
			}
			result := r5AuthorityAwait(t, ctx, sourceDone)
			r5AuthorityAdmission(t, s, before, result, "JWT "+change+" after real guarded parent wait")
		})
	}
}

// B: password mutation is T KEY SHARE, so a tenant FK fence alone does not
// serialize it with JWT writes. Accept either real U-fence serialization or
// rejection after the password/session writer has actually committed.
func TestR5SuppressionJWTSessionVersionAfterSourceWait(t *testing.T) {
	s := r5AuthorityFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	before := r5AuthoritySource(t, s)
	gate, err := s.f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(context.Background())
	_, err = gate.Exec(ctx, `SELECT id FROM suppression_list WHERE id=$1 FOR UPDATE`, s.entry.ID)
	must(t, err)
	sourceDone := make(chan r5AuthorityResult, 1)
	go func() { sourceDone <- r5AuthoritySuppression(s, ctx, s.adminToken, "") }()
	sourcePID, sourceEarly := r5AuthorityWaitOrDone(t, s, ctx, gate.Conn().PgConn().PID(), sourceDone)
	writerDone := make(chan r5AuthorityResult, 1)
	nextHash := "test-only-AC-next-password-hash"
	go func() {
		e := s.f.st.ChangePasswordAtomic(ctx, s.f.admin.ID, s.f.admin.PasswordHash, nextHash)
		writerDone <- r5AuthorityResult{err: e}
	}()
	serialized := false
	if !sourceEarly {
		pid, done := r5AuthorityWaitOrDone(t, s, ctx, sourcePID, writerDone)
		serialized = !done
		if serialized {
			r5MaintenanceTrace(t, s.f, ctx, gate.Conn().PgConn().PID(), sourcePID, pid)
		}
	}
	must(t, gate.Rollback(ctx))
	result := r5AuthorityAwait(t, ctx, sourceDone)
	writer := r5AuthorityAwait(t, ctx, writerDone)
	must(t, writer.err)
	current, err := s.f.st.GetUser(ctx, s.f.admin.ID)
	must(t, err)
	if current == nil || current.SessionVersion != s.f.admin.SessionVersion+1 || current.PasswordHash != nextHash || !current.IsActive {
		t.Fatal("password writer effects were lost or rewritten")
	}
	if serialized {
		if result.status != http.StatusNoContent || result.err != nil {
			t.Fatal("authorized earlier writer lost its serialized effect receipt")
		}
		r5AuthorityEffect(t, s, before, true)
		t.Log("actual user-fence serialization: source committed before password/session mutation")
	} else {
		r5AuthorityAdmission(t, s, before, result, "old JWT SessionVersion after committed password writer")
	}
}

// C: a regular user's owned key with manage scope is valid even though that
// user's JWT is not an administrator. A formal owner DELETE revokes the key;
// either the key fence serializes it or the later suppression write refuses.
func TestR5SuppressionKeyRevocationAfterSourceWait(t *testing.T) {
	s := r5AuthorityFixture(t)
	key := r5AuthorityIssueKey(t, s, true, "suppression:manage")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	before := r5AuthoritySource(t, s)
	gate, err := s.f.pool.Begin(ctx)
	must(t, err)
	defer gate.Rollback(context.Background())
	_, err = gate.Exec(ctx, `SELECT id FROM suppression_list WHERE id=$1 FOR UPDATE`, s.entry.ID)
	must(t, err)
	sourceDone := make(chan r5AuthorityResult, 1)
	go func() { sourceDone <- r5AuthoritySuppression(s, ctx, "", key.raw) }()
	sourcePID, sourceEarly := r5AuthorityWaitOrDone(t, s, ctx, gate.Conn().PgConn().PID(), sourceDone)
	writerDone := make(chan r5AuthorityResult, 1)
	go func() {
		writerDone <- r5AuthorityHTTP(s, ctx, http.MethodDelete, "/api/v1/keys/"+key.id.String(), "", s.userToken, "")
	}()
	serialized := false
	if !sourceEarly {
		pid, done := r5AuthorityWaitOrDone(t, s, ctx, sourcePID, writerDone)
		serialized = !done
		if serialized {
			r5MaintenanceTrace(t, s.f, ctx, gate.Conn().PgConn().PID(), sourcePID, pid)
		}
	}
	must(t, gate.Rollback(ctx))
	result := r5AuthorityAwait(t, ctx, sourceDone)
	writer := r5AuthorityAwait(t, ctx, writerDone)
	if writer.status != http.StatusNoContent {
		t.Fatalf("formal owned-key delete did not succeed: status=%d code=%s", writer.status, writer.code)
	}
	remaining, err := s.f.st.GetAPIKey(ctx, key.id)
	must(t, err)
	if remaining != nil {
		t.Fatal("key-revocation writer did not commit")
	}
	if serialized {
		if result.status != http.StatusNoContent || result.err != nil {
			t.Fatal("authorized earlier key writer lost its effect receipt")
		}
		r5AuthorityEffect(t, s, before, true)
		t.Log("actual key-fence serialization: source committed before key deletion")
	} else {
		r5AuthorityAdmission(t, s, before, result, "revoked owned manage key after source wait")
	}
}

func r5AuthorityAuditGate(t *testing.T, s *r5SuppressionFixture, ctx context.Context, id uuid.UUID, action string) pgx.Tx {
	t.Helper()
	name := "r5_authority_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	key := "r5-authority-gate:" + id.String()
	_, err := s.f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_id='`+id.String()+`'::uuid AND NEW.action='`+action+`' THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+key+`',0)); END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
	must(t, err)
	gate, err := s.f.pool.Begin(ctx)
	must(t, err)
	_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
	must(t, err)
	return gate
}

func r5AuthorityWaitOrDone(t *testing.T, s *r5SuppressionFixture, ctx context.Context, blocker uint32, done chan r5AuthorityResult) (uint32, bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case r := <-done:
			done <- r
			return 0, true
		default:
		}
		var pid uint32
		err := s.f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return pid, false
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("no actual SQL wait or command terminal observed; not authority-race evidence")
		case <-ticker.C:
		}
	}
}
func r5AuthorityAwait(t *testing.T, ctx context.Context, done chan r5AuthorityResult) r5AuthorityResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-ctx.Done():
		t.Fatal("complete command terminal missing; timeout is not target evidence")
		return r5AuthorityResult{}
	}
}
func r5AuthoritySource(t *testing.T, s *r5SuppressionFixture) string {
	t.Helper()
	var source string
	must(t, s.f.pool.QueryRow(context.Background(), `SELECT to_jsonb(x)::text FROM suppression_list x WHERE id=$1`, s.entry.ID).Scan(&source))
	return source
}
func r5AuthorityEffect(t *testing.T, s *r5SuppressionFixture, before string, deleted bool) {
	t.Helper()
	var sources, audits, outboxes int
	must(t, s.f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM suppression_list WHERE id=$1),(SELECT count(*) FROM audit_log WHERE action='suppression.delete' AND resource_id=$1),(SELECT count(*) FROM outbox_events)`, s.entry.ID).Scan(&sources, &audits, &outboxes))
	wantSources, wantAudits := 1, 0
	if deleted {
		wantSources, wantAudits = 0, 1
	}
	if sources != wantSources || audits != wantAudits || outboxes != 0 {
		t.Fatalf("suppression-owned effects/required audit/outbox mismatch: source=%d audit=%d outbox=%d", sources, audits, outboxes)
	}
	if !deleted && r5AuthoritySource(t, s) != before {
		t.Fatal("authority refusal rewrote suppression source bytes")
	}
}
func r5AuthorityAdmission(t *testing.T, s *r5SuppressionFixture, before string, r r5AuthorityResult, condition string) {
	t.Helper()
	if r.status == http.StatusNoContent && r.err == nil {
		r5AuthorityEffect(t, s, before, true)
		t.Errorf("actual stale authority admitted: %s; HTTP204 with committed suppression delete/audit after credential writer won", condition)
		return
	}
	if r.status == http.StatusUnauthorized && r.code == "UNAUTHORIZED" || r.status == http.StatusForbidden && r.code == "FORBIDDEN" {
		if r.observed {
			e, ok := app.As(app.FromAuthz(r.err))
			if !ok || e.Kind != app.KindForbidden {
				t.Fatalf("credential denial wire concealed an unknown writer error: SQLSTATE=%s err=%v", r5SQLState(r.err), r.err)
			}
		}
		r5AuthorityEffect(t, s, before, false)
		return
	}
	if r.status == http.StatusConflict && r.code == "CONFLICT" {
		e, ok := app.As(r.err)
		if !ok || e.Kind != app.KindConflict {
			t.Fatal("conflict lacks an exact bounded writer refusal")
		}
		r5AuthorityEffect(t, s, before, false)
		t.Log("bounded busy refusal; not a claim of post-wait authority refresh")
		return
	}
	t.Fatalf("unexpected authority result, not accepted as rejection: status=%d code=%s SQLSTATE=%s err=%v", r.status, r.code, r5SQLState(r.err), r.err)
}
