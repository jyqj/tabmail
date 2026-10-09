package postgres_test

// Secure target-boundary regressions on production PgStore entry points.
// Fixture SQL creates deterministic state and blockers only. Expiry uses the
// PostgreSQL clock after pg_blocking_pids proves the audit wait; no sleeps.
// Recovery inspection/reconciliation are not ordinary content authorization.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

func r5RecoveryTargetDeadline(t *testing.T, f *companyFixture, ctx context.Context) time.Time {
	t.Helper()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, f.personal.ID).Scan(&deadline))
	return deadline
}

func r5RecoveryTargetConflict(t *testing.T, err error) {
	t.Helper()
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindConflict {
		t.Fatalf("R5_RECOVERY_TARGET_STALE: unavailable fixed destination must reject retry with conflict; got %v", err)
	}
}

func TestR5RecoveryRetryRejectsExpiryAfterAuditWait(t *testing.T) {
	f := seedCompany(t)
	c := r5RecoveryParentFixture(t, f, "retry")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	deadline := r5RecoveryTargetDeadline(t, f, ctx)
	before := r5RecoveryParentSnapshot(t, f)
	hold := r5AuditedHold(t, f, ctx, c)
	done := make(chan error, 1)
	go func() { done <- c.run(ctx) }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
	r5AwaitSentDeadline(t, f, ctx, writer, deadline)
	must(t, hold.Rollback(ctx))
	r5RecoveryTargetConflict(t, r5ConcurrentResult(t, ctx, done))
	if r5RecoveryParentSnapshot(t, f) != before {
		t.Fatal("expired retry changed held outcome/job, audit or outbox")
	}
}

