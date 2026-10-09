package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type reconcileCoverageFixture struct {
	*companyFixture
	actor authz.Actor
	job   *models.OutboundJob
}

func newReconcileCoverageFixture(t *testing.T, addresses []string) *reconcileCoverageFixture {
	t.Helper()
	ctx := context.Background()
	f := seedCompany(t)
	_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
	must(t, err)
	a := f.a
	a.Role, a.IsSuperAdmin = models.RoleSuperAdmin, true
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: addresses, To: addresses, Subject: "Ledger coverage fixture", TextBody: "Controlled synthetic content", State: models.OutboundFailed}
	must(t, f.st.CreateOutboundJob(ctx, j))
	_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='legacy:review-required',last_error='Original uncertain evidence' WHERE id=$1`, j.ID)
	must(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain',diagnostic='Original recipient evidence',attempts=2 WHERE job_id=$1`, j.ID)
	must(t, err)
	j, err = f.st.GetOutboundJob(ctx, j.ID)
	must(t, err)
	return &reconcileCoverageFixture{companyFixture: f, actor: a, job: j}
}

func (f *reconcileCoverageFixture) reconcile(ctx context.Context, results []company.Recipient) error {
	return f.st.ReconcileOutbound(ctx, f.actor, f.job.ID, f.job.UpdatedAt, results, "Controlled operator evidence confirms downstream outcome")
}

func reconcileCoverageWantKind(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	if v, ok := app.As(err); !ok || v.Kind != kind {
		t.Fatalf("got %v, want %s", err, kind)
	}
}

