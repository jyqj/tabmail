package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/store"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testutil"
)

const r5TouchSQL = `UPDATE tenant_api_key_usage SET last_used_at=$2,last_used_ip=$3 WHERE api_key_id=$1 AND (last_used_at IS NULL OR last_used_at<$2)`
const r5TouchPermissionSQL = `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`
const r5TouchIP = "192.0.2.41"

type r5TouchCall struct {
	id uuid.UUID
	ip string
}

// This is scheduling instrumentation, NOT a replacement auth/receipt oracle.
// Auth still launches its own goroutine; Touch delegates immediately, without
// changing its context, key, IP or error. The receipt seam waits for observable
// PostgreSQL state and then calls the actual PgStore method, unchanged.
// Consequently this can prove a possible causal interleaving, not identify the
// unknown holder in a historical 55P03 log. Schema19 proves a new boundary;
// it does not relabel the frozen schema18 witness.
type r5TouchAdmissionStore struct {
	*postgres.PgStore
	started chan r5TouchCall
	done    chan struct{}
	result  error // published by closing done
	calls   atomic.Int32
	workers sync.WaitGroup
	key     uuid.UUID
	job     uuid.UUID
	before  func(context.Context) error
	once    sync.Once
	gateErr error
	reads   int // synchronous in-process router only
}

func (s *r5TouchAdmissionStore) TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error {
	s.workers.Add(1)
	defer s.workers.Done()
	first := s.calls.Add(1) == 1
	if first {
		s.started <- r5TouchCall{id: id, ip: ip}
	}
	err := s.PgStore.TouchAPIKey(ctx, id, ip)
	if first {
		s.result = err
		close(s.done)
	}
	return err
}

func (s *r5TouchAdmissionStore) GetOutboundReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	s.reads++
	if a.Type != authz.PrincipalAPIKey || a.ID != s.key || id != s.job {
		return nil, errors.New("receipt did not use the same fixture key and job")
	}
	s.once.Do(func() { s.gateErr = s.before(ctx) })
	if s.gateErr != nil {
		return nil, s.gateErr
	}
	return s.PgStore.GetOutboundReceipt(ctx, a, id, scope)
}

// BEFORE STATEMENT runs before ExecModifyTable fetches the first candidate row:
// PostgreSQL REL_16_STABLE, src/backend/executor/nodeModifyTable.c,
// ExecModifyTable: fireBSTriggers(node), then ExecProcNode(subplanstate).
// https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/executor/nodeModifyTable.c
// https://www.postgresql.org/docs/16/trigger-definition.html
// BEFORE ROW is deliberately NOT used: obtaining that row may already lock it.
// AFTER ROW holds the real metadata UPDATE's tuple lock until actual commit.
// Both gates preserve relation locks and all production FOR SHARE NOWAIT reads.
func r5TouchInstallGate(t *testing.T, f *companyFixture, key uuid.UUID, phase string) pgx.Tx {
	t.Helper()
	if phase != "before-statement" && phase != "after-row" {
		t.Fatal("unknown touch gate phase")
	}
	ctx := context.Background()
	var keyCount int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM tenant_api_keys`).Scan(&keyCount))
	if keyCount != 1 {
		t.Fatal("statement gate requires exactly the one owned fixture key")
	}
	// No roles, SECURITY DEFINER, shared schemas, server setting or external DB.
	// A row trace becomes visible only when the actual Touch transaction commits.
	_, err := f.pool.Exec(ctx, fmt.Sprintf(`
CREATE TABLE r5_touch_trace(key_id uuid NOT NULL, holder_pid integer NOT NULL,
 phase text NOT NULL, usage_identity_unchanged boolean NOT NULL,
 previous_at timestamptz, used_at timestamptz, used_ip inet);
CREATE FUNCTION r5_touch_gate() RETURNS trigger LANGUAGE plpgsql AS $gate$
BEGIN
 IF TG_LEVEL = 'STATEMENT' THEN
  IF '%s' = 'before-statement' THEN PERFORM pg_advisory_xact_lock(529014,1); END IF;
  RETURN NULL;
 END IF;
 IF NEW.api_key_id <> '%s'::uuid THEN RAISE EXCEPTION 'unexpected fixture key'; END IF;
 INSERT INTO r5_touch_trace VALUES(NEW.api_key_id,pg_backend_pid(),'%s',
  (to_jsonb(OLD)-'last_used_at'-'last_used_ip') = (to_jsonb(NEW)-'last_used_at'-'last_used_ip'),
  OLD.last_used_at,NEW.last_used_at,NEW.last_used_ip);
 IF '%s' = 'after-row' THEN PERFORM pg_advisory_xact_lock(529014,1); END IF;
 RETURN NEW;
END $gate$;
CREATE TRIGGER r5_touch_before BEFORE UPDATE OF last_used_at,last_used_ip ON tenant_api_key_usage
 FOR EACH STATEMENT EXECUTE FUNCTION r5_touch_gate();
CREATE TRIGGER r5_touch_after AFTER UPDATE OF last_used_at,last_used_ip ON tenant_api_key_usage
 FOR EACH ROW EXECUTE FUNCTION r5_touch_gate();`, phase, key, phase, phase))
	must(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := f.pool.Exec(ctx, `DROP TRIGGER r5_touch_before ON tenant_api_key_usage;
DROP TRIGGER r5_touch_after ON tenant_api_key_usage; DROP FUNCTION r5_touch_gate(); DROP TABLE r5_touch_trace`)
		if err != nil {
			t.Errorf("owned touch fixture teardown failed: %T", err)
		}
	})
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { r5TouchRollback(t, hold) })
	_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(529014,1)`)
	must(t, err)
	return hold
}

