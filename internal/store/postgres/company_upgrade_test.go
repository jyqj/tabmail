package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"path/filepath"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"testing"
	"testing/fstest"
)

func TestArchitectureUpgradeBackfillsExistingEmployeeAssets(t *testing.T) {
	st, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	// Only this freshly allocated disposable test database is reinitialized.
	// The administrator DSN and production data are never modified.
	_, e := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	must(t, e)
	cfg, e := pgx.ParseConfig(dsn)
	must(t, e)
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	old := fstest.MapFS{}
	names, e := filepath.Glob("migrations/0000*.sql")
	must(t, e)
	for _, name := range names {
		raw, e := os.ReadFile(name)
		must(t, e)
		old[filepath.Base(name)] = &fstest.MapFile{Data: raw}
	}
	if len(old) != 9 {
		t.Fatalf("expected PR14 schema, got %d files", len(old))
	}
	provider, e := goose.NewProvider(goose.DialectPostgres, db, old)
	must(t, e)
	_, e = provider.Up(ctx)
	must(t, e)
	tenant := &models.Tenant{Name: "Pre-upgrade company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	user := &models.User{TenantID: tenant.ID, Email: "upgrade@contact.test", PasswordHash: "test-only", Role: models.RoleUser, IsActive: true}
	must(t, st.CreateUser(ctx, user))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "upgrade.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	mailbox := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, OwnerUserID: &user.ID, Kind: "personal", FullAddress: "staff@upgrade.test", LocalPart: "staff", ResolvedDomain: zone.Domain, AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, mailbox))
	job := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &user.ID, SenderUserID: &user.ID, SenderMailboxID: &mailbox.ID, MailFrom: mailbox.FullAddress, To: []string{"client@test"}, RcptTo: []string{"client@test"}, Subject: "existing archive", TextBody: "old body", State: models.OutboundSent}
	must(t, st.CreateOutboundJob(ctx, job))
	attachment, draft := uuid.New(), uuid.New()
	_, e = pool.Exec(ctx, `INSERT INTO mail_attachments(id,tenant_id,mailbox_id,user_id,object_key,filename,content_type,size,sha256,state) VALUES($1,$2,$3,$4,'old-original','retained.txt','text/plain',4,$5,'ready')`, attachment, tenant.ID, mailbox.ID, user.ID, company.Hash("test"))
	must(t, e)
	_, e = pool.Exec(ctx, `INSERT INTO outbound_attachments(tenant_id,job_id,attachment_id) VALUES($1,$2,$3)`, tenant.ID, job.ID, attachment)
	must(t, e)
	_, e = pool.Exec(ctx, `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload) VALUES($1,$2,$3,$4,'{"subject":"legacy draft"}')`, draft, tenant.ID, user.ID, mailbox.ID)
	must(t, e)
	msg := &models.Message{TenantID: tenant.ID, ZoneID: zone.ID, MailboxID: mailbox.ID, Sender: "client@test", Recipients: []string{mailbox.FullAddress}, Subject: "old incoming", RawObjectKey: "old-original"}
	must(t, st.CreateMessage(ctx, msg))
	must(t, postgres.Migrate(ctx, cfg))
	must(t, postgres.Migrate(ctx, cfg))
	var version, pending, receipts int
	must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM mail_index_jobs WHERE message_id=$1 AND source_key='old-original'`, msg.ID).Scan(&pending))
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM draft_creation_receipts WHERE id=$1`, draft).Scan(&receipts))
	if version != 13 || pending != 1 || receipts != 1 {
		t.Fatalf("incomplete backfill v%d index%d receipts%d", version, pending, receipts)
	}
	_, e = pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, job.ID)
	must(t, e)
	actor := authz.Actor{Type: authz.PrincipalUser, ID: user.ID, TenantID: tenant.ID, Role: models.RoleUser}
	content, e := st.GetSubmissionContent(ctx, actor, job.ID)
	must(t, e)
	files, e := st.ListSubmissionAttachments(ctx, actor, job.ID)
	must(t, e)
	if content.TextBody != "old body" || len(files) != 1 || files[0].ID != attachment {
		t.Fatal("pre-upgrade sent content or pins lost")
	}
	if _, e = st.GetMailDraft(ctx, actor, draft); e != nil {
		t.Fatal("pre-upgrade draft lost", e)
	}
}