func TestR5ReconciliationRequiresCompleteLedger(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("reconciliation coverage acceptance requires TABMAIL_TEST_DB_DSN; no skip")
	}
	ctx := context.Background()
	const first, second = "first@fixture.test", "second@fixture.test"
	for _, fault := range []string{"missing-recipient", "extra-recipient", "same-count-wrong-recipient", "empty-envelope", "empty-address", "oversized-complete-ledger", "all-rows-missing"} {
		t.Run(fault, func(t *testing.T) {
			f := newReconcileCoverageFixture(t, []string{first, second})
			var err error
			switch fault {
			case "missing-recipient":
				_, err = f.pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1 AND address=$2`, f.job.ID, second)
			case "extra-recipient":
				_, err = f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,'unintended@fixture.test','accepted')`, f.tenant.ID, f.job.ID)
			case "same-count-wrong-recipient":
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET address='replacement@fixture.test',state='accepted' WHERE job_id=$1 AND address=$2`, f.job.ID, second)
			case "empty-envelope":
				_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET rcpt_to='{}'::text[] WHERE id=$1`, f.job.ID)
			case "empty-address":
				_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET rcpt_to=$2 WHERE id=$1`, f.job.ID, []string{first, ""})
				must(t, err)
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET address='',state='accepted' WHERE job_id=$1 AND address=$2`, f.job.ID, second)
			case "oversized-complete-ledger":
				addresses := []string{first, second}
				for i := 0; i < 49; i++ {
					address := fmt.Sprintf("overflow-%02d@fixture.test", i)
					addresses = append(addresses, address)
					_, err = f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,$3,'accepted')`, f.tenant.ID, f.job.ID, address)
					must(t, err)
				}
				_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET rcpt_to=$2 WHERE id=$1`, f.job.ID, addresses)
			case "all-rows-missing":
				_, err = f.pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, f.job.ID)
			}
			must(t, err)
			f.job, err = f.st.GetOutboundJob(ctx, f.job.ID)
			must(t, err)
			before := r5RecoveryParentSnapshot(t, f.companyFixture)
			err = f.reconcile(ctx, []company.Recipient{{Address: first, State: "accepted"}})
			reconcileCoverageWantKind(t, err, app.KindConflict)
			if after := r5RecoveryParentSnapshot(t, f.companyFixture); after != before {
				t.Fatal("incomplete ledger changed recipient evidence, job marker/state, audit or outbox")
			}
		})
	}
	t.Run("locked-unselected-recipient-fails-before-any-write", func(t *testing.T) {
		f := newReconcileCoverageFixture(t, []string{first, second})
		hold, err := f.pool.Begin(ctx)
		must(t, err)
		defer hold.Rollback(ctx)
		_, err = hold.Exec(ctx, `SELECT address FROM outbound_recipients WHERE job_id=$1 AND address=$2 FOR UPDATE`, f.job.ID, second)
		must(t, err)
		before := r5RecoveryParentSnapshot(t, f.companyFixture)
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		err = f.reconcile(bounded, []company.Recipient{{Address: first, State: "accepted"}})
		reconcileCoverageWantKind(t, err, app.KindConflict)
		if after := r5RecoveryParentSnapshot(t, f.companyFixture); after != before {
			t.Fatal("busy complete ledger changed unblocked recipient or job evidence")
		}
		must(t, hold.Rollback(ctx))
		must(t, f.reconcile(ctx, []company.Recipient{{Address: first, State: "accepted"}}))
	})
	for _, control := range []string{"all-accepted", "partial-then-complete", "temporary-is-explicit-retry", "permanent-is-explicit-retry", "quoted-addresses", "repeated-envelope-address", "exact-fifty-recipient-limit"} {
		t.Run(control, func(t *testing.T) {
			addresses := []string{first, second}
			wantRows := 2
			if control == "quoted-addresses" {
				addresses = []string{`"quoted@local"@fixture.test`, `"quoted space"@fixture.test`}
			} else if control == "repeated-envelope-address" {
				// Legacy envelopes can repeat a canonical address. The ledger and
				// inspection contract have always represented its one identity.
				addresses = []string{first, second, first}
			} else if control == "exact-fifty-recipient-limit" {
				for i := 0; i < 48; i++ {
					addresses = append(addresses, fmt.Sprintf("confirmed-%02d@fixture.test", i))
				}
				wantRows = 50
			}
			f := newReconcileCoverageFixture(t, addresses)
			results := []company.Recipient{{Address: addresses[0], State: "accepted"}, {Address: addresses[1], State: "accepted"}}
			if control == "exact-fifty-recipient-limit" {
				for _, address := range addresses[2:] {
					results = append(results, company.Recipient{Address: address, State: "accepted"})
				}
			}
			want := models.OutboundSent
			switch control {
			case "partial-then-complete":
				must(t, f.reconcile(ctx, results[:1]))
				current, err := f.st.GetOutboundJob(ctx, f.job.ID)
				must(t, err)
				if current.InFlightDomain != f.job.InFlightDomain || current.State != f.job.State || current.LastError != f.job.LastError {
					t.Fatal("partial reconciliation erased unresolved evidence")
				}
				f.job = current
				results = results[1:]
			case "temporary-is-explicit-retry":
				results[1].State, want = "temporary", models.OutboundFailed
			case "permanent-is-explicit-retry":
				results[1].State, want = "permanent", models.OutboundFailed
			}
			must(t, f.reconcile(ctx, results))
			current, err := f.st.GetOutboundJob(ctx, f.job.ID)
			must(t, err)
			if current.State != want || current.InFlightDomain != "" {
				t.Fatalf("complete confirmation state=%s marker=%q, want %s/empty", current.State, current.InFlightDomain, want)
			}
			rows, err := f.st.ListOutboundRecipients(ctx, f.tenant.ID, f.job.ID)
			must(t, err)
			if len(rows) != wantRows {
				t.Fatalf("canonical recipient cardinality=%d, want %d", len(rows), wantRows)
			}
			for _, row := range rows {
				if row.Attempts != 2 {
					t.Fatal("operator confirmation invented an SMTP attempt")
				}
			}
		})
	}
	for _, failure := range []string{"stale-version", "unknown-selected-recipient", "duplicate-selected-recipient", "required-audit-failure"} {
		t.Run(failure, func(t *testing.T) {
			f := newReconcileCoverageFixture(t, []string{first, second})
			results := []company.Recipient{{Address: first, State: "accepted"}, {Address: second, State: "accepted"}}
			want := app.KindConflict
			switch failure {
			case "stale-version":
				f.job.UpdatedAt = f.job.UpdatedAt.Add(-time.Second)
			case "unknown-selected-recipient":
				results[1].Address = "unknown@fixture.test"
			case "duplicate-selected-recipient":
				results[1].Address, want = first, app.KindBadRequest
			case "required-audit-failure":
				_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT reconcile_coverage_audit_fault CHECK(action<>'outbound.reconcile') NOT VALID`)
				must(t, err)
			}
			before := r5RecoveryParentSnapshot(t, f.companyFixture)
			err := f.reconcile(ctx, results)
			if failure == "required-audit-failure" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "reconcile_coverage_audit_fault" {
					t.Fatalf("reconciliation did not reach actual required audit fault: %v", err)
				}
			} else {
				reconcileCoverageWantKind(t, err, want)
			}
			if after := r5RecoveryParentSnapshot(t, f.companyFixture); after != before {
				t.Fatal("failed reconciliation committed partial effects")
			}
		})
	}
}