func r5TouchRollback(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if tx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("owned transaction rollback failed: %T", err)
	}
}

// Poll only an explicit lock/transaction predicate; elapsed time never stands
// in for reaching a phase. Parent context bounds all observer queries.
func r5TouchAwait(ctx context.Context, check func() (bool, error)) error {
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := check()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func r5TouchWaitGate(ctx context.Context, f *companyFixture, gatePID uint32) (int32, error) {
	var pid int32
	err := r5TouchAwait(ctx, func() (bool, error) {
		var query string
		err := f.pool.QueryRow(ctx, `SELECT a.pid,a.query FROM pg_stat_activity a
WHERE a.datname=current_database() AND a.wait_event_type='Lock' AND a.wait_event='advisory'
 AND pg_blocking_pids(a.pid)=ARRAY[$1::integer]
 AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.locktype='advisory'
  AND NOT l.granted AND l.classid=529014 AND l.objid=1 AND l.objsubid=2)`, gatePID).Scan(&pid, &query)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if query != r5TouchSQL {
			return false, errors.New("advisory waiter is not the real metadata UPDATE")
		}
		return true, nil
	})
	return pid, err
}

// Diagnostics classify only known protocol fields/fixed fixture errors. Never
// print an error message, SQL, DSN, request headers or response body verbatim.
func r5TouchErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "no_rows"
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		if len(pgerr.Code) == 5 && strings.IndexFunc(pgerr.Code, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') }) == -1 {
			return "sqlstate_" + pgerr.Code
		}
		return "postgres_unclassified"
	}
	switch err.Error() {
	case "auth launched telemetry for another key or IP":
		return "fixture_touch_identity_mismatch"
	case "auth did not launch real telemetry":
		return "fixture_touch_not_launched"
	case "advisory waiter is not the real metadata UPDATE":
		return "fixture_update_query_mismatch"
	case "row-free probe selected another key":
		return "fixture_row_identity_mismatch"
	default:
		return fmt.Sprintf("type_%T", err)
	}
}

func r5TouchHTTPClass(w *httptest.ResponseRecorder) (string, string) {
	var envelope struct {
		Error struct{ Code, Message string }
	}
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
		return "not_json", "unclassified"
	}
	code := "unclassified"
	switch envelope.Error.Code {
	case "", "CONFLICT", "UNAUTHORIZED", "FORBIDDEN", "NOT_FOUND", "INTERNAL", "BAD_REQUEST", "RATE_LIMITED", "QUOTA_EXCEEDED":
		code = envelope.Error.Code
		if code == "" {
			code = "no_error_code"
		}
	}
	reason := "unclassified"
	switch envelope.Error.Message {
	case "":
		reason = "no_error_message"
	case "outbound job not found":
		reason = "outbound_not_found"
	case "key is changing; reload before retrying":
		reason = "authority_key_changing"
	case "invalid api key":
		reason = "auth_invalid_key"
	case "key lookup failed":
		reason = "auth_key_lookup"
	case "api key owner lookup failed":
		reason = "auth_owner_lookup"
	case "api key owner not found or inactive":
		reason = "auth_owner_unavailable"
	case "failed to load api key owner permissions":
		reason = "auth_owner_permissions"
	case "internal server error":
		reason = "internal"
	}
	return code, reason
}

