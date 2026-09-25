package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"testing"
	"testing/fstest"
	"time"
)

func TestP0PostgresOwnerGrantAndPermanentRetention(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}
	must(t, st.CreateTenant(ctx, tenant))
	other := &models.Tenant{Name: "Other", PlanID: tenant.PlanID}
	must(t, st.CreateTenant(ctx, other))
	user := &models.User{TenantID: tenant.ID, Email: "a@company.test", PasswordHash: "test", Role: models.RoleUser, IsActive: true}
	must(t, st.CreateUser(ctx, user))
	outsider := &models.User{TenantID: other.ID, Email: "b@other.test", PasswordHash: "test", Role: models.RoleUser, IsActive: true}
	must(t, st.CreateUser(ctx, outsider))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "company.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	mb := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, OwnerUserID: &user.ID, FullAddress: user.Email, LocalPart: "a", ResolvedDomain: zone.Domain, AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, mb))
	got, err := st.ForTenant(tenant.ID).GetMailbox(ctx, mb.ID)
	must(t, err)
	if got.OwnerUserID == nil || *got.OwnerUserID != user.ID {
		t.Fatal("owner codec lost")
	}
	bad := *mb
	bad.ID = uuid.New()
	bad.FullAddress = "bad@company.test"
	bad.OwnerUserID = &outsider.ID
	if st.CreateMailbox(ctx, &bad) == nil {
		t.Fatal("cross-company owner permitted")
	}
	g := &models.MailboxGrant{TenantID: tenant.ID, MailboxID: mb.ID, UserID: user.ID, CanSend: true}
	must(t, st.SetMailboxGrant(ctx, g))
	g.UserID = outsider.ID
	if st.SetMailboxGrant(ctx, g) == nil {
		t.Fatal("cross-company grant permitted")
	}
	g.UserID = user.ID
	g.CanOrganize = true
	if st.SetMailboxGrant(ctx, g) == nil {
		t.Fatal("organize without read permitted")
	}
	m := &models.Message{TenantID: tenant.ID, ZoneID: zone.ID, MailboxID: mb.ID, Sender: "x@sender.test", Recipients: []string{mb.FullAddress}, RawObjectKey: "test-original", ExpiresAt: nil}
	must(t, st.CreateMessage(ctx, m))
	stored, err := st.GetMessage(ctx, m.ID)
	must(t, err)
	if stored.ExpiresAt != nil {
		t.Fatal("NULL expiry codec lost")
	}
	// Ownership protects previously expiring messages, not just newly created mail.
	_, err = pool.Exec(ctx, `UPDATE messages SET expires_at=now()-interval '1 hour' WHERE id=$1`, m.ID)
	must(t, err)
	n, _, err := st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
	must(t, err)
	if n != 0 {
		t.Fatal("owner's existing mail expired")
	}
	_, err = pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=NULL,mailbox_kind='legacy' WHERE id=$1`, mb.ID)
	must(t, err)
	n, _, err = st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
	must(t, err)
	if n != 1 {
		t.Fatal("legacy temporary TTL no longer expires")
	}
}
func TestP0PostgresOutboundDomainFencingAndUncertainty(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Send", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "sender.test"}
	must(t, st.CreateZone(ctx, zone))
	job := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, MailFrom: "me@sender.test", RcptTo: []string{"one@a.test", "two@b.test"}, Subject: "test", MaxAttempts: 5}
	must(t, st.CreateOutboundJob(ctx, job))
	jobs, err := st.ClaimOutboundJobs(ctx, time.Now(), 100)
	must(t, err)
	if len(jobs) != 1 {
		t.Fatal("no claim")
	}
	first := jobs[0]
	must(t, st.BeginOutboundDomain(ctx, job.ID, first.DeliveryToken, "a.test"))
	must(t, st.CompleteOutboundDomain(ctx, job.ID, first.DeliveryToken, "a.test", true))
	must(t, st.MarkOutboundJobRetry(ctx, job.ID, first.DeliveryToken, "b failed", time.Now().Add(-time.Second)))
	jobs, err = st.ClaimOutboundJobs(ctx, time.Now(), 100)
	must(t, err)
	second := jobs[0]
	if len(second.DeliveredDomains) != 1 || second.DeliveredDomains[0] != "a.test" {
		t.Fatal("checkpoint missing after claim")
	}
	if st.BeginOutboundDomain(ctx, job.ID, first.DeliveryToken, "b.test") == nil {
		t.Fatal("stale token permitted send")
	}
	if st.BeginOutboundDomain(ctx, job.ID, second.DeliveryToken, "a.test") == nil {
		t.Fatal("accepted domain resent")
	}
	must(t, st.BeginOutboundDomain(ctx, job.ID, second.DeliveryToken, "b.test"))
	_, err = pool.Exec(ctx, `UPDATE outbound_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	must(t, err)
	jobs, err = st.ClaimOutboundJobs(ctx, time.Now(), 100)
	must(t, err)
	if len(jobs) != 0 {
		t.Fatal("uncertain operation reclaimed for SMTP")
	}
	got, err := st.GetOutboundJob(ctx, job.ID)
	must(t, err)
	if got.State != models.OutboundFailed || got.InFlightDomain != "b.test" || len(got.DeliveredDomains) != 1 {
		t.Fatalf("uncertainty or accepted progress lost: %+v", got)
	}
	if err = st.RequeueOutboundJob(ctx, job.ID); err != store.ErrOutboundNotRetryable {
		t.Fatalf("uncertain retry: %v", err)
	}
	if st.MarkOutboundJobSent(ctx, job.ID, second.DeliveryToken, 250, "OK", "id") == nil {
		t.Fatal("stale worker finalized")
	}
}

