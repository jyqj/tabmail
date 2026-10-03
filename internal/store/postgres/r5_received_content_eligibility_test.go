package postgres_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/app/companymail"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
)

func r5ReceivedRequirePG(t *testing.T) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("TABMAIL_TEST_DB_DSN required: received content runtime must not skip")
	}
}
func r5Received404(t *testing.T, err error) {
	t.Helper()
	e, ok := app.As(err)
	if !ok || e.Kind != app.KindNotFound {
		t.Fatalf("want unavailable content 404, got %v", err)
	}
}

// This intentionally retains cached documents and canonical objects. Refusing
// an expired request by deleting the fixture would not prove authorization.
func TestR5ReceivedContentEligibilityMatrix(t *testing.T) {
	r5ReceivedRequirePG(t)
	for _, cutoff := range []string{"active", "expiry", "equality", "purge", "archive-expiry", "trash-live", "trash-expiry", "null-deadline", "shared-zero-historical", "shared-null-historical"} {
		t.Run(cutoff, func(t *testing.T) {
			f := seedCompany(t)
			m, o, svc := r5ReadFixture(t, f)
			ctx := context.Background()
			_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, f.shared.ID)
			must(t, e)
			sql := `UPDATE messages SET expires_at=clock_timestamp()+interval '1 day' WHERE id=$1`
			deny := false
			folder := "inbox"
			switch cutoff {
			case "expiry", "equality":
				sql = `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`
				deny = true
			case "purge":
				sql = `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp(),expires_at=NULL WHERE id=$1`
				deny = true
				folder = "trash"
			case "archive-expiry":
				sql = `UPDATE messages SET archived_at=clock_timestamp(),expires_at=clock_timestamp() WHERE id=$1`
				deny = true
				folder = "archive"
			case "trash-live":
				sql = `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '1 day',expires_at=clock_timestamp()+interval '1 day' WHERE id=$1`
				folder = "trash"
			case "trash-expiry":
				sql = `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '1 day',expires_at=clock_timestamp() WHERE id=$1`
				deny = true
				folder = "trash"
			case "null-deadline":
				sql = `UPDATE messages SET expires_at=NULL WHERE id=$1`
			case "shared-zero-historical", "shared-null-historical":
				deny = true // Root contract: recorded snapshot wins; GC protection remains.
				override := any(0)
				if cutoff == "shared-null-historical" {
					override = nil
				}
				_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=$2 WHERE id=$1`, f.shared.ID, override)
				must(t, e)
				sql = `UPDATE messages SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`
			}
			_, e = f.pool.Exec(ctx, sql, m.ID)
			must(t, e)
			detail, e := svc.Message(ctx, f.u, f.shared.ID, m.ID)
			if deny {
				if detail != nil {
					t.Fatal("expired cached detail exposed")
				}
				r5Received404(t, e)
			} else {
				must(t, e)
				if detail == nil || detail.TextBody != "cached synthetic content" {
					t.Fatal("active cached content lost")
				}
			}
			before := o.gets.Load()
			source, e := svc.Source(ctx, f.u, f.shared.ID, m.ID)
			if deny {
				if source != nil {
					source.Close()
					t.Fatal("expired source returned")
				}
				r5Received404(t, e)
			} else {
				must(t, e)
				must(t, source.Close())
			}
			parts, e := svc.InboundAttachments(ctx, f.u, f.shared.ID, m.ID)
			if deny {
				if parts != nil {
					t.Fatal("expired parts returned")
				}
				r5Received404(t, e)
			} else {
				must(t, e)
			}
			if deny {
				v, e := svc.InboundAttachment(ctx, f.u, f.shared.ID, m.ID, 0)
				if v != nil {
					t.Fatal("expired ordinal returned")
				}
				r5Received404(t, e)
				v, e = svc.InboundAttachmentByID(ctx, f.u, f.shared.ID, m.ID, "existing-or-guessed-part")
				if v != nil {
					t.Fatal("expired stable part returned")
				}
				r5Received404(t, e)
				if o.gets.Load() != before {
					t.Fatal("initial eligibility denial opened object")
				}
				d, e := f.st.GetParsedMessage(ctx, f.u, f.shared.ID, m.ID)
				if d != nil {
					t.Fatal("expired parsed cache exposed")
				}
				r5Received404(t, e)
				e = f.st.SaveParsedMessage(ctx, f.u, f.shared.ID, company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("fixture"), ParserVersion: 1, TextBody: "must not overwrite"})
				r5Received404(t, e)
			}
			for _, query := range []string{"", "cached synthetic content"} {
				rows, n, e := f.st.ListWorkMessages(ctx, f.u, f.shared.ID, folder, query, models.Page{})
				must(t, e)
				want := 1
				if deny {
					want = 0
				}
				if len(rows) != want || n != want {
					t.Fatalf("list/search count exposed expired content: rows=%d total=%d want=%d", len(rows), n, want)
				}
			}
			status, e := f.st.ContentIndexStatus(ctx, f.u, f.shared.ID)
			must(t, e)
			wantTotal := 1
			if deny || folder == "trash" {
				wantTotal = 0
			}
			if status.Total != wantTotal || status.Indexed != wantTotal {
				t.Fatalf("index summary leaked expired metadata: %+v", status)
			}
			rows, n, e := f.st.ListMessageConversation(ctx, f.u, f.shared.ID, m.ID, models.Page{})
			if deny {
				if len(rows) != 0 || n != 0 {
					t.Fatal("expired conversation exposed")
				}
				r5Received404(t, e)
			} else {
				must(t, e)
				want := 1
				if folder == "trash" {
					want = 0
				}
				if len(rows) != want || n != want {
					t.Fatal("conversation folder semantics changed")
				}
			}
			var body string
			must(t, f.pool.QueryRow(ctx, `SELECT text_body FROM mail_documents WHERE message_id=$1`, m.ID).Scan(&body))
			if body != "cached synthetic content" {
				t.Fatal("eligibility altered cache")
			}
		})
	}
}

func TestR5ReceivedPersonalHistoricalExpiryIsExempt(t *testing.T) {
	r5ReceivedRequirePG(t)
	f := seedCompany(t)
	ctx := context.Background()
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "fixture@example.test", Recipients: []string{f.personal.FullAddress}, Subject: "historical", RawObjectKey: uuid.NewString()}
	must(t, f.st.CreateMessage(ctx, m))
	_, e := f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, m.ID)
	must(t, e)
	v, e := f.st.GetWorkMessage(ctx, f.u, f.personal.ID, m.ID)
	must(t, e)
	if v == nil {
		t.Fatal("personal historical deadline defeated original permanent protection")
	}
	_, e = f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp() WHERE id=$1`, m.ID)
	must(t, e)
	v, e = f.st.GetWorkMessage(ctx, f.u, f.personal.ID, m.ID)
	if v != nil {
		t.Fatal("personal permanence incorrectly waived purge")
	}
	r5Received404(t, e)
}

// Uses a real identity/mailbox wait edge, then observes the PostgreSQL clock.
// The transaction began before the deadline, so now() would return stale time.
func TestR5ReceivedContentRechecksDeadlineAfterAuthorizationWait(t *testing.T) {
	r5ReceivedRequirePG(t)
	for _, endpoint := range []string{"detail", "parsed", "save-parsed", "list", "conversation", "summary"} {
		t.Run(endpoint, func(t *testing.T) {
			f := seedCompany(t)
			m, _, svc := r5ReadFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, f.shared.ID)
			must(t, e)
			var deadline time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE messages SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, m.ID).Scan(&deadline))
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.shared.ID)
			must(t, e)
			done := make(chan error, 1)
			go func() {
				var e error
				switch endpoint {
				case "detail":
					_, e = svc.Message(ctx, f.u, f.shared.ID, m.ID)
				case "parsed":
					_, e = f.st.GetParsedMessage(ctx, f.u, f.shared.ID, m.ID)
				case "save-parsed":
					e = f.st.SaveParsedMessage(ctx, f.u, f.shared.ID, company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("fixture"), ParserVersion: 1})
				case "conversation":
					_, _, e = f.st.ListMessageConversation(ctx, f.u, f.shared.ID, m.ID, models.Page{})
				case "list":
					v, n, err := f.st.ListWorkMessages(ctx, f.u, f.shared.ID, "inbox", "", models.Page{})
					if err == nil && (len(v) != 0 || n != 0) {
						err = errors.New("expired content accepted after mailbox lock wait")
					}
					done <- err
					return
				case "summary":
					v, err := f.st.ContentIndexStatus(ctx, f.u, f.shared.ID)
					if err == nil && v.Total != 0 {
						err = errors.New("expired content accepted after mailbox lock wait")
					}
					done <- err
					return
				}
				if v, ok := app.As(e); ok && v.Kind == app.KindNotFound {
					done <- nil
					return
				}
				if e == nil {
					e = errors.New("expired content accepted after mailbox lock wait")
				}
				done <- e
			}()
			reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
			r5AwaitSentDeadline(t, f, ctx, reader, deadline)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
		})
	}
}