// The diagnostic SHARE waiter is not the production admission query. It proves
// the exact same-key blocker PID, then joins and releases its own tx
// BEFORE delegating to the untouched production FOR SHARE NOWAIT reader.
func r5TouchProveRowBlocker(ctx context.Context, t *testing.T, f *companyFixture, key uuid.UUID, holder int32, holderQuery string, usage bool) error {
	t.Helper()
	probe, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	pid := probe.Conn().PgConn().PID()
	// The server ends this diagnostic statement itself. Do not close the
	// connection with client cancellation and race its asynchronous rollback.
	if _, err = probe.Exec(ctx, `SET LOCAL statement_timeout='1500ms'`); err != nil {
		r5TouchRollback(t, probe)
		return err
	}
	done := make(chan error, 1)
	go func() {
		query := `SELECT id FROM tenant_api_keys WHERE id=$1 FOR SHARE`
		if usage {
			query = `SELECT api_key_id FROM tenant_api_key_usage WHERE api_key_id=$1 FOR SHARE`
		}
		_, err := probe.Exec(ctx, query, key)
		done <- err
	}()
	// Always join before releasing this pool borrower, including failed proofs.
	defer func() {
		select {
		case err := <-done:
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "57014" {
				t.Errorf("same-key diagnostic waiter did not end by its statement timeout: %T", err)
			}
		case <-time.After(6 * time.Second):
			t.Error("diagnostic same-key waiter did not join")
		}
		r5TouchRollback(t, probe)
	}()
	err = r5TouchAwait(ctx, func() (bool, error) {
		var blocked bool
		err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a
JOIN pg_stat_activity h ON h.pid=$2 WHERE a.pid=$1 AND a.datname=current_database()
 AND a.wait_event_type='Lock' AND pg_blocking_pids(a.pid)=ARRAY[$2::integer]
 AND h.query=$3 AND h.xact_start IS NOT NULL)`, pid, holder, holderQuery).Scan(&blocked)
		return blocked, err
	})
	if err == nil {
		queryClass := "authority_permission_update"
		if usage {
			queryClass = "usage_update"
		}
		t.Logf("phase=same_key_SHARE_wait waiter_pid=%d exact_blocker_pid=%d holder_query_class=%s", pid, holder, queryClass)
	}
	return err
}

func r5TouchRowFree(ctx context.Context, t *testing.T, f *companyFixture, key uuid.UUID) error {
	t.Helper()
	probe, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer r5TouchRollback(t, probe)
	var got uuid.UUID
	err = probe.QueryRow(ctx, `SELECT id FROM tenant_api_keys WHERE id=$1 FOR SHARE NOWAIT`, key).Scan(&got)
	if err == nil && got != key {
		return errors.New("row-free probe selected another key")
	}
	return err
}

func r5TouchRouter(f *companyFixture, st store.Store, svc *outbound.Service, obj *testutil.MemoryObjectStore) *api.Router {
	return api.NewRouter(api.RouterConfig{Store: st, CompanyRepository: f.st,
		ObjectStore: obj, RawObjects: rawobject.NewStore(obj, f.st),
		JWTSecret: companyTestJWT, MailboxTokenSecret: "test-only-mailbox-secret",
		PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull,
		CompanyOnly: true, RateLimiter: middleware.NewRateLimiter(nil, st, 10000, nil),
		OutboundService: svc, Logger: zerolog.Nop(), Readiness: f.st.Readiness})
}

func r5TouchHTTP(ctx context.Context, h http.Handler, key, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil).WithContext(ctx)
	req.RemoteAddr = r5TouchIP + ":41001"
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func r5TouchCommitted(t *testing.T, ctx context.Context, f *companyFixture, st *r5TouchAdmissionStore, key uuid.UUID, pid int32, phase string, since time.Time) {
	t.Helper()
	select {
	case <-st.done:
	case <-ctx.Done():
		t.Fatal("real asynchronous metadata UPDATE did not finish")
	}
	if st.result != nil {
		t.Fatalf("real asynchronous metadata UPDATE failed: %T", st.result)
	}
	var valid bool
	must(t, f.pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(
 tr.key_id=$1 AND tr.holder_pid=$2 AND tr.phase=$3 AND tr.usage_identity_unchanged
 AND tr.previous_at IS NULL AND tr.used_at >= $4 AND tr.used_at <= clock_timestamp()
 AND host(tr.used_ip)=$5 AND k.last_used_at=tr.used_at AND k.last_used_ip=tr.used_ip)
 FROM r5_touch_trace tr JOIN tenant_api_key_usage k ON k.api_key_id=tr.key_id`, key, pid, phase, since, r5TouchIP).Scan(&valid))
	if !valid || st.calls.Load() != 1 {
		t.Fatal("same-key telemetry did not commit exactly once with real timestamp/IP and unchanged authority")
	}
	must(t, r5TouchRowFree(ctx, t, f, key))
	t.Logf("phase=telemetry_committed holder_pid=%d trigger_phase=%s calls=1 last_used_at_and_ip_verified=true", pid, phase)
}