// Each subtest uses a newly created testpg database. The supplied administrator
// DSN itself is NEVER cleared. Install only v1 with Goose's real bookkeeping.
func TestP0GooseRestartAndHistoricalGrantSafety(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade_and_restart", true: "preserve_incompatible_grants"}[conflict], func(t *testing.T) {
			_, pool, dsn := testpg.NewPostgres(t)
			ctx := context.Background()
			cfg, err := pgx.ParseConfig(dsn)
			must(t, err)
			_, err = pool.Exec(ctx, `DROP SCHEMA public CASCADE;CREATE SCHEMA public`)
			must(t, err)
			baseline, err := os.ReadFile("migrations/00001_baseline.sql")
			must(t, err)
			db := stdlib.OpenDB(*cfg)
			defer db.Close()
			provider, err := goose.NewProvider(goose.DialectPostgres, db, fstest.MapFS{"00001_baseline.sql": &fstest.MapFile{Data: baseline}})
			must(t, err)
			_, err = provider.Up(ctx)
			must(t, err)
			_, err = pool.Exec(ctx, `CREATE TABLE send_as_grants(id integer PRIMARY KEY);INSERT INTO send_as_grants VALUES(7)`)
			must(t, err)
			if conflict {
				_, err = pool.Exec(ctx, `CREATE TABLE mailbox_grants(legacy_id integer);INSERT INTO mailbox_grants VALUES(9)`)
				must(t, err)
			}
			err = postgres.Migrate(ctx, cfg)
			if conflict {
				if err == nil {
					t.Fatal("incompatible grants silently adopted")
				}
				var value int
				must(t, pool.QueryRow(ctx, `SELECT legacy_id FROM mailbox_grants`).Scan(&value))
				if value != 9 {
					t.Fatal("historical grant lost")
				}
			} else {
				must(t, err)
				must(t, postgres.Migrate(ctx, cfg))
			}
			var n int
			must(t, pool.QueryRow(ctx, `SELECT count(*) FROM send_as_grants WHERE id=7`).Scan(&n))
			if n != 1 {
				t.Fatal("migration destroyed history")
			}
			var version int
			must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
			want := 9 // 00009_domain_asset_guard
			if conflict {
				want = 1
			}
			if version != want {
				t.Fatalf("version=%d want=%d", version, want)
			}
		})
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