// Object-port callback changes the real PostgreSQL deadline at an exact I/O
// boundary. It does not replace the production repository or parser response.
type r5ReceivedIOObjects struct {
	*r5ReadObjects
	onOpen func()
	onRead func()
}

func (o *r5ReceivedIOObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, e := o.r5ReadObjects.Get(ctx, key)
	if e != nil {
		return nil, e
	}
	if o.onOpen != nil {
		o.onOpen()
	}
	return &r5ReceivedIOReader{ReadCloser: r, onRead: o.onRead}, nil
}

type r5ReceivedIOReader struct {
	io.ReadCloser
	onRead func()
	once   sync.Once
}

func (r *r5ReceivedIOReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		if r.onRead != nil {
			r.onRead()
		}
	})
	return r.ReadCloser.Read(p)
}
func TestR5ReceivedContentRechecksExpiryAtObjectBoundaries(t *testing.T) {
	r5ReceivedRequirePG(t)
	for _, endpoint := range []string{"detail", "source", "ordinal", "stable-part", "first-byte"} {
		t.Run(endpoint, func(t *testing.T) {
			f := seedCompany(t)
			m, o, _ := r5ReadFixture(t, f)
			ctx := context.Background()
			_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, f.shared.ID)
			must(t, e)
			raw := "From: fixture@boundary.test\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nprivate received body\r\n--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=private.txt\r\n\r\nprivate received attachment\r\n--b--\r\n"
			must(t, o.Put(ctx, m.RawObjectKey, strings.NewReader(raw), int64(len(raw))))
			doc, e := mailcontent.New(o).Document(ctx, m.ID, m.RawObjectKey)
			must(t, e)
			if len(doc.Parts) != 1 {
				t.Fatal("invalid MIME fixture")
			}
			// Force a real parser/object request; a prewarmed application instance is
			// never used to justify absence of object boundary validation.
			_, e = f.pool.Exec(ctx, `DELETE FROM mail_documents WHERE message_id=$1`, m.ID)
			must(t, e)
			expire := func() {
				_, e := f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`, m.ID)
				must(t, e)
			}
			obj := &r5ReceivedIOObjects{r5ReadObjects: o, onOpen: expire}
			if endpoint == "first-byte" {
				obj.onOpen = nil
				obj.onRead = expire
			}
			svc := companymail.NewService(f.st, obj)
			var err error
			var exposed bool
			switch endpoint {
			case "detail":
				v, e := svc.Message(ctx, f.u, f.shared.ID, m.ID)
				err = e
				exposed = v != nil
			case "source":
				v, e := svc.Source(ctx, f.u, f.shared.ID, m.ID)
				err = e
				exposed = v != nil
				if v != nil {
					v.Close()
				}
			case "ordinal":
				v, e := svc.InboundAttachment(ctx, f.u, f.shared.ID, m.ID, 0)
				err = e
				exposed = v != nil
			case "stable-part":
				v, e := svc.InboundAttachmentByID(ctx, f.u, f.shared.ID, m.ID, doc.Parts[0].ID)
				err = e
				exposed = v != nil
			case "first-byte":
				v, e := svc.Source(ctx, f.u, f.shared.ID, m.ID)
				must(t, e)
				defer v.Close()
				buf := make([]byte, 100)
				n, e := v.Read(buf)
				err = e
				exposed = n != 0
				for _, b := range buf {
					if b != 0 {
						t.Fatal("denied source retained stale first-byte buffer")
					}
				}
			}
			if exposed {
				t.Fatal("expiry crossed during object I/O exposed received content")
			}
			r5Received404(t, err)
		})
	}
}

func TestR5ReceivedConversationMembersAndSearchAreEligible(t *testing.T) {
	r5ReceivedRequirePG(t)
	f := seedCompany(t)
	root, _, _ := r5ReadFixture(t, f)
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, f.shared.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE mail_documents SET thread_key='received-private-thread' WHERE message_id=$1`, root.ID)
	must(t, e)
	member := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "expired@fixture.test", Recipients: []string{f.shared.FullAddress}, Subject: "expired-private-subject", RawObjectKey: uuid.NewString()}
	must(t, f.st.CreateMessage(ctx, member))
	must(t, f.st.SaveParsedMessage(ctx, f.u, f.shared.ID, company.ParsedMessage{MessageID: member.ID, SourceKey: member.RawObjectKey, SourceSHA256: company.Hash("expired-private-body"), ParserVersion: 1, TextBody: "expired-private-body", ThreadKey: "received-private-thread"}))
	_, e = f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`, member.ID)
	must(t, e)
	rows, n, e := f.st.ListMessageConversation(ctx, f.u, f.shared.ID, root.ID, models.Page{})
	must(t, e)
	if n != 1 || len(rows) != 1 || rows[0].ID != root.ID {
		t.Fatal("active conversation root leaked expired member/count")
	}
	for _, q := range []string{"expired-private-subject", "expired-private-body"} {
		rows, n, e = f.st.ListWorkMessages(ctx, f.u, f.shared.ID, "inbox", q, models.Page{})
		must(t, e)
		if len(rows) != 0 || n != 0 {
			t.Fatal("expired derived index leaked search/count")
		}
	}
	status, e := f.st.ContentIndexStatus(ctx, f.u, f.shared.ID)
	must(t, e)
	if status.Total != 1 || status.Indexed != 1 {
		t.Fatal("expired member leaked index summary")
	}
}

// These waits occur AFTER initial actor/mailbox/message qualification. They
// independently cover document UPSERT contention and later relation locks;
// an initial mailbox-lock test cannot establish these release boundaries.
func TestR5ReceivedEligibilityAfterLaterWaits(t *testing.T) {
	r5ReceivedRequirePG(t)
	for _, axis := range []string{"save-message", "save-mailbox", "save-new-document", "conversation-root", "list-count-page", "summary-shared-mailbox", "summary-personal-mailbox"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			m, _, _ := r5ReadFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, f.shared.ID)
			must(t, e)
			mailbox := f.shared.ID
			if axis == "summary-personal-mailbox" {
				mailbox = f.personal.ID
				_, e = f.pool.Exec(ctx, `UPDATE messages SET mailbox_id=$2 WHERE id=$1`, m.ID, mailbox)
				must(t, e)
			}
			if axis == "conversation-root" {
				_, e = f.pool.Exec(ctx, `UPDATE mail_documents SET thread_key='later-wait-thread' WHERE message_id=$1`, m.ID)
				must(t, e)
				member := &models.Message{TenantID: f.tenant.ID, MailboxID: mailbox, ZoneID: f.zone.ID, Sender: "fixture@example.test", Recipients: []string{f.shared.FullAddress}, Subject: "live-conversation-member", RawObjectKey: uuid.NewString()}
				must(t, f.st.CreateMessage(ctx, member))
				must(t, f.st.SaveParsedMessage(ctx, f.u, mailbox, company.ParsedMessage{MessageID: member.ID, SourceKey: member.RawObjectKey, SourceSHA256: company.Hash("member"), ParserVersion: 1, ThreadKey: "later-wait-thread"}))
			}
			if axis == "save-new-document" {
				_, e = f.pool.Exec(ctx, `DELETE FROM mail_documents WHERE message_id=$1`, m.ID)
				must(t, e)
			}
			var deadline time.Time
			if strings.Contains(axis, "mailbox") {
				must(t, f.pool.QueryRow(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, mailbox).Scan(&deadline))
			} else {
				must(t, f.pool.QueryRow(ctx, `UPDATE messages SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, m.ID).Scan(&deadline))
			}
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			// pg_stat_activity.query is bounded by the native 1kB setting.
			// Match a stable visible prefix shared by the old page SQL and
			// the new eligible CTE, not the message_user_states tail beyond
			// truncation. r5WaitBlockedBy still requires this exact owned
			// relation-lock holder PID in the wait edge and isolated DB.
			blocker := "FROM messages m"
			if strings.HasPrefix(axis, "save-") {
				if axis == "save-new-document" {
					_, e = hold.Exec(ctx, `INSERT INTO mail_documents(tenant_id,message_id,source_key,source_sha256,parser_version,text_body,html_body,body_access,parts,thread_key,search_text) VALUES($1,$2,$3,$4,1,'uncommitted predecessor','','full','[]'::jsonb,'','predecessor')`, f.tenant.ID, m.ID, m.RawObjectKey, company.Hash("predecessor"))
				} else {
					_, e = hold.Exec(ctx, `SELECT message_id FROM mail_documents WHERE message_id=$1 FOR UPDATE`, m.ID)
				}
				blocker = "INSERT INTO mail_documents"
			} else if strings.HasPrefix(axis, "summary-") {
				_, e = hold.Exec(ctx, `LOCK TABLE mail_index_jobs IN ACCESS EXCLUSIVE MODE`)
				blocker = "mail_index_jobs"
			} else {
				_, e = hold.Exec(ctx, `LOCK TABLE message_user_states IN ACCESS EXCLUSIVE MODE`)
			}
			must(t, e)
			type result struct {
				rows   []*models.Message
				total  int
				status *company.ContentIndexStatus
				err    error
			}
			done := make(chan result, 1)
			go func() {
				v := result{}
				switch {
				case strings.HasPrefix(axis, "save-"):
					v.err = f.st.SaveParsedMessage(ctx, f.u, mailbox, company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("replacement"), ParserVersion: 1, TextBody: "late overwrite must roll back"})
				case axis == "conversation-root":
					v.rows, v.total, v.err = f.st.ListMessageConversation(ctx, f.u, mailbox, m.ID, models.Page{})
				case axis == "list-count-page":
					v.rows, v.total, v.err = f.st.ListWorkMessages(ctx, f.u, mailbox, "inbox", "", models.Page{})
				default:
					v.status, v.err = f.st.ContentIndexStatus(ctx, f.u, mailbox)
				}
				done <- v
			}()
			reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), blocker)
			r5AwaitSentDeadline(t, f, ctx, reader, deadline)
			if axis == "save-new-document" {
				must(t, hold.Commit(ctx))
			} else {
				must(t, hold.Rollback(ctx))
			}
			select {
			case v := <-done:
				if axis == "list-count-page" {
					must(t, v.err)
					if len(v.rows) != 0 || v.total != 0 {
						t.Fatalf("later wait leaked old count/page: rows=%d total=%d", len(v.rows), v.total)
					}
				} else {
					r5Received404(t, v.err)
					if len(v.rows) != 0 || v.total != 0 || v.status != nil {
						t.Fatal("later wait denial returned stale payload")
					}
				}
			case <-ctx.Done():
				t.Fatal("later wait operation did not finish")
			}
			if strings.HasPrefix(axis, "save-") {
				var body string
				must(t, f.pool.QueryRow(ctx, `SELECT text_body FROM mail_documents WHERE message_id=$1`, m.ID).Scan(&body))
				wantBody := "cached synthetic content"
				if axis == "save-new-document" {
					wantBody = "uncommitted predecessor"
				}
				if body != wantBody {
					t.Fatal("expired UPSERT committed cache overwrite")
				}
			}
		})
	}
}
