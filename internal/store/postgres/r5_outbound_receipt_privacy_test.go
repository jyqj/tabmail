package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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

// Only the post-commit display port is fault-injected. Every command, archive,
// ledger, middleware and router operation delegates to the actual PG store.
type r5ReceiptDisplayFaultStore struct {
	// Preserve the real repository's full method set, including the optional
	// idempotency/draft lookup ports. Embedding only store.Store erased those
	// ports and caused a 400 before enqueue, not a post-commit display fault.
	*postgres.PgStore
	failAt int
	calls  int
}

func (s *r5ReceiptDisplayFaultStore) GetOutboundReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	s.calls++
	if s.failAt > 0 && s.calls == s.failAt {
		return nil, errors.New("PRIVATE_DISPLAY_LOOKUP_FAILURE")
	}
	return s.PgStore.GetOutboundReceipt(ctx, a, id, scope)
}

var _ interface {
	FindOutboundSubmission(context.Context, uuid.UUID, string, string, string) (*models.OutboundJob, error)
	FindOutboundJobByDraft(context.Context, uuid.UUID, uuid.UUID) (*models.OutboundJob, error)
} = (*r5ReceiptDisplayFaultStore)(nil)

func r5OrdinaryOutboundRouter(t *testing.T, f *companyFixture, failAt int) (http.Handler, *r5ReceiptDisplayFaultStore) {
	t.Helper()
	st := &r5ReceiptDisplayFaultStore{PgStore: f.st, failAt: failAt}
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h := api.NewRouter(api.RouterConfig{Store: st, CompanyRepository: f.st, ObjectStore: obj, RawObjects: rawobject.NewStore(obj, f.st), JWTSecret: companyTestJWT, MailboxTokenSecret: "test-only-mailbox-secret", PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), OutboundService: svc, Logger: zerolog.Nop(), Readiness: f.st.Readiness})
	return h, st
}

// Draft identity and submit revision come from the actual shipping save/get
// chain. No SQL-created draft, guessed revision or relaxed body decoder is used.
func r5ReceiptHTTPDraft(t *testing.T, h http.Handler, token string, mailbox uuid.UUID, payload company.DraftPayload) company.Draft {
	t.Helper()
	id := uuid.New()
	saved := r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", company.Draft{ID: id, MailboxID: mailbox, Payload: payload, Revision: 0}, 200)
	created := r3Data[company.Draft](t, saved)
	loaded := r3Data[company.Draft](t, r3HTTP(t, h, token, "GET", "/api/v1/company/drafts/"+id.String(), nil, 200))
	if created.ID != id || loaded.ID != id || created.Revision < 1 || loaded.Revision != created.Revision || loaded.MailboxID != mailbox {
		t.Fatal("formal draft save/get did not return its real identity/revision")
	}
	return loaded
}

func r5ReceiptHTTPFailureLabel(raw []byte) (string, string) {
	var env struct {
		Error struct{ Code, Message string }
	}
	if json.Unmarshal(raw, &env) != nil {
		return "unparseable", "redacted"
	}
	code := "unknown"
	switch env.Error.Code {
	case "BAD_REQUEST", "INTERNAL", "CONFLICT", "FORBIDDEN", "NOT_FOUND", "QUOTA_EXCEEDED":
		code = env.Error.Code
	}
	label := "redacted"
	switch env.Error.Message {
	case "idempotent submission unavailable":
		label = "idempotency_port_missing"
	case "invalid request body":
		label = "invalid_body"
	case "expected_revision is required":
		label = "revision_required"
	case "internal server error":
		label = "internal"
	}
	return code, label
}

func r5StrictOutboundReceiptJSON(t *testing.T, raw []byte, expected *uuid.UUID) *company.OutboundReceipt {
	t.Helper()
	r5SubmissionPrivacyJSON(t, json.RawMessage(raw))
	var env struct{ Data json.RawMessage }
	must(t, json.Unmarshal(raw, &env))
	var fields map[string]json.RawMessage
	must(t, json.Unmarshal(env.Data, &fields))
	allowed := map[string]bool{"id": true, "tenant_id": true, "state": true, "status": true, "progress": true, "created_at": true, "updated_at": true, "attempt_count": true, "next_retry": true, "delivery_uncertain": true, "capabilities": true}
	for key := range fields {
		if !allowed[key] {
			t.Fatalf("ordinary response key not allowlisted: %s", key)
		}
	}
	var v company.OutboundReceipt
	must(t, json.Unmarshal(env.Data, &v))
	if expected != nil && v.ID != *expected {
		t.Fatal("safe receipt changed command identity")
	}
	if v.Progress.Completeness == "unknown" && v.Progress.Counts != nil {
		t.Fatal("unknown progress fabricated counts")
	}
	if v.Capabilities != nil {
		switch v.Capabilities.RetryBlockReason {
		case "", "unknown", "delivery_uncertain", "state_not_retryable", "sender_authority":
		default:
			t.Fatal("unsafe capability diagnostic")
		}
	}
	return &v
}

