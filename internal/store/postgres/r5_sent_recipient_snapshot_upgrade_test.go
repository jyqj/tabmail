package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

// This proves a REAL schema-16 write -> migration-17 -> explicit backfill.
// Removing the source of a current post-17 enqueue is not a legacy fixture.
func TestR5SentRecipientSnapshotUpgradeV16BoundedTrustedBackfill(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("sent-recipient upgrade acceptance requires owned TABMAIL_TEST_DB_DSN")
	}
	st, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	_, e := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	must(t, e)
	cfg, e := pgx.ParseConfig(dsn)
	must(t, e)
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	old := fstest.MapFS{}
	names, e := filepath.Glob("migrations/*.sql")
	must(t, e)
	for _, name := range names {
		prefix, _, _ := strings.Cut(filepath.Base(name), "_")
		v, e := strconv.Atoi(prefix)
		must(t, e)
		if v > 16 {
			continue
		}
		raw, e := os.ReadFile(name)
		must(t, e)
		old[filepath.Base(name)] = &fstest.MapFile{Data: raw}
	}
	if len(old) != 16 {
		t.Fatalf("expected exactly released migrations 1..16, got %d", len(old))
	}
	provider, e := goose.NewProvider(goose.DialectPostgres, db, old)
	must(t, e)
	_, e = provider.Up(ctx)
	must(t, e)
	var pre int
	must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&pre))
	if pre != 16 {
		t.Fatal("not a genuine pre-17 schema")
	}
	tenant := &models.Tenant{Name: "Snapshot upgrade", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, tenant))
	user := &models.User{TenantID: tenant.ID, Email: "snapshot-upgrade@contact.test", PasswordHash: "test-only", Role: models.RoleUser, IsActive: true}
	must(t, st.CreateUser(ctx, user))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "snapshot-upgrade.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, zone))
	mailbox := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, OwnerUserID: &user.ID, Kind: "personal", FullAddress: "employee@" + zone.Domain, LocalPart: "employee", ResolvedDomain: zone.Domain, AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, mailbox))
	otherMailbox := *mailbox
	otherMailbox.ID = uuid.Nil
	otherMailbox.FullAddress = "other@" + zone.Domain
	otherMailbox.LocalPart = "other"
	must(t, st.CreateMailbox(ctx, &otherMailbox))
	otherZone := &models.DomainZone{TenantID: tenant.ID, Domain: "snapshot-other-zone.test"}
	must(t, st.CreateZone(ctx, otherZone))
	templateID, templateVersion := uuid.New(), uuid.New()
	_, e = pool.Exec(ctx, `INSERT INTO mail_templates(id,tenant_id,name,draft,created_by) VALUES($1,$2,'source mismatch','{}',$3)`, templateID, tenant.ID, user.ID)
	must(t, e)
	_, e = pool.Exec(ctx, `INSERT INTO mail_template_versions(id,tenant_id,template_id,version,snapshot,content_hash,published_by) VALUES($1,$2,$3,1,'{}','test-only',$4)`, templateVersion, tenant.ID, templateID, user.ID)
	must(t, e)
	foreignTenant := &models.Tenant{Name: "Foreign snapshot source", PlanID: tenant.PlanID}
	must(t, st.CreateTenant(ctx, foreignTenant))
	foreignZone := &models.DomainZone{TenantID: foreignTenant.ID, Domain: "snapshot-foreign.test"}
	must(t, st.CreateZone(ctx, foreignZone))
	foreignMailbox := &models.Mailbox{TenantID: foreignTenant.ID, ZoneID: foreignZone.ID, FullAddress: "source@snapshot-foreign.test", LocalPart: "source", ResolvedDomain: foreignZone.Domain, AccessMode: models.AccessAPIKey}
	must(t, st.CreateMailbox(ctx, foreignMailbox))
	actor := authz.Actor{Type: authz.PrincipalUser, ID: user.ID, TenantID: tenant.ID, Role: models.RoleUser}
	type legacy struct {
		name     string
		job      *models.OutboundJob
		expected bool
	}
	cases := []legacy{}
	for _, name := range []string{"reliable", "known-empty", "no-job", "wrong-tenant", "wrong-sender-id", "wrong-zone-id", "wrong-from", "wrong-to", "wrong-cc", "wrong-subject", "wrong-text", "wrong-html", "wrong-headers", "wrong-template", "wrong-time", "envelope-only", "different-job-id"} {
		bcc := []string{"old-private@recipient.test"}
		if name == "known-empty" {
			bcc = nil
		}
		j := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &user.ID, SenderUserID: &user.ID, SenderMailboxID: &mailbox.ID, MailFrom: mailbox.FullAddress, To: []string{"old-visible@recipient.test"}, CC: []string{"old-copy@recipient.test"}, BCC: bcc, RcptTo: []string{"old-visible@recipient.test", "old-copy@recipient.test", "old-private@recipient.test"}, Subject: "pre-17 " + name, TextBody: "immutable pre-17 body", HTMLBody: "<p>original</p>", HeadersJSON: json.RawMessage(`{"X-Original":"yes"}`), State: models.OutboundSent}
		if name == "envelope-only" {
			j.BCC = nil
		}
		must(t, st.CreateOutboundJob(ctx, j))
		expected := name == "reliable" || name == "known-empty" || name == "envelope-only"
		cases = append(cases, legacy{name, j, expected})
		var q string
		args := []any{j.ID}
		switch name {
		case "no-job":
			q = `DELETE FROM outbound_jobs WHERE id=$1`
		case "wrong-tenant":
			_, e = pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, j.ID)
			must(t, e)
			q = `UPDATE outbound_jobs SET tenant_id=$2,zone_id=$3,sender_mailbox_id=$4,user_id=NULL,sender_user_id=NULL WHERE id=$1`
			args = append(args, foreignTenant.ID, foreignZone.ID, foreignMailbox.ID)
		case "wrong-sender-id":
			q = `UPDATE outbound_jobs SET sender_mailbox_id=$2 WHERE id=$1`
			args = append(args, otherMailbox.ID)
		case "wrong-zone-id":
			q = `UPDATE outbound_jobs SET zone_id=$2 WHERE id=$1`
			args = append(args, otherZone.ID)
		case "wrong-from":
			q = `UPDATE outbound_jobs SET mail_from='other@snapshot-upgrade.test' WHERE id=$1`
		case "wrong-to":
			q = `UPDATE outbound_jobs SET to_addrs=ARRAY['changed@recipient.test'] WHERE id=$1`
		case "wrong-cc":
			q = `UPDATE outbound_jobs SET cc_addrs='{}' WHERE id=$1`
		case "wrong-subject":
			q = `UPDATE outbound_jobs SET subject='changed' WHERE id=$1`
		case "wrong-text":
			q = `UPDATE outbound_jobs SET text_body='changed' WHERE id=$1`
		case "wrong-html":
			q = `UPDATE outbound_jobs SET html_body='changed' WHERE id=$1`
		case "wrong-headers":
			q = `UPDATE outbound_jobs SET headers_json='{"X-Original":"no"}' WHERE id=$1`
		case "wrong-template":
			q = `UPDATE outbound_jobs SET template_version_id=$2 WHERE id=$1`
			args = append(args, templateVersion)
		case "wrong-time":
			q = `UPDATE outbound_jobs SET created_at=created_at+interval '1 second' WHERE id=$1`
		case "different-job-id":
			_, e = pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, j.ID)
			must(t, e)
			q = `UPDATE outbound_jobs SET id=$2 WHERE id=$1`
			args = append(args, uuid.New())
		}
		if q != "" {
			_, e = pool.Exec(ctx, q, args...)
			must(t, e)
		}
	}
	must(t, postgres.Migrate(ctx, cfg))
	must(t, postgres.Migrate(ctx, cfg))
	var version, unknown int
	must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1 AND recipient_completeness='legacy_unknown' AND bcc_addrs IS NULL AND recipient_snapshot_version=0`, tenant.ID).Scan(&unknown))
	if version != 19 || unknown != len(cases) {
		t.Fatalf("migration guessed old snapshots: version=%d unknown=%d", version, unknown)
	}
	// GET cannot opportunistically complete even the reliable source cases.
	for _, c := range cases {
		v, e := st.GetSubmissionContent(ctx, actor, c.job.ID)
		must(t, e)
		if v.RecipientCompleteness != "legacy_unknown" || v.BCC != nil {
			t.Fatalf("read backfilled legacy %s: %+v", c.name, v)
		}
	}
	// An UNKNOWN->COMPLETE write with changed ORIGINAL content remains illegal.
	if _, e = pool.Exec(ctx, `UPDATE sent_mail_assets SET bcc_addrs=ARRAY['old-private@recipient.test'],recipient_completeness='complete',recipient_snapshot_version=1,text_body='changed' WHERE id=$1`, cases[0].job.ID); e == nil {
		t.Fatal("extension widened original asset immutability")
	}
	if _, e = pool.Exec(ctx, `UPDATE sent_mail_assets SET bcc_addrs=ARRAY['invented@recipient.test'],recipient_completeness='complete',recipient_snapshot_version=1 WHERE id=$1`, cases[0].job.ID); e == nil {
		t.Fatal("extension trusted a forged BCC array")
	}
	before := map[uuid.UUID]string{}
	for _, c := range cases {
		var raw string
		must(t, pool.QueryRow(ctx, `SELECT (to_jsonb(a)-ARRAY['bcc_addrs','recipient_completeness','recipient_snapshot_version'])::text FROM sent_mail_assets a WHERE id=$1`, c.job.ID).Scan(&raw))
		before[c.job.ID] = raw
	}
	var cursor *uuid.UUID
	scanned, completed := 0, 0
	for i := 0; i <= len(cases); i++ {
		r, e := st.BackfillSentRecipientSnapshotsV1(ctx, tenant.ID, cursor, 2)
		must(t, e)
		if r.Scanned > 2 {
			t.Fatal("batch exceeded explicit bound")
		}
		scanned += r.Scanned
		completed += r.Completed
		if r.Scanned == 0 {
			break
		}
		if r.AfterAssetID == nil || (cursor != nil && r.AfterAssetID.String() <= cursor.String()) {
			t.Fatal("keyset cursor did not advance past unreliable prefix")
		}
		cursor = r.AfterAssetID
	}
	if scanned != len(cases) || completed != 3 {
		t.Fatalf("bounded backfill scanned=%d completed=%d", scanned, completed)
	}
	// Empty structured source is complete even when RcptTo contains additional
	// envelope-only recipients. NEVER invent BCC using an envelope difference.
	for _, c := range cases {
		v, e := st.GetSubmissionContent(ctx, actor, c.job.ID)
		must(t, e)
		if c.expected {
			if v.RecipientCompleteness != "complete" || v.BCC == nil {
				t.Fatalf("trusted source %s not completed", c.name)
			}
			if c.name == "reliable" && (len(v.BCC) != 1 || v.BCC[0] != "old-private@recipient.test") {
				t.Fatal("original structured BCC lost")
			}
			if c.name != "reliable" && len(v.BCC) != 0 {
				t.Fatal("envelope inferred private BCC")
			}
		} else if v.RecipientCompleteness != "legacy_unknown" || v.BCC != nil {
			t.Fatalf("unreliable source %s completed", c.name)
		}
		var raw string
		must(t, pool.QueryRow(ctx, `SELECT (to_jsonb(a)-ARRAY['bcc_addrs','recipient_completeness','recipient_snapshot_version'])::text FROM sent_mail_assets a WHERE id=$1`, c.job.ID).Scan(&raw))
		if raw != before[c.job.ID] {
			t.Fatal("backfill changed original immutable content")
		}
	}
	// Repeating the command has NO row UPDATE or audit/event effects. The record
	// xmin and the full public row/event counts stay exactly equal.
	snapshot := func() string {
		var raw string
		must(t, pool.QueryRow(ctx, `SELECT jsonb_build_object('assets',(SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(a),'xmin',a.xmin::text) ORDER BY id) FROM sent_mail_assets a WHERE tenant_id=$1),'audit',(SELECT count(*) FROM audit_log),'outbox',(SELECT count(*) FROM outbox_events),'events',(SELECT count(*) FROM mailbox_event_log))::text`, tenant.ID).Scan(&raw))
		return raw
	}
	unchanged := snapshot()
	r, e := st.BackfillSentRecipientSnapshotsV1(ctx, tenant.ID, nil, 1000)
	must(t, e)
	if r.Completed != 0 || snapshot() != unchanged {
		t.Fatal("repeated backfill changed completed rows or emitted effects")
	}
	_, e = pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE tenant_id=$1`, tenant.ID)
	must(t, e)
	for _, c := range cases {
		v, e := st.GetSubmissionContent(ctx, actor, c.job.ID)
		must(t, e)
		if c.expected && v.RecipientCompleteness != "complete" {
			t.Fatal("completed legacy asset depended on queue lifetime")
		}
		if !c.expected && v.RecipientCompleteness != "legacy_unknown" {
			t.Fatal("missing source invented completeness")
		}
	}
	for _, input := range []struct {
		tenant uuid.UUID
		limit  int
	}{{uuid.Nil, 1}, {tenant.ID, 0}, {tenant.ID, 1001}} {
		if _, e = st.BackfillSentRecipientSnapshotsV1(ctx, input.tenant, nil, input.limit); e == nil {
			t.Fatal("unbounded/unscoped maintenance admitted")
		}
	}
}
