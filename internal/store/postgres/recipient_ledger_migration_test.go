package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"os"
	"strings"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"testing"
	"time"
)

func TestAR04LegacyRecipientMigrationPreservesEvidence(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	// This is an isolated fixture database, not a downgrade of an existing
	// deployment. Recreate the v13 flag constraint shape and legacy queue rows,
	// then execute the actual, unchanged v14 Up SQL inside its own transaction.
	_, err := f.pool.Exec(ctx, `ALTER TABLE outbound_jobs DROP CONSTRAINT outbound_jobs_recipient_ledger_required; ALTER TABLE outbound_jobs ALTER COLUMN recipient_ledger SET DEFAULT false`)
	must(t, err)
	type fixture struct {
		name      string
		state     models.OutboundState
		attempts  int
		domains   []string
		marker    string
		addresses []string
		wantState models.OutboundState
		want      []string
		held      bool
		job       *models.OutboundJob
	}
	cases := []fixture{
		{name: "unattempted", state: models.OutboundPending, addresses: []string{"alice@a.test", "bob@b.test"}, wantState: models.OutboundPending, want: []string{delivery.Pending, delivery.Pending}},
		{name: "partial-retry", state: models.OutboundRetry, attempts: 1, domains: []string{"a.test"}, addresses: []string{"alice@a.test", "bob@b.test"}, wantState: models.OutboundFailed, want: []string{delivery.Accepted, delivery.Uncertain}, held: true},
		{name: "interrupted", state: models.OutboundProcessing, attempts: 1, domains: []string{"a.test"}, marker: "b.test", addresses: []string{"alice@a.test", "bob@b.test"}, wantState: models.OutboundFailed, want: []string{delivery.Accepted, delivery.Uncertain}, held: true},
		{name: "accepted", state: models.OutboundSent, attempts: 1, addresses: []string{"alice@a.test", "bob@b.test"}, wantState: models.OutboundSent, want: []string{delivery.Accepted, delivery.Accepted}},
		{name: "dead-unknown", state: models.OutboundDead, attempts: 2, addresses: []string{"alice@a.test"}, wantState: models.OutboundDead, want: []string{delivery.Uncertain}, held: true},
		{name: "cancelled-unsent", state: models.OutboundCancelled, addresses: []string{"alice@a.test"}, wantState: models.OutboundCancelled, want: []string{delivery.Pending}},
		{name: "empty-history", state: models.OutboundPending, addresses: []string{}, wantState: models.OutboundFailed, held: true},
	}
	for i := range cases {
		c := &cases[i]
		j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: c.addresses, To: c.addresses, Subject: c.name, State: c.state}
		must(t, f.st.CreateOutboundJob(ctx, j))
		c.job = j
		_, err = f.pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, j.ID)
		must(t, err)
		_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET recipient_ledger=false,attempts=$2,delivered_domains=COALESCE($3::text[],'{}'),in_flight_domain=$4,last_error='historical diagnostic',next_attempt_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, j.ID, c.attempts, c.domains, c.marker)
		must(t, err)
	}
	migration, err := os.ReadFile("migrations/00014_recipient_ledger_required.sql")
	must(t, err)
	up := strings.Split(string(migration), "-- +goose Down")[0]
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, up)
	must(t, err)
	must(t, tx.Commit(ctx))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j, err := f.st.GetOutboundJob(ctx, c.job.ID)
			must(t, err)
			if !j.RecipientLedger || j.State != c.wantState || (j.InFlightDomain != "") != c.held || j.Attempts != c.attempts || j.DeliveryToken != nil || j.LeaseUntil != nil {
				t.Fatalf("history/hold changed incorrectly: %+v", j)
			}
			if c.marker != "" && j.InFlightDomain != c.marker {
				t.Fatal("original in-flight evidence lost")
			}
			if !strings.Contains(j.LastError, "historical diagnostic") {
				t.Fatal("original diagnostic lost")
			}
			rows, err := f.st.ListOutboundRecipients(ctx, f.tenant.ID, j.ID)
			must(t, err)
			if len(rows) != len(c.want) {
				t.Fatalf("rows=%+v want=%v", rows, c.want)
			}
			for i, r := range rows {
				if r.State != c.want[i] || r.Attempts != 0 {
					t.Fatalf("invented recipient evidence: %+v", r)
				}
			}
		})
	}
	claimed, err := f.st.ClaimOutboundJobs(ctx, time.Now(), 50)
	must(t, err)
	if len(claimed) != 1 || claimed[0].ID != cases[0].job.ID {
		t.Fatalf("migration made ambiguous history retryable: %+v", claimed)
	}
	_, err = f.pool.Exec(ctx, `UPDATE outbound_jobs SET recipient_ledger=false WHERE id=$1`, cases[0].job.ID)
	if err == nil {
		t.Fatal("old writer can reintroduce a legacy job")
	}
	// A defaulted direct fixture row also receives the required ledger flag.
	var flag bool
	newID := uuid.New()
	must(t, f.pool.QueryRow(ctx, `INSERT INTO outbound_jobs(id,tenant_id,zone_id,mail_from,rcpt_to) VALUES($1,$2,$3,'test@example.test',ARRAY['client@example.test']) RETURNING recipient_ledger`, newID, f.tenant.ID, f.zone.ID).Scan(&flag))
	if !flag {
		t.Fatal("new row default is legacy")
	}
}