// Each router owns its own throttle. The independent GET is a second real
// observation, identified while its UPDATE waits for the primary usage holder.
// Join both lifecycle owners before checking this exact two-commit chain.
func r5TouchTwoRoutersCommitted(t *testing.T, ctx context.Context, f *companyFixture, primary, other *r5TouchAdmissionStore, key uuid.UUID, primaryPID, otherPID int32, phase string, since time.Time) {
	t.Helper()
	for _, st := range []*r5TouchAdmissionStore{primary, other} {
		select {
		case <-st.done:
		default:
			t.Fatal("router telemetry not physically joined")
		}
		if st.calls.Load() != 1 || st.result != nil {
			t.Fatalf("router telemetry calls=%d result_class=%s", st.calls.Load(), r5TouchErrorClass(st.result))
		}
	}
	if primaryPID == 0 || otherPID == 0 || primaryPID == otherPID {
		t.Fatal("two routers did not use independently observed backend identities")
	}
	var valid bool
	must(t, f.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM r5_touch_trace)=2
 AND (SELECT count(*) FROM r5_touch_trace WHERE holder_pid=$2)=1
 AND (SELECT count(*) FROM r5_touch_trace WHERE holder_pid=$3)=1
 AND EXISTS(SELECT 1 FROM r5_touch_trace p JOIN r5_touch_trace o ON o.key_id=p.key_id
 JOIN tenant_api_key_usage u ON u.api_key_id=p.key_id
 WHERE p.key_id=$1 AND p.holder_pid=$2 AND o.holder_pid=$3
 AND p.phase=$4 AND o.phase=$4 AND p.usage_identity_unchanged AND o.usage_identity_unchanged
 AND p.previous_at IS NULL AND o.previous_at=p.used_at AND o.used_at>p.used_at
 AND p.used_at >= $5 AND o.used_at >= $5
 AND p.used_at <= clock_timestamp() AND o.used_at <= clock_timestamp()
 AND host(p.used_ip)=$6 AND host(o.used_ip)=$6
 AND u.last_used_at=(SELECT max(used_at) FROM r5_touch_trace)
 AND u.last_used_at=o.used_at AND u.last_used_ip=o.used_ip)`, key, primaryPID, otherPID, phase, since, r5TouchIP).Scan(&valid))
	if !valid {
		t.Fatal("two independent router touches did not each commit exactly once with real timestamp/IP, unchanged identity and current latest usage")
	}
	must(t, r5TouchRowFree(ctx, t, f, key))
	t.Logf("phase=two_router_telemetry_committed primary_pid=%d independent_pid=%d calls_each=1 traces_each=1 current_latest_timestamp_and_ip_verified=true", primaryPID, otherPID)
}

// Report each original aggregate component after physically joining both owners.
func r5TouchDiagnose(t *testing.T, ctx context.Context, f *companyFixture, primary, other *r5TouchAdmissionStore, key uuid.UUID, pid, otherPID int32, phase string, since time.Time) {
	t.Helper()
	t.Logf("phase=joined_calls primary_pid=%d primary_calls=%d primary_result=%s independent_pid=%d independent_calls=%d independent_result=%s", pid, primary.calls.Load(), r5TouchErrorClass(primary.result), otherPID, other.calls.Load(), r5TouchErrorClass(other.result))
	rows, err := f.pool.Query(ctx, `SELECT tr.holder_pid,tr.used_at,host(tr.used_ip),tr.previous_at,
 tr.key_id=$1,tr.holder_pid=$2,tr.phase=$3,tr.usage_identity_unchanged,
 tr.previous_at IS NULL,tr.used_at >= $4,tr.used_at <= clock_timestamp(),host(tr.used_ip)=$5,
 k.last_used_at=tr.used_at,k.last_used_ip=tr.used_ip,(SELECT count(*) FROM r5_touch_trace)=1,
 k.last_used_at,host(k.last_used_ip)
 FROM r5_touch_trace tr JOIN tenant_api_key_usage k ON k.api_key_id=tr.key_id ORDER BY tr.used_at`, key, pid, phase, since, r5TouchIP)
	must(t, err)
	defer rows.Close()
	for rows.Next() {
		var holder int32
		var used, current time.Time
		var previous *time.Time
		var ip, currentIP string
		var identity, holderOK, phaseOK, unchanged, initial, lower, upper, ipOK, atEqual, ipEqual, countOne bool
		must(t, rows.Scan(&holder, &used, &ip, &previous, &identity, &holderOK, &phaseOK, &unchanged, &initial, &lower, &upper, &ipOK, &atEqual, &ipEqual, &countOne, &current, &currentIP))
		t.Logf("phase=original_valid_components holder_pid=%d used_at=%s used_ip=%s previous_at=%v key=%t primary_holder=%t trigger_phase=%t identity_unchanged=%t previous_null=%t lower_bound=%t upper_bound=%t expected_ip=%t current_timestamp_equal=%t current_ip_equal=%t count_one=%t current_usage_at=%s current_usage_ip=%s", holder, used.Format(time.RFC3339Nano), ip, previous, identity, holderOK, phaseOK, unchanged, initial, lower, upper, ipOK, atEqual, ipEqual, countOne, current.Format(time.RFC3339Nano), currentIP)
	}
	must(t, rows.Err())
}

// Schema19 successor, NOT the frozen schema18 causal witness: the old
// metadata-held 409 expectation/raw belongs to its original source only.
// Default (untagged): four serial, separately owned databases.
// Missing private fixture input is a failure, never a skip. There is no worker,
// SMTP server, redis server, HTTP listener, real mail, credential file or signal.
// This is additional default-package work; it does not repair/replace the old
// whole-suite result or justify silently widening/splitting its 180 s budget.
func TestR5KeyUsageAuthorityAdmissionIsolation(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("key-touch causality requires an explicit owned TABMAIL_TEST_DB_DSN")
	}
	for _, scenario := range []string{"usage_held_allows_retry", "telemetry_committed_then_zone_revoked", "permission_change_held_conflicts", "unrevoked_retry_and_telemetry_commit"} {
		t.Run(scenario, func(t *testing.T) {
			f := seedCompany(t) // fresh DB, never the supplied DSN's existing DB
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			key, _ := r5LegacyKey(t, f)
			otherZone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "other-touch.test", IsVerified: true, MXVerified: true}
			must(t, f.st.CreateZone(ctx, otherZone))
			_, err := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read","send:write"]'::jsonb WHERE id=$1`, key.ID)
			must(t, err)
			key.Scopes = []string{"send:read", "send:write"}
			obj := testutil.NewMemoryObjectStore()
			svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
			svc.SetObjectStore(obj)
			job := r5StageQueued(t, f, svc, f.personal, key, "synthetic@touch.test")
			_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, job.ID)
			must(t, err)
			phase := "before-statement"
			if scenario == "usage_held_allows_retry" {
				phase = "after-row"
			}
			independent, err := postgres.New(ctx, config.DB{DSN: f.pool.Config().ConnString(), MaxOpenConns: 4, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
			must(t, err)
			t.Cleanup(func() { _ = independent.Close() })
			gate := r5TouchInstallGate(t, f, key.ID, phase)
			st := &r5TouchAdmissionStore{PgStore: independent, started: make(chan r5TouchCall, 1), done: make(chan struct{}), key: key.ID, job: job.ID}
			otherStore := &r5TouchAdmissionStore{PgStore: f.st, started: make(chan r5TouchCall, 1), done: make(chan struct{}), key: key.ID, job: job.ID, before: func(context.Context) error { return nil }}
			var routers []*api.Router
			var permission pgx.Tx
			requested, joined := false, false
			// Later cleanup precedes trigger drops and testpg's synchronous pool
			// close/drop. Release both owners before joining auth's detached work.
			t.Cleanup(func() {
				r5TouchRollback(t, permission)
				r5TouchRollback(t, gate)
				if requested && !joined {
					select {
					case <-st.done:
						if st.result != nil {
							t.Errorf("cleanup joined failed telemetry UPDATE: %T", st.result)
						}
					case <-time.After(6 * time.Second):
						t.Error("auth telemetry not launched or not joined before owned fixture teardown")
					}
				}
				// Stop both actual lifecycle owners before trigger teardown, including
				// work admitted but not yet entered in the wrapper.
				joinCtx, joinCancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer joinCancel()
				for _, router := range routers {
					if err := router.StopContext(joinCtx); err != nil {
						t.Errorf("router telemetry join failed: %T", err)
					}
				}
				otherStore.workers.Wait()
				// Each router permits one launch within this 12 s case. Join all
				// wrapper-entered work after closing and joining both owners.
				st.workers.Wait()
			})
			var permissionPID uint32
			if scenario == "permission_change_held_conflicts" {
				permission, err = f.pool.Begin(ctx)
				must(t, err)
				permissionPID = permission.Conn().PgConn().PID()
				_, err = permission.Exec(ctx, r5TouchPermissionSQL, key.ID, []uuid.UUID{otherZone.ID})
				must(t, err)
			}
			path := "/api/v1/outbound/" + job.ID.String()
			rawKey := "tm_content_" + key.ID.String()
			otherRouter := r5TouchRouter(f, otherStore, svc, obj)
			routers = append(routers, otherRouter)
			var authorityBefore string
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(k)::text FROM tenant_api_keys k WHERE id=$1`, key.ID).Scan(&authorityBefore))
			var telemetryPID, otherPID int32
			gatePhase := "not_entered"
			otherStatus, otherCode, otherReason := 0, "not_called", "not_called"
			// Keep the case timer/cancel owner, not the outer POST's request
			// values. Reusing its chi RouteContext would carry the mounted path
			// and method into this independent GET. Do not reset the live POST.
			caseCtx := ctx
			st.before = func(ctx context.Context) error {
				gatePhase = "await_auth_touch_launch"
				select {
				case call := <-st.started:
					if call.id != key.ID || call.ip != r5TouchIP {
						return errors.New("auth launched telemetry for another key or IP")
					}
				case <-ctx.Done():
					return errors.New("auth did not launch real telemetry")
				}
				var err error
				gatePhase = "await_telemetry_advisory_gate"
				telemetryPID, err = r5TouchWaitGate(ctx, f, gate.Conn().PgConn().PID())
				if err != nil {
					return err
				}
				t.Logf("phase=%s telemetry_pid=%d advisory_blocker_pid=%d query_class=usage_update", phase, telemetryPID, gate.Conn().PgConn().PID())
				if phase == "after-row" {
					gatePhase = "prove_usage_row_blocker"
					if err := r5TouchProveRowBlocker(ctx, t, f, key.ID, telemetryPID, r5TouchSQL, true); err != nil {
						return err
					}
					gatePhase = "prove_authority_row_free"
					if err := r5TouchRowFree(ctx, t, f, key.ID); err != nil {
						return err
					}
					t.Log("phase=authority_row_free_after_usage_waiter_join outcome=pass")
					gatePhase = "independent_router_receipt"
					other := r5TouchHTTP(caseCtx, otherRouter, rawKey, "GET", path)
					otherStatus = other.Code
					otherCode, otherReason = r5TouchHTTPClass(other)
					t.Logf("phase=independent_router_receipt http_status=%d error_code=%s reason_class=%s", otherStatus, otherCode, otherReason)
					if other.Code != http.StatusOK {
						return fmt.Errorf("independent-pool router receipt HTTP=%d", other.Code)
					}
					select {
					case call := <-otherStore.started:
						if call.id != key.ID || call.ip != r5TouchIP {
							return errors.New("auth launched telemetry for another key or IP")
						}
					case <-ctx.Done():
						return errors.New("auth did not launch real telemetry")
					}
					// This second router's real UPDATE must wait on the primary's
					// usage tuple; identify its backend from that actual lock edge.
					err = r5TouchAwait(ctx, func() (bool, error) {
						err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database()
 AND query=$1 AND wait_event_type='Lock' AND pg_blocking_pids(pid)=ARRAY[$2::integer]`, r5TouchSQL, telemetryPID).Scan(&otherPID)
						if errors.Is(err, pgx.ErrNoRows) {
							return false, nil
						}
						return err == nil, err
					})
					if err != nil {
						return err
					}
					t.Logf("phase=second_router_touch_wait router=independent holder_pid=%d exact_blocker_pid=%d calls=%d", otherPID, telemetryPID, otherStore.calls.Load())
					t.Log("usage held: independent router/store/pool admitted same key; two pools, not two OS processes")
					gatePhase = "delegate_current_receipt"
					return nil
				}
				if permission != nil {
					gatePhase = "prove_authority_permission_blocker"
					if err := r5TouchProveRowBlocker(ctx, t, f, key.ID, int32(permissionPID), r5TouchPermissionSQL, false); err != nil {
						return err
					}
					gatePhase = "delegate_current_receipt"
					return nil
				}
				// Positive counterfactual: UPDATE truly reached the statement gate,
				// but the same-key SHARE NOWAIT probe can still take the row lock.
				gatePhase = "prove_pre_statement_authority_free"
				if err := r5TouchRowFree(ctx, t, f, key.ID); err != nil {
					return err
				}
				gatePhase = "delegate_current_receipt"
				return nil
			}
			h := r5TouchRouter(f, st, svc, obj)
			routers = append(routers, h)
			before := r5StageOwnerlessRetryState(t, f, job.ID)
			var since time.Time
			must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&since))
			method, requestPath, want := "POST", path+"/retry", http.StatusConflict
			if scenario == "telemetry_committed_then_zone_revoked" {
				method, requestPath, want = "GET", path, http.StatusOK
			} else if scenario == "unrevoked_retry_and_telemetry_commit" || scenario == "usage_held_allows_retry" {
				want = http.StatusOK
			}
			requested = true
			w := r5TouchHTTP(ctx, h, rawKey, method, requestPath)
			if st.gateErr != nil {
				t.Fatalf("causal gate evidence failed: phase=%s error_class=%s independent_http=%d independent_code=%s independent_reason=%s current_http=%d", gatePhase, r5TouchErrorClass(st.gateErr), otherStatus, otherCode, otherReason, w.Code)
			}
			if st.reads == 0 || w.Code != want {
				code, reason := r5TouchHTTPClass(w)
				t.Fatalf("real receipt admission phase=%s HTTP=%d want=%d reads=%d error_code=%s reason_class=%s", gatePhase, w.Code, want, st.reads, code, reason)
			}
			if want == http.StatusConflict {
				var envelope struct {
					Error struct{ Code, Message string }
				}
				must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
				if envelope.Error.Code != "CONFLICT" || envelope.Error.Message != "key is changing; reload before retrying" {
					t.Fatal("409 was not the current key receipt-admission conflict")
				}
			}
			if scenario != "unrevoked_retry_and_telemetry_commit" && scenario != "usage_held_allows_retry" && before != r5StageOwnerlessRetryState(t, f, job.ID) {
				t.Fatal("receipt-only or denied retry changed job/recipients/attempts/audit/outbox")
			}
			r5TouchRollback(t, permission)
			r5TouchRollback(t, gate)
			select {
			case <-st.done:
			case <-ctx.Done():
				t.Fatal("primary telemetry did not join")
			}
			if scenario == "usage_held_allows_retry" {
				must(t, h.StopContext(ctx))
				must(t, otherRouter.StopContext(ctx))
			}
			st.workers.Wait()
			otherStore.workers.Wait()
			r5TouchDiagnose(t, ctx, f, st, otherStore, key.ID, telemetryPID, otherPID, phase, since)
			if scenario == "usage_held_allows_retry" {
				r5TouchTwoRoutersCommitted(t, ctx, f, st, otherStore, key.ID, telemetryPID, otherPID, phase, since)
			} else {
				r5TouchCommitted(t, ctx, f, st, key.ID, telemetryPID, phase, since)
			}
			joined = true
			var authorityAfter string
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(k)::text FROM tenant_api_keys k WHERE id=$1`, key.ID).Scan(&authorityAfter))
			if authorityBefore != authorityAfter {
				t.Fatal("telemetry changed the authority row")
			}
			if scenario != "unrevoked_retry_and_telemetry_commit" && scenario != "usage_held_allows_retry" && before != r5StageOwnerlessRetryState(t, f, job.ID) {
				t.Fatal("denied/read-only state changed after real telemetry commit")
			}
			if scenario == "telemetry_committed_then_zone_revoked" {
				_, err = f.pool.Exec(ctx, r5TouchPermissionSQL, key.ID, []uuid.UUID{otherZone.ID})
				must(t, err) // autocommit returned before the new request
				before = r5StageOwnerlessRetryState(t, f, job.ID)
				w = r5TouchHTTP(ctx, h, rawKey, "POST", path+"/retry")
				// No sleeps/reset of auth's real one-minute touch throttle. Its first
				// update has committed, and the second request remains within 12 s.
				if w.Code != http.StatusNotFound || st.reads != 2 || st.calls.Load() != 1 {
					t.Fatalf("stable zone revocation HTTP=%d want=404 receipt_reads=%d touch_calls=%d", w.Code, st.reads, st.calls.Load())
				}
				if before != r5StageOwnerlessRetryState(t, f, job.ID) {
					t.Fatal("stable denied retry changed job/recipients/attempts/audit/outbox")
				}
			} else if scenario == "unrevoked_retry_and_telemetry_commit" || scenario == "usage_held_allows_retry" {
				current, err := f.st.GetOutboundJob(ctx, job.ID)
				must(t, err)
				var retries int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='outbound.retry'`, job.ID).Scan(&retries))
				var events int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE payload->'metadata'->>'resource_id'=$1 AND payload->'metadata'->>'action'='outbound.retry'`, job.ID.String()).Scan(&events))
				if current == nil || current.State != models.OutboundPending || retries != 1 || events != 1 || st.reads < 2 {
					t.Fatal("positive router retry did not really commit exactly once")
				}
			}
		})
	}
}

// Lifecycle coverage is deliberately separate from the same-key lock proof:
// these are actual public store commands, not an assertion that the entire
// tenant/offboarding graph is free of every pre-existing deadlock.
func TestR5APIKeyUsageLifecycleCascade(t *testing.T) {
	r5StageRequirePG(t)
	for _, mode := range []string{"key_delete", "user_freeze", "guarded_user_delete", "raw_user_delete", "offboard", "minimal_tenant_delete"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			owner := f.employee.ID
			tenant := f.tenant.ID
			if mode == "guarded_user_delete" || mode == "raw_user_delete" {
				u := &models.User{TenantID: tenant, Email: uuid.NewString() + "@usage.test", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic-unused"}
				must(t, f.st.CreateUser(ctx, u))
				owner = u.ID // no mailbox or retained historical sender identity
			}
			key := &models.TenantAPIKey{TenantID: tenant, OwnerUserID: &owner, KeyHash: company.Hash(uuid.NewString()), KeyPrefix: "fixture", Scopes: []string{"send:read"}}
			if mode == "minimal_tenant_delete" {
				other := &models.Tenant{Name: "Usage-only tenant cascade", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, other))
				key.TenantID, key.OwnerUserID = other.ID, nil
			}
			must(t, f.st.CreateAPIKey(ctx, key))
			must(t, f.st.TouchAPIKey(ctx, key.ID, "192.0.2.64"))
			actor := r5OffboardingActor(t, f)
			switch mode {
			case "key_delete":
				must(t, f.st.DeleteAPIKey(ctx, key.ID))
			case "user_freeze":
				inactive := false
				_, err := f.st.UpdateUserGuarded(ctx, actor, tenant, owner, models.UserAdminPatch{IsActive: &inactive})
				must(t, err)
			case "guarded_user_delete":
				must(t, f.st.DeleteUserGuarded(ctx, actor, tenant, owner))
			case "raw_user_delete":
				must(t, f.st.DeleteUser(ctx, owner))
			case "offboard":
				plan := r5OffboardingPreview(t, f, actor)
				_, err := f.st.ExecuteOffboarding(ctx, actor, owner, plan.ID)
				must(t, err)
			case "minimal_tenant_delete":
				must(t, f.st.DeleteTenant(ctx, key.TenantID))
			}
			must(t, f.st.TouchAPIKey(ctx, key.ID, "2001:db8::64"))
			var rows int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_api_keys WHERE id=$1)+(SELECT count(*) FROM tenant_api_key_usage WHERE api_key_id=$1)`, key.ID).Scan(&rows))
			if rows != 0 {
				t.Fatal("lifecycle deletion or its late Touch left/resurrected authority or usage")
			}
		})
	}
}