func TestR5OutboundReceiptPrivacyHTTPAllReadSurfaces(t *testing.T) {
	f, j := r5SubmissionPrivacyFixture(t)
	_, err := f.pool.Exec(context.Background(), `UPDATE outbound_recipients SET state='permanent',diagnostic='PRIVATE_RECIPIENT_DIAGNOSTIC' WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0])
	must(t, err)
	must(t, f.st.CreateOutboundAttempt(context.Background(), &models.OutboundAttempt{TenantID: f.tenant.ID, JobID: j.ID, Attempt: 1, SMTPCode: 550, SMTPResponse: "PRIVATE_PROTOCOL", RemoteHost: "hidden@fixture.test", Error: "PRIVATE_ATTEMPT_DIAGNOSTIC"}))
	h, _ := r5OrdinaryOutboundRouter(t, f, 0)
	token := r3Token(t, f.employee)
	for _, surface := range []string{"legacy-list", "legacy-detail", "legacy-attempts", "recipient-aggregate", "company-list", "company-detail"} {
		t.Run(surface, func(t *testing.T) {
			paths := map[string]string{"legacy-list": "/api/v1/outbound", "legacy-detail": "/api/v1/outbound/" + j.ID.String(), "legacy-attempts": "/api/v1/outbound/" + j.ID.String() + "/attempts", "recipient-aggregate": "/api/v1/company/outbound/" + j.ID.String() + "/recipients", "company-list": "/api/v1/company/submissions", "company-detail": "/api/v1/company/submissions/" + j.ID.String()}
			w := r3HTTP(t, h, token, "GET", paths[surface], nil, 200)
			raw := w.Body.Bytes()
			if strings.HasSuffix(surface, "list") {
				list := r3Data[[]company.OutboundReceipt](t, w)
				if len(list) != 1 {
					t.Fatalf("receipt list length=%d", len(list))
				}
				row, e := json.Marshal(struct {
					Data company.OutboundReceipt `json:"data"`
				}{list[0]})
				must(t, e)
				raw = row
			}
			v := r5StrictOutboundReceiptJSON(t, raw, &j.ID)
			if v.TenantID == nil || *v.TenantID != f.tenant.ID || v.Status != "partially_accepted" || v.Progress.Completeness != "known" || v.Progress.Counts == nil || v.Progress.Counts.Total != 3 || v.Progress.Counts.Permanent != 1 {
				t.Fatal("ordinary surface filtered the complete ledger")
			}
			if surface != "company-list" && (v.Capabilities == nil || !v.Capabilities.ViewContent) {
				t.Fatal("safe receipt weakened actual current-read hint")
			}
		})
	}
}

func TestR5OutboundReceiptPrivacyHTTPFreshAndReplay(t *testing.T) {
	for _, mode := range []string{"owner-live", "send-only-shared", "expired-before-replay"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			mailbox := f.personal
			if mode == "send-only-shared" {
				mailbox = f.shared
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mailbox.ID, UserID: f.employee.ID, CanSend: true}))
			}
			if mode == "expired-before-replay" {
				hours := 1
				var err error
				mailbox, err = f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "receipt-finite", Kind: "shared", RetentionHours: &hours})
				must(t, err)
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mailbox.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			}
			h, _ := r5OrdinaryOutboundRouter(t, f, 0)
			token := r3Token(t, f.employee)
			d := r5ReceiptHTTPDraft(t, h, token, mailbox.ID, company.DraftPayload{To: []string{"to@fixture.test"}, CC: []string{"cc@fixture.test"}, BCC: []string{"hidden@fixture.test"}, Subject: "PRIVATE_SUBJECT", TextBody: "PRIVATE_BODY", HTMLBody: "<b>PRIVATE_HTML</b>", Headers: map[string]string{"X-Private": "PRIVATE_HEADER"}})
			first := draftSubmitHTTP(t, h, token, d.ID.String(), "r5-safe-receipt", d.Revision)
			if first.Code != 201 {
				code, label := r5ReceiptHTTPFailureLabel(first.Body.Bytes())
				t.Fatalf("actual fresh submit status=%d code=%s classified_message=%s", first.Code, code, label)
			}
			v := r5StrictOutboundReceiptJSON(t, first.Body.Bytes(), nil)
			if v.ID == uuid.Nil || v.Progress.Counts == nil || v.Progress.Counts.Total != 3 || v.Status != "submitted" || v.Capabilities == nil || v.Capabilities.ViewContent != (mode != "send-only-shared") {
				t.Fatal("fresh committed receipt lost truthful evidence/current authority")
			}
			job, err := f.st.GetOutboundJob(ctx, v.ID)
			must(t, err)
			if job == nil || job.Subject != "PRIVATE_SUBJECT" || len(job.BCC) != 1 || job.TextBody != "PRIVATE_BODY" {
				t.Fatal("response projection modified committed content")
			}
			if mode == "expired-before-replay" {
				var finite bool
				must(t, f.pool.QueryRow(ctx, `SELECT m.mailbox_kind='shared' AND m.retention_hours_override=1
      AND i.expires_at IS NOT NULL AND i.expires_at=a.created_at+make_interval(hours=>m.retention_hours_override)
      AND i.expires_at>clock_timestamp()
      FROM sent_mail_items i JOIN sent_mail_assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id
      JOIN mailboxes m ON m.tenant_id=i.tenant_id AND m.id=i.mailbox_id
      WHERE i.tenant_id=$1 AND i.asset_id=$2 AND i.mailbox_id=$3`, f.tenant.ID, v.ID, mailbox.ID).Scan(&finite))
				if !finite {
					t.Fatal("archive trigger did not create configured finite retention snapshot")
				}
				content := r3Data[company.SubmissionContent](t, r3HTTP(t, h, token, "GET", "/api/v1/company/submissions/"+v.ID.String()+"/content", nil, 200))
				if content.TextBody != "PRIVATE_BODY" {
					t.Fatal("live configured reader was not authorized on actual content route")
				}
				// This fixture advances only an ALREADY FINITE, trigger-produced item
				// deadline. It never forges an expiry on a permanent personal mailbox.
				tag, err := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND asset_id=$2 AND mailbox_id=$3 AND expires_at IS NOT NULL`, f.tenant.ID, v.ID, mailbox.ID)
				must(t, err)
				if tag.RowsAffected() != 1 {
					t.Fatal("finite fixture did not advance exactly its original sent-item deadline")
				}
			}
			before := r5ReplayCounts(t, f)
			repeated := draftSubmitHTTP(t, h, token, d.ID.String(), "r5-safe-receipt", d.Revision)
			if repeated.Code != 200 {
				code, label := r5ReceiptHTTPFailureLabel(repeated.Body.Bytes())
				t.Fatalf("actual replay status=%d code=%s classified_message=%s", repeated.Code, code, label)
			}
			again := r5StrictOutboundReceiptJSON(t, repeated.Body.Bytes(), &v.ID)
			if again.Capabilities == nil || again.Capabilities.ViewContent != (mode == "owner-live") || again.Progress.Counts == nil || again.Progress.Counts.Total != 3 {
				t.Fatal("replay confused historical command receipt and content authority")
			}
			if before != r5ReplayCounts(t, f) {
				t.Fatal("receipt replay repeated committed effects")
			}
			if mode == "expired-before-replay" {
				r3HTTP(t, h, token, "GET", "/api/v1/company/submissions/"+v.ID.String()+"/content", nil, 404)
			}
		})
	}
}