// A legal implementation may either lock the target before audit (retry wins)
// or revalidate after a completed target writer (writer wins). The test refuses
// stale commit, not one particular SQL lock mechanism. NOWAIT is a probe only;
// no test controller waits while it owns the audit blocker.
func TestR5RecoveryRetryOrdersDestinationChanges(t *testing.T) {
	for _, kind := range []string{"address", "recreated_mailbox", "zone", "tenant", "zone_unverified", "mx_unverified"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5RecoveryParentFixture(t, f, "retry")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			replacementZone := uuid.Nil
			replacementTenant := uuid.Nil
			if kind == "zone" {
				z := &models.DomainZone{TenantID: f.tenant.ID, Domain: "alternate.fixture.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, z))
				replacementZone = z.ID
			}
			if kind == "tenant" {
				tenant := &models.Tenant{Name: "unrelated recovery company", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, tenant))
				replacementTenant = tenant.ID
				z := &models.DomainZone{TenantID: tenant.ID, Domain: "foreign-recovery.fixture.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, z))
				replacementZone = z.ID
			}
			before := r5RecoveryParentSnapshot(t, f)
			hold := r5AuditedHold(t, f, ctx, c)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			change, e := f.pool.Begin(ctx)
			must(t, e)
			defer change.Rollback(context.Background())
			lockSQL := `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`
			lockID := f.personal.ID
			if kind == "zone_unverified" || kind == "mx_unverified" {
				lockSQL = `SELECT id FROM domain_zones WHERE id=$1 FOR UPDATE NOWAIT`
				lockID = f.zone.ID
			}
			_, e = change.Exec(ctx, lockSQL, lockID)
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "55P03" {
				must(t, change.Rollback(ctx))
				must(t, hold.Rollback(ctx))
				r5AwaitOperation(t, ctx, done)
				t.Log("retry protected destination policy before audit; competing writer cannot commit first")
				return
			}
			must(t, e)
			switch kind {
			case "address":
				_, e = change.Exec(ctx, `UPDATE mailboxes SET full_address='changed@company.test' WHERE id=$1`, f.personal.ID)
			case "recreated_mailbox":
				_, e = change.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, f.personal.ID)
				must(t, e)
				_, e = change.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,full_address,resolved_domain,access_mode,mailbox_kind) VALUES($1,$2,$3,'employee',$4,'company.test','token','shared')`, uuid.New(), f.tenant.ID, f.zone.ID, f.personal.FullAddress)
			case "zone":
				_, e = change.Exec(ctx, `UPDATE mailboxes SET zone_id=$2 WHERE id=$1`, f.personal.ID, replacementZone)
			case "tenant":
				_, e = change.Exec(ctx, `UPDATE mailboxes SET tenant_id=$2,zone_id=$3,owner_user_id=NULL,mailbox_kind='shared',
                  access_mode='token',password_hash=NULL,route_id=NULL,resolved_domain='foreign-recovery.fixture.test',
                  full_address='employee@foreign-recovery.fixture.test' WHERE id=$1`, f.personal.ID, replacementTenant, replacementZone)
			case "zone_unverified":
				_, e = change.Exec(ctx, `UPDATE domain_zones SET is_verified=false WHERE id=$1`, f.zone.ID)
			case "mx_unverified":
				_, e = change.Exec(ctx, `UPDATE domain_zones SET mx_verified=false WHERE id=$1`, f.zone.ID)
			}
			must(t, e)
			must(t, change.Commit(ctx))
			must(t, hold.Rollback(ctx))
			r5RecoveryTargetConflict(t, r5ConcurrentResult(t, ctx, done))
			if r5RecoveryParentSnapshot(t, f) != before {
				t.Fatal("changed destination left retry, audit or outbox effects")
			}
		})
	}
}

func TestR5IngressDeliveryRejectsTargetExpiryAfterAuditWait(t *testing.T) {
	f := seedCompany(t)
	claim, message := r5ClaimIngress(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	deadline := r5RecoveryTargetDeadline(t, f, ctx)
	before := r5RecoveryParentSnapshot(t, f)
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, e)
	type result struct {
		delivered bool
		err       error
	}
	done := make(chan result, 1)
	go func() { ok, e := f.st.DeliverIngress(ctx, claim, message, 100, 100); done <- result{ok, e} }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
	r5AwaitSentDeadline(t, f, ctx, writer, deadline)
	must(t, hold.Rollback(ctx))
	select {
	case r := <-done:
		if r.delivered || !errors.Is(r.err, store.ErrIngressQuota) {
			t.Fatalf("R5_INGRESS_TARGET_STALE: mailbox expired during audit; expected no delivery and target quota rejection, got delivered=%v error=%v", r.delivered, r.err)
		}
	case <-ctx.Done():
		t.Fatal("delivery did not finish after audit release")
	}
	if r5RecoveryParentSnapshot(t, f) != before {
		t.Fatal("target expiry changed durable receipt progress")
	}
	r5IngressEffects(t, f, claim, 0)
}

func TestR5IngressDeliveryOrdersZoneRevocationAfterAuditWait(t *testing.T) {
	for _, column := range []string{"is_verified", "mx_verified"} {
		t.Run(column, func(t *testing.T) {
			f := seedCompany(t)
			claim, message := r5ClaimIngress(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			must(t, e)
			type result struct {
				delivered bool
				err       error
			}
			done := make(chan result, 1)
			go func() { ok, e := f.st.DeliverIngress(ctx, claim, message, 100, 100); done <- result{ok, e} }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			change, e := f.pool.Begin(ctx)
			must(t, e)
			defer change.Rollback(context.Background())
			// Non-key update is the real verification-policy writer. KEY SHARE from a
			// message FK does not serialize it; SET LOCAL prevents controller deadlock.
			_, e = change.Exec(ctx, `SET LOCAL lock_timeout='250ms'`)
			must(t, e)
			_, e = change.Exec(ctx, `UPDATE domain_zones SET `+column+`=false WHERE id=$1`, f.zone.ID)
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "55P03" {
				must(t, change.Rollback(ctx))
				must(t, hold.Rollback(ctx))
				select {
				case r := <-done:
					if !r.delivered || r.err != nil {
						t.Fatalf("protected delivery should commit first: %+v", r)
					}
				case <-ctx.Done():
					t.Fatal("delivery stuck")
				}
				r5IngressEffects(t, f, claim, 1)
				return
			}
			must(t, e)
			must(t, change.Commit(ctx))
			must(t, hold.Rollback(ctx))
			select {
			case r := <-done:
				if r.delivered || !errors.Is(r.err, store.ErrIngressQuota) {
					t.Fatalf("R5_INGRESS_ZONE_STALE: completed verification revoke preceded commit: delivered=%v error=%v", r.delivered, r.err)
				}
			case <-ctx.Done():
				t.Fatal("delivery stuck")
			}
			r5IngressEffects(t, f, claim, 0)
		})
	}
}

// Missing ordinary read/send rights and expired/deleted destinations must not
// destroy accepted held bytes or block a CURRENT platform operator's inspect.
func TestR5RecoveryInspectRetainsUnavailableDestinationOriginal(t *testing.T) {
	for _, kind := range []string{"expired", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5RecoveryParentFixture(t, f, "inspect")
			ctx := context.Background()
			var job uuid.UUID
			var raw string
			must(t, f.pool.QueryRow(ctx, `SELECT id,raw_object_key FROM ingest_jobs`).Scan(&job, &raw))
			if kind == "expired" {
				_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, f.personal.ID)
				must(t, e)
			} else {
				_, e := f.pool.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, f.personal.ID)
				must(t, e)
			}
			must(t, c.run(ctx))
			refs, e := f.st.CountRawObjectReferences(ctx, raw)
			must(t, e)
			if refs < 1 {
				t.Fatal("operator inspection lost held original reference")
			}
			targets, e := f.st.ListIngressTargets(ctx, job)
			must(t, e)
			if len(targets) != 1 || targets[0].MailboxID != f.personal.ID || targets[0].State != "held" {
				t.Fatal("inspection retargeted or resurrected the held destination")
			}
		})
	}
}

func TestR5RecoveryReconcileDoesNotRequireLiveSender(t *testing.T) {
	f := seedCompany(t)
	c := r5RecoveryParentFixture(t, f, "reconcile")
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, f.personal.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
	must(t, e)
	must(t, c.run(ctx))
	var state, marker string
	must(t, f.pool.QueryRow(ctx, `SELECT state,in_flight_domain FROM outbound_jobs`).Scan(&state, &marker))
	if state != "sent" || marker != "" {
		t.Fatal("confirmed next-hop evidence not reconciled after sender became unavailable")
	}
}

func TestR5RecoveryRetryDoesNotRequireReadOrSendPolicy(t *testing.T) {
	f := seedCompany(t)
	c := r5RecoveryParentFixture(t, f, "retry")
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET send_policy='disabled' WHERE id=$1`, f.personal.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
	must(t, e)
	// The current super-admin has no mailbox grant. Retrying received bytes is
	// neither ordinary content read nor a new outbound send-as action.
	must(t, c.run(ctx))
	var state string
	must(t, f.pool.QueryRow(ctx, `SELECT state FROM ingest_recipient_outcomes`).Scan(&state))
	if state != "pending" {
		t.Fatal("valid fixed receive destination did not enter pending")
	}
}

// Busy-target HTTP contract, not an environment timeout. A baseline waiting
// UPDATE is released only after pg_blocking_pids proves it; a NOWAIT response
// is collected directly. In either branch the controller releases its exact
// blocker before waiting for request completion.
func TestR5RecoveryRetryHTTPDestinationBusyConflict(t *testing.T) {
	for _, kind := range []string{"target_row", "zone_row"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			r5RecoveryParentFixture(t, f, "retry")
			f.admin.Role = models.RoleSuperAdmin
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			const raw = "From: sender@fixture.test\r\nTo: employee@company.test\r\nSubject: controlled recovery\r\n\r\nsynthetic held original\r\n"
			var job uuid.UUID
			var key string
			var version time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE ingest_jobs SET raw_sha256=$1,raw_size=$2 RETURNING id,raw_object_key,updated_at`, company.Hash(raw), len(raw)).Scan(&job, &key, &version))
			objects := testutil.NewMemoryObjectStore()
			must(t, objects.Put(ctx, key, strings.NewReader(raw), int64(len(raw))))
			svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
			svc.SetObjectStore(objects) // Never start a sending worker.
			server := httptest.NewServer(companyRouter(t, f, objects, svc))
			defer server.Close()
			var before string
			snapshot := func() string {
				var result string
				must(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
    'job',(SELECT to_jsonb(j) FROM ingest_jobs j WHERE id=$1),
    'targets',(SELECT jsonb_agg(to_jsonb(r) ORDER BY mailbox_id) FROM ingest_recipient_outcomes r WHERE job_id=$1),
    'retry_audit',(SELECT count(*) FROM audit_log WHERE action='ingress.retry'),
    'retry_outbox',(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='ingress.retry'))::text`, job).Scan(&result))
				return result
			}
			before = snapshot()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			switch kind {
			case "target_row":
				_, e = hold.Exec(ctx, `SELECT mailbox_id FROM ingest_recipient_outcomes WHERE job_id=$1 AND mailbox_id=$2 FOR UPDATE`, job, f.personal.ID)
			case "zone_row":
				_, e = hold.Exec(ctx, `SELECT id FROM domain_zones WHERE id=$1 FOR UPDATE`, f.zone.ID)
			}
			must(t, e)
			payload, e := json.Marshal(map[string]any{"updated_at": version, "targets": []uuid.UUID{f.personal.ID}, "reason": "Controlled isolated busy-target HTTP verification"})
			must(t, e)
			req, e := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/company/recovery/"+job.String()+"/retry", bytes.NewReader(payload))
			must(t, e)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+r3Token(t, f.admin))
			type response struct {
				status int
				body   []byte
				err    error
			}
			done := make(chan response, 1)
			go func() {
				res, e := server.Client().Do(req)
				if e != nil {
					done <- response{err: e}
					return
				}
				defer res.Body.Close()
				body, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
				done <- response{status: res.StatusCode, body: body, err: e}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			var observed *response
			waiting := false
			for observed == nil && !waiting {
				select {
				case result := <-done:
					observed = &result
				default:
				}
				if observed != nil {
					break
				}
				must(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)))`, int32(hold.Conn().PgConn().PID())).Scan(&waiting))
				if waiting {
					break
				}
				select {
				case result := <-done:
					observed = &result
				case <-ctx.Done():
					t.Fatal("no response or actual destination waiter observed; fixture failure, not target refusal")
				case <-ticker.C:
				}
			}
			must(t, hold.Rollback(ctx))
			if observed == nil {
				select {
				case result := <-done:
					observed = &result
				case <-ctx.Done():
					t.Fatal("request did not finish after exact destination blocker was released")
				}
			}
			must(t, observed.err)
			if observed.status != http.StatusConflict {
				t.Fatalf("R5_RECOVERY_TARGET_BUSY_HTTP: target/zone contention must return409, got%d actual_wait=%v body=%s", observed.status, waiting, observed.body)
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			must(t, json.Unmarshal(observed.body, &envelope))
			if envelope.Error.Code != "CONFLICT" {
				t.Fatalf("wrong busy error code: %s", observed.body)
			}
			if snapshot() != before {
				t.Fatal("busy-target refusal left job, target or retry audit/outbox changes")
			}
			// Inspect is independently required before verification and can commit its
			// own audit; that legitimate evidence is intentionally excluded above.
			var inspected int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='ingress.inspect'`).Scan(&inspected))
			if inspected != 1 {
				t.Fatalf("expected real successful original inspection, got %d", inspected)
			}
			refs, e := f.st.CountRawObjectReferences(ctx, key)
			must(t, e)
			if refs < 1 {
				t.Fatal("busy-target refusal lost held original reference")
			}
		})
	}
}
