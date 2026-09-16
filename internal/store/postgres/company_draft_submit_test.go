package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// draftSubmitHTTP POSTs the draft submit endpoint with an explicit
// Idempotency-Key and returns the raw recorder for status/body assertions.
func draftSubmitHTTP(t *testing.T, h http.Handler, token, draftID, key string, revision int) *httptest.ResponseRecorder {
	t.Helper()
	raw, e := json.Marshal(map[string]int{"expected_revision": revision})
	must(t, e)
	r := httptest.NewRequest("POST", "/api/v1/company/drafts/"+draftID+"/submit", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func seedDraftSubmit(t *testing.T, f *companyFixture) http.Handler {
	t.Helper()
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1, RelayTLS: "none", MaxRetries: 3}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj)
	return companyRouter(t, f, obj, svc)
}

func TestDraftSubmitConsumesDraftAtomically(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)

	att, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "quote.txt", Size: 5})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, att.ID, company.Hash("hello")))

	draft := company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{
		To: []string{"client@recipient.test"}, CC: []string{"cc@recipient.test"}, Subject: "Quote",
		TextBody: "see attachment", AttachmentIDs: []uuid.UUID{att.ID},
	}}
	saved := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", draft, 200))

	w := draftSubmitHTTP(t, h, token, saved.ID.String(), "draft-key-1", saved.Revision)
	if w.Code != 201 {
		t.Fatalf("submit: wanted 201 got %d: %s", w.Code, w.Body.String())
	}
	jobID := r3Data[struct {
		ID uuid.UUID `json:"id"`
	}](t, w).ID
	if jobID == uuid.Nil {
		t.Fatal("submit returned no job id")
	}

	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, saved.ID).Scan(&n))
	if n != 0 {
		t.Fatal("draft survived a successful submit")
	}
	job, e := f.st.GetOutboundJob(ctx, jobID)
	must(t, e)
	if job == nil || job.TenantID != f.tenant.ID || job.MailFrom != f.personal.FullAddress {
		t.Fatalf("job not attributed to the draft mailbox: %+v", job)
	}
	if job.DraftID == nil || *job.DraftID != saved.ID {
		t.Fatalf("job lost draft provenance: %+v", job.DraftID)
	}
	if len(job.To) != 1 || job.To[0] != "client@recipient.test" || len(job.CC) != 1 || job.CC[0] != "cc@recipient.test" || job.Subject != "Quote" {
		t.Fatalf("draft payload not projected into job: %+v", job)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_recipients WHERE job_id=$1`, jobID).Scan(&n))
	if n != 2 {
		t.Fatalf("recipient ledger seeds=%d", n)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_attachments WHERE job_id=$1`, jobID).Scan(&n))
	if n != 1 {
		t.Fatalf("attachment pins=%d", n)
	}

	// A lost response retried with the same Idempotency-Key replays the same job.
	w2 := draftSubmitHTTP(t, h, token, saved.ID.String(), "draft-key-1", saved.Revision)
	if w2.Code != 200 {
		t.Fatalf("idempotent replay: wanted 200 got %d: %s", w2.Code, w2.Body.String())
	}
	replayID := r3Data[struct {
		ID uuid.UUID `json:"id"`
	}](t, w2).ID
	if replayID != jobID {
		t.Fatalf("replay created a different job %s vs %s", replayID, jobID)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, saved.ID).Scan(&n))
	if n != 1 {
		t.Fatalf("jobs for draft=%d", n)
	}
}

func TestDraftSubmitConcurrentWindowsProduceOneJob(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)

	draft := company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{
		To: []string{"client@recipient.test"}, Subject: "Race", TextBody: "body",
	}}
	saved := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", draft, 200))

	type result struct {
		code int
		body []byte
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, key := range []string{"window-a", "window-b"} {
		go func(key string) {
			<-start
			w := draftSubmitHTTP(t, h, token, saved.ID.String(), key, saved.Revision)
			results <- result{w.Code, w.Body.Bytes()}
		}(key)
	}
	close(start)
	created := 0
	conflicted := 0
	for i := 0; i < 2; i++ {
		r := <-results
		switch r.code {
		case 201:
			created++
		case 409:
			conflicted++
		default:
			t.Fatalf("unexpected status %d: %s", r.code, r.body)
		}
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("race outcome created=%d conflicted=%d", created, conflicted)
	}
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, saved.ID).Scan(&n))
	if n != 1 {
		t.Fatalf("race produced %d jobs", n)
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, saved.ID).Scan(&n))
	if n != 0 {
		t.Fatal("draft survived the race")
	}
	// A third window with yet another key after consumption gets the explicit
	// already-submitted conflict, never a silent 404.
	w := draftSubmitHTTP(t, h, token, saved.ID.String(), "window-c", saved.Revision)
	if w.Code != 409 {
		t.Fatalf("post-consumption submit: wanted 409 got %d: %s", w.Code, w.Body.String())
	}
}