func TestR5OutboundReceiptPrivacyHTTPRetry(t *testing.T) {
	for _, mode := range []string{"normal", "display-failure-after-commit"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			_, err := f.pool.Exec(context.Background(), `UPDATE outbound_jobs SET state='failed' WHERE id=$1`, j.ID)
			must(t, err)
			_, err = f.pool.Exec(context.Background(), `UPDATE outbound_recipients SET state='temporary' WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0])
			must(t, err)
			failAt := 0
			if mode != "normal" {
				failAt = 2
			}
			h, fault := r5OrdinaryOutboundRouter(t, f, failAt)
			w := r3HTTP(t, h, r3Token(t, f.employee), "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil, 200)
			v := r5StrictOutboundReceiptJSON(t, w.Body.Bytes(), &j.ID)
			current, err := f.st.GetOutboundJob(context.Background(), j.ID)
			must(t, err)
			if current == nil || current.State != models.OutboundPending {
				t.Fatal("actual retry did not commit before response")
			}
			if mode == "normal" {
				if v.State != models.OutboundPending || v.Progress.Counts == nil || v.Progress.Counts.Total != 3 || v.Capabilities == nil || !v.Capabilities.ViewContent {
					t.Fatal("normal retry lost actual receipt evidence")
				}
			} else if v.TenantID != nil || v.Progress.Completeness != "unknown" || v.Progress.Counts != nil || v.Capabilities == nil || v.Capabilities.ViewContent || v.Capabilities.Retry || fault.calls != 2 {
				t.Fatal("committed retry display failure returned raw/fabricated receipt")
			}
		})
	}
}

func TestR5OutboundReceiptPrivacyHTTPCommitVersusDisplayFailure(t *testing.T) {
	for _, mode := range []string{"display-fails", "commit-fails"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			subject := "PRIVATE_SUBJECT"
			if mode == "commit-fails" {
				subject = "PRIVATE_COMMIT_FAILURE"
				_, err := f.pool.Exec(ctx, `ALTER TABLE outbound_jobs ADD CONSTRAINT r5_receipt_commit_failure CHECK(subject<>'PRIVATE_COMMIT_FAILURE') NOT VALID`)
				must(t, err)
			}
			h, fault := r5OrdinaryOutboundRouter(t, f, 1)
			token := r3Token(t, f.employee)
			d := r5ReceiptHTTPDraft(t, h, token, f.personal.ID, company.DraftPayload{To: []string{"to@fixture.test"}, BCC: []string{"hidden@fixture.test"}, Subject: subject, TextBody: "PRIVATE_BODY"})
			before := r5ReplayCounts(t, f)
			w := draftSubmitHTTP(t, h, token, d.ID.String(), "commit-display-boundary", d.Revision)
			if mode == "commit-fails" {
				if w.Code != 500 || fault.calls != 0 || before != r5ReplayCounts(t, f) {
					code, label := r5ReceiptHTTPFailureLabel(w.Body.Bytes())
					t.Fatalf("actual failed commit became a successful fallback or persisted effects: status=%d code=%s classified_message=%s display_calls=%d", w.Code, code, label, fault.calls)
				}
				if strings.Contains(w.Body.String(), "PRIVATE") {
					t.Fatal("commit failure exposed database/caller diagnostics")
				}
				var drafts int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, d.ID).Scan(&drafts))
				if drafts != 1 {
					t.Fatal("failed commit consumed draft")
				}
				return
			}
			if w.Code != 201 {
				code, label := r5ReceiptHTTPFailureLabel(w.Body.Bytes())
				t.Fatalf("display failure falsely reported failed committed submit: status=%d code=%s classified_message=%s", w.Code, code, label)
			}
			v := r5StrictOutboundReceiptJSON(t, w.Body.Bytes(), nil)
			if v.ID == uuid.Nil || v.TenantID != nil || v.Progress.Completeness != "unknown" || v.Progress.Counts != nil || v.Capabilities == nil || v.Capabilities.ViewContent || v.Capabilities.Retry || fault.calls != 1 {
				t.Fatal("post-commit display failure fabricated authority/outcomes")
			}
			job, err := f.st.GetOutboundJob(ctx, v.ID)
			must(t, err)
			if job == nil || job.Subject != subject {
				t.Fatal("minimal fallback did not correspond to an actual committed command")
			}
			after := r5ReplayCounts(t, f)
			if after[0] != before[0]+1 || after[1] != before[1]+1 || after[2] != before[2]+2 || after[3] != before[3]+1 {
				t.Fatal("committed fallback lost atomic command effects")
			}
		})
	}
}

func TestR5OutboundReceiptPrivacyPGSnapshotAndUnknownLedger(t *testing.T) {
	for _, mode := range []string{"private-failure", "private-uncertain", "missing-ledger"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			if mode == "missing-ledger" {
				_, err := f.pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, j.ID)
				must(t, err)
			} else {
				state := "permanent"
				if mode == "private-uncertain" {
					state = "uncertain"
				}
				_, err := f.pool.Exec(ctx, `UPDATE outbound_recipients SET state=$3 WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0], state)
				must(t, err)
			}
			r, err := f.st.GetOutboundReceipt(ctx, f.u, j.ID, "send:read")
			must(t, err)
			if r == nil || r.Job == nil {
				t.Fatal("current authorized snapshot missing")
			}
			v := company.ProjectOutboundReceipt(r.Job, r.RecipientStates, r.LedgerKnown)
			raw, err := json.Marshal(struct {
				Data *company.OutboundReceipt `json:"data"`
			}{v})
			must(t, err)
			r5StrictOutboundReceiptJSON(t, raw, &j.ID)
			if mode == "missing-ledger" {
				if v.Status != "needs_attention" || v.Progress.Completeness != "unknown" || v.Progress.Counts != nil {
					t.Fatal("missing ledger falsely claimed complete success")
				}
			} else if v.Progress.Counts == nil || v.Progress.Counts.Total != 3 {
				t.Fatal("authorized snapshot omitted private outcome")
			}
			if mode == "private-uncertain" && (!v.DeliveryUncertain || v.Status != "needs_attention") {
				t.Fatal("hidden uncertain outcome disappeared")
			}
		})
	}
}
