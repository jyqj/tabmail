package postgres_test

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app/companymail"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// The object adapter alone is in-memory and counted. All member/grant/message
// and parsed-cache reads/writes use the real PgStore in an isolated test DB.
// These tests cover new authorizations, not revoking bytes already delivered.
type r5ReadObjects struct {
	*testutil.MemoryObjectStore
	gets atomic.Int64
}

func (o *r5ReadObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	o.gets.Add(1)
	return o.MemoryObjectStore.Get(ctx, key)
}
func r5ReadFixture(t *testing.T, f *companyFixture) (*models.Message, *r5ReadObjects, *companymail.Service) {
	t.Helper()
	ctx := context.Background()
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "sender@read.test", Recipients: []string{f.shared.FullAddress}, RawObjectKey: "r5-read-" + uuid.NewString(), Subject: "read boundary"}
	must(t, f.st.CreateMessage(ctx, m))
	raw := "From: sender@read.test\r\nTo: support@company.test\r\nSubject: read boundary\r\n\r\nsynthetic content"
	o := &r5ReadObjects{MemoryObjectStore: testutil.NewMemoryObjectStore()}
	must(t, o.Put(ctx, m.RawObjectKey, strings.NewReader(raw), int64(len(raw))))
	d := company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash(raw), ParserVersion: 1, TextBody: "cached synthetic content", BodyAccess: "full"}
	must(t, f.st.SaveParsedMessage(ctx, f.u, f.shared.ID, d))
	return m, o, companymail.NewService(f.st, o)
}
func TestR5ReadCachedContentRejectsNewRequestsAfterRevoke(t *testing.T) {
	f := seedCompany(t)
	m, o, svc := r5ReadFixture(t, f)
	ctx := context.Background()
	before, err := svc.Message(ctx, f.u, f.shared.ID, m.ID)
	must(t, err)
	if before.TextBody != "cached synthetic content" || o.gets.Load() != 0 {
		t.Fatal("fixture did not exercise the stored parsed document")
	}
	r, err := svc.Source(ctx, f.u, f.shared.ID, m.ID)
	must(t, err)
	must(t, r.Close())
	count := o.gets.Load()
	if count != 1 {
		t.Fatal("fixture source did not open exactly one object")
	}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: true}))
	if d, e := f.st.GetParsedMessage(ctx, f.u, f.shared.ID, m.ID); e == nil || d != nil {
		t.Fatal("parsed cache opened after completed read revocation")
	}
	if d, e := svc.Message(ctx, f.u, f.shared.ID, m.ID); e == nil || d != nil {
		t.Fatal("content opened after completed read revocation")
	}
	if r, e := svc.Source(ctx, f.u, f.shared.ID, m.ID); e == nil || r != nil {
		if r != nil {
			_ = r.Close()
		}
		t.Fatal("raw source opened after completed read revocation")
	}
	if parts, e := svc.InboundAttachments(ctx, f.u, f.shared.ID, m.ID); e == nil || parts != nil {
		t.Fatal("attachment metadata opened after completed read revocation")
	}
	if file, e := svc.InboundAttachment(ctx, f.u, f.shared.ID, m.ID, 0); e == nil || file != nil {
		t.Fatal("ordinal attachment opened after completed read revocation")
	}
	if file, e := svc.InboundAttachmentByID(ctx, f.u, f.shared.ID, m.ID, "unknown-part"); e == nil || file != nil {
		t.Fatal("stable attachment opened after completed read revocation")
	}
	if o.gets.Load() != count {
		t.Fatal("denied reads still opened the object adapter")
	}
	var docs int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE message_id=$1`, m.ID).Scan(&docs))
	if docs != 1 {
		t.Fatal("revocation test passed by deleting cached content")
	}
	exists, err := o.Exists(ctx, m.RawObjectKey)
	must(t, err)
	if !exists {
		t.Fatal("revocation removed canonical bytes")
	}
	t.Log("existing cache/raw bytes retained; new requests denied before object access")
}
func TestR5ReadAdminAndWrongMailboxDoNotOpenObject(t *testing.T) {
	f := seedCompany(t)
	m, o, svc := r5ReadFixture(t, f)
	ctx := context.Background()
	if _, err := svc.Message(ctx, f.a, f.shared.ID, m.ID); err == nil {
		t.Fatal("management role became content permission")
	}
	if _, err := svc.Message(ctx, f.u, f.personal.ID, m.ID); err == nil {
		t.Fatal("same-tenant wrong mailbox read shared content")
	}
	foreign := f.u
	foreign.TenantID = uuid.New()
	if _, err := svc.Message(ctx, foreign, f.shared.ID, m.ID); err == nil {
		t.Fatal("mismatched tenant actor read cached content")
	}
	if o.gets.Load() != 0 {
		t.Fatal("denied identities opened object bytes")
	}
}
func TestR5ReadWaitingIdentityReloadRejectsFrozenActor(t *testing.T) {
	f := seedCompany(t)
	m, o, svc := r5ReadFixture(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
	must(t, err)
	type result struct {
		value *models.MessageDetail
		err   error
	}
	done := make(chan result, 1)
	go func() { v, e := svc.Message(ctx, f.u, f.shared.ID, m.ID); done <- result{v, e} }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")
	// The controller changes only the fixture's active flag while the genuine
	// member reload waits. This is not a full admin/session-revocation journey.
	_, err = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
	must(t, err)
	must(t, hold.Commit(ctx))
	select {
	case r := <-done:
		if r.err == nil || r.value != nil {
			t.Fatal("waiting read trusted stale active actor")
		}
	case <-ctx.Done():
		t.Fatal("read did not finish after identity lock released")
	}
	if o.gets.Load() != 0 {
		t.Fatal("inactive waiting read opened object")
	}
}