func TestDraftSubmitRevisionMismatchKeepsDraft(t *testing.T) {
	f := seedCompany(t)
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)

	draft := company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{
		To: []string{"client@recipient.test"}, Subject: "Stale", TextBody: "body",
	}}
	saved := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", draft, 200))

	w := draftSubmitHTTP(t, h, token, saved.ID.String(), "stale-key", saved.Revision+1)
	if w.Code != 409 {
		t.Fatalf("stale revision: wanted 409 got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Revision int `json:"revision"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &resp))
	if resp.Data.Revision != saved.Revision {
		t.Fatalf("conflict did not return current revision: %s", w.Body.String())
	}
	ctx := context.Background()
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, saved.ID).Scan(&n))
	if n != 1 {
		t.Fatal("stale submit consumed the draft")
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, saved.ID).Scan(&n))
	if n != 0 {
		t.Fatal("stale submit created a job")
	}
}

func TestDraftSubmitRevokedSendRightKeepsDraft(t *testing.T) {
	f := seedCompany(t)
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)
	ctx := context.Background()

	// Draft on the shared mailbox via an explicit grant, then revoke the grant.
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: true}))
	draft := company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{
		To: []string{"client@recipient.test"}, Subject: "Grant", TextBody: "body",
	}}
	saved := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", draft, 200))
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: false}))

	w := draftSubmitHTTP(t, h, token, saved.ID.String(), "revoked-key", saved.Revision)
	if w.Code != 403 {
		t.Fatalf("revoked grant: wanted 403 got %d: %s", w.Code, w.Body.String())
	}
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, saved.ID).Scan(&n))
	if n != 1 {
		t.Fatal("rejected submit consumed the draft")
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, saved.ID).Scan(&n))
	if n != 0 {
		t.Fatal("rejected submit created a job")
	}
}

func TestCreateOutboundJobConsumeDraftSingleConsumption(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	mb, e := f.st.GetMailboxByAddress(ctx, "employee@company.test")
	must(t, e)

	draftID := uuid.New()
	_, e = f.pool.Exec(ctx, `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload,revision) VALUES($1,$2,$3,$4,$5,1)`,
		draftID, f.tenant.ID, f.employee.ID, mb.ID, []byte(`{"to":["client@recipient.test"],"subject":"Store"}`))
	must(t, e)

	newJob := func(key string) *models.OutboundJob {
		return &models.OutboundJob{
			ID: uuid.New(), TenantID: f.tenant.ID, UserID: &f.employee.ID, MailFrom: mb.FullAddress,
			RcptTo: []string{"client@recipient.test"}, To: []string{"client@recipient.test"},
			Subject: "Store", TextBody: "body", ZoneID: f.zone.ID,
			IdempotencyKey: key, SubmitActor: "user:" + f.employee.ID.String(), RequestHash: "hash-" + key,
		}
	}
	consumption := store.DraftConsumption{TenantID: f.tenant.ID, UserID: f.employee.ID, ID: draftID, Revision: 1}

	job := newJob("store-a")
	replayed, e := f.st.CreateOutboundJobConsumeDraft(ctx, job, store.OutboundQuotaReservation{}, consumption)
	must(t, e)
	if replayed || job.ID == uuid.Nil {
		t.Fatalf("first consumption: replayed=%v job=%+v", replayed, job)
	}
	jobID := job.ID
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, draftID).Scan(&n))
	if n != 0 {
		t.Fatal("draft survived consumption")
	}
	// A second enqueue against the same revision must roll back entirely.
	job2 := newJob("store-b")
	replayed2, e := f.st.CreateOutboundJobConsumeDraft(ctx, job2, store.OutboundQuotaReservation{}, consumption)
	if !errors.Is(e, store.ErrDraftAlreadyConsumed) {
		t.Fatalf("second consumption: wanted ErrDraftAlreadyConsumed got %v", e)
	}
	if replayed2 {
		t.Fatal("second consumption misreported as idempotent replay")
	}
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, draftID).Scan(&n))
	if n != 1 {
		t.Fatalf("rollback left %d jobs for the draft", n)
	}
	// Replaying the winning key returns the original job, not a new one.
	job3 := newJob("store-a")
	replayed3, e := f.st.CreateOutboundJobConsumeDraft(ctx, job3, store.OutboundQuotaReservation{}, consumption)
	must(t, e)
	if !replayed3 || job3.ID != jobID {
		t.Fatalf("idempotent replay after consumption: replayed=%v job=%+v", replayed3, job3)
	}
}