func TestR5APIKeyUsageMetadataStoreAndWire(t *testing.T) {
	r5StageRequirePG(t)
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, _ := r5LegacyKey(t, f)
	other := &models.TenantAPIKey{TenantID: f.tenant.ID, OwnerUserID: &f.other.ID, KeyHash: company.Hash(uuid.NewString()), KeyPrefix: "fixture", Scopes: key.Scopes}
	must(t, f.st.CreateAPIKey(ctx, other))
	foreign := &models.Tenant{Name: "Foreign usage metadata", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, foreign))
	foreignKey := &models.TenantAPIKey{TenantID: foreign.ID, KeyHash: company.Hash(uuid.NewString()), KeyPrefix: "fixture", Scopes: key.Scopes}
	must(t, f.st.CreateAPIKey(ctx, foreignKey))
	// Keep stale legacy display data present, so separately COALESCEing the IP
	// would be caught both before first use and after the empty-IP observation.
	_, err := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET last_used_at='2024-01-02T03:04:05Z',last_used_ip='192.0.2.200' WHERE id=$1`, key.ID)
	must(t, err)
	obj := testutil.NewMemoryObjectStore()
	h := r5TouchRouter(f, f.st, r5StageService(f, 1), obj)
	verify := func(at *time.Time, ip string) {
		t.Helper()
		check := func(k *models.TenantAPIKey) {
			t.Helper()
			if k == nil || k.ID != key.ID || k.TenantID != f.tenant.ID || k.KeyHash != "" || (k.LastUsedAt == nil) != (at == nil) || (at != nil && !k.LastUsedAt.Equal(*at)) || (ip == "" && k.LastUsedIP != nil) || (ip != "" && (k.LastUsedIP == nil || *k.LastUsedIP != ip)) {
				t.Fatal("store/wire metadata lost identity, NULL, timestamp/IP pairing or hash privacy")
			}
		}
		got, err := f.st.GetAPIKey(ctx, key.ID)
		must(t, err)
		check(got)
		for _, ownerOnly := range []bool{false, true} {
			var items []*models.TenantAPIKey
			if ownerOnly {
				items, err = f.st.ListAPIKeysByOwner(ctx, f.tenant.ID, f.employee.ID)
			} else {
				items, err = f.st.ListAPIKeys(ctx, f.tenant.ID)
			}
			must(t, err)
			want := 2
			if ownerOnly {
				want = 1
			}
			if len(items) != want {
				t.Fatal("metadata list widened tenant/owner scope")
			}
			found := false
			for _, item := range items {
				if item.ID == foreignKey.ID {
					t.Fatal("foreign key leaked into list")
				}
				if item.ID == key.ID {
					check(item)
					found = true
				}
			}
			if !found {
				t.Fatal("metadata list omitted current key")
			}
			user := f.admin
			if ownerOnly {
				user = f.employee
			}
			req := httptest.NewRequest("GET", "/api/v1/keys", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+r3Token(t, user))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("formal metadata list HTTP=%d", w.Code)
			}
			for _, private := range []string{"key_hash", key.KeyHash, other.KeyHash, "tm_content_" + key.ID.String()} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("metadata HTTP exposed credential material")
				}
			}
			var envelope struct{ Data []*models.TenantAPIKey }
			must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			if len(envelope.Data) != want {
				t.Fatal("wire metadata list widened tenant/owner scope")
			}
			found = false
			for _, item := range envelope.Data {
				if item.ID == key.ID {
					check(item)
					found = true
				}
			}
			if !found {
				t.Fatal("wire metadata list omitted current key")
			}
		}
	}
	verify(nil, "") // existing NULL usage pair must not borrow legacy metadata
	for _, ip := range []string{"192.0.2.52", "2001:db8::52", ""} {
		previous, err := f.st.GetAPIKey(ctx, key.ID)
		must(t, err)
		if previous.LastUsedAt != nil {
			must(t, r5TouchAwait(ctx, func() (bool, error) {
				var now time.Time
				err := f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
				return now.After(*previous.LastUsedAt), err
			}))
		}
		must(t, f.st.TouchAPIKey(ctx, key.ID, ip))
		current, err := f.st.GetAPIKey(ctx, key.ID)
		must(t, err)
		if current.LastUsedAt == nil {
			t.Fatal("real Touch did not publish its observation")
		}
		verify(current.LastUsedAt, ip)
	}
	// Explicitly simulate missing usage: display falls back as one legacy pair,
	// while public Touch remains UPDATE-only and never repairs it implicitly.
	_, err = f.pool.Exec(ctx, `DELETE FROM tenant_api_key_usage WHERE api_key_id=$1`, key.ID)
	must(t, err)
	must(t, f.st.TouchAPIKey(ctx, key.ID, "192.0.2.99"))
	legacy := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	verify(&legacy, "192.0.2.200")
}
