package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func r5ReplayDraft(t *testing.T, f *companyFixture) *company.Draft {
	t.Helper()
	d, e := f.st.SaveMailDraft(context.Background(), f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{
		To: []string{"visible@replay.test"}, BCC: []string{"REPLAY_PRIVATE@hidden.test"}, Subject: "safe replay receipt", TextBody: "REPLAY_PRIVATE_BODY", HTMLBody: "<b>REPLAY_PRIVATE_HTML</b>", Headers: map[string]string{"X-Private": "REPLAY_PRIVATE_HEADER"},
	}})
	must(t, e)
	return d
}

func r5ReplayRequest(ctx context.Context, h http.Handler, token string, d *company.Draft, key string) (<-chan error, *httptest.ResponseRecorder) {
	raw, _ := json.Marshal(map[string]int{"expected_revision": d.Revision})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/company/drafts/"+d.ID.String()+"/submit", bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	rr := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { h.ServeHTTP(rr, req); done <- nil }()
	return done, rr
}

func r5ReplayCounts(t *testing.T, f *companyFixture) [4]int {
	t.Helper()
	var v [4]int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM outbound_jobs),(SELECT count(*) FROM sent_mail_assets),(SELECT count(*) FROM outbound_recipients),(SELECT count(*) FROM audit_log WHERE action='outbound.submit')`).Scan(&v[0], &v[1], &v[2], &v[3]))
	return v
}

func r5ReplaySafeReceipt(t *testing.T, rr *httptest.ResponseRecorder, id uuid.UUID) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("historical replay requires 200, got %d %s", rr.Code, rr.Body.String())
	}
	got := r3Data[models.OutboundJob](t, rr)
	if got.ID != id {
		t.Fatalf("replay changed durable job: %s vs %s", got.ID, id)
	}
	if strings.Contains(strings.ToUpper(rr.Body.String()), "REPLAY_PRIVATE") {
		t.Fatal("expired sent content or hidden recipient leaked through POST replay")
	}
}

// The missing consumed draft is the normal production POST replay path.
// A lookup-table barrier is after GetMailDraft's authorization transaction,
// so this checks identity again after middleware and the initial user fence.
func TestR5SubmissionConsumedReplayRechecksIdentity(t *testing.T) {
	for _, mode := range []string{"live", "session-revoke", "freeze-reactivate"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			h := seedDraftSubmit(t, f)
			d := r5ReplayDraft(t, f)
			token := r3Token(t, f.employee)
			first := draftSubmitHTTP(t, h, token, d.ID.String(), "replay-identity", d.Revision)
			if first.Code != http.StatusCreated {
				t.Fatalf("initial submit=%d %s", first.Code, first.Body.String())
			}
			id := r3Data[models.OutboundJob](t, first).ID
			before := r5ReplayCounts(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			gate, e := f.pool.Begin(ctx)
			must(t, e)
			defer gate.Rollback(context.Background())
			_, e = gate.Exec(ctx, `LOCK TABLE outbound_jobs IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			done, rr := r5ReplayRequest(ctx, h, token, d, "replay-identity")
			r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "draft_id=$2")
			if mode == "session-revoke" {
				_, e = gate.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.employee.ID)
				must(t, e)
			} else if mode == "freeze-reactivate" {
				// Only fixture scheduling uses SQL; the real Router performs
				// both submission and consumed-receipt handling.
				_, e = gate.Exec(ctx, `UPDATE users SET is_active=false,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
				must(t, e)
				_, e = gate.Exec(ctx, `UPDATE users SET is_active=true,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
				must(t, e)
			}
			must(t, gate.Commit(ctx))
			r5AwaitOperation(t, ctx, done)
			want := http.StatusForbidden
			if mode == "live" {
				want = http.StatusOK
			}
			if rr.Code != want {
				t.Fatalf("%s replay status=%d want %d: %s", mode, rr.Code, want, rr.Body.String())
			}
			if mode == "live" && r3Data[models.OutboundJob](t, rr).ID != id {
				t.Fatal("live replay replaced job")
			}
			if r5ReplayCounts(t, f) != before {
				t.Fatal("identity replay created another job/asset/recipient/audit")
			}
		})
	}
}

func TestR5SubmissionReplayExpiresContentWithoutReenqueuing(t *testing.T) {
	for _, branch := range []string{"consumed", "outer-found"} {
		t.Run(branch, func(t *testing.T) {
			f := seedCompany(t)
			h := seedDraftSubmit(t, f)
			d := r5ReplayDraft(t, f)
			token := r3Token(t, f.employee)
			first := draftSubmitHTTP(t, h, token, d.ID.String(), "replay-content", d.Revision)
			if first.Code != http.StatusCreated {
				t.Fatalf("initial submit=%d %s", first.Code, first.Body.String())
			}
			id := r3Data[models.OutboundJob](t, first).ID
			before := r5ReplayCounts(t, f)
			_, e := f.pool.Exec(context.Background(), `UPDATE sent_mail_items SET expires_at=clock_timestamp()-interval '1 second' WHERE asset_id=$1`, id)
			must(t, e)
			if branch == "consumed" {
				// A history receipt does not cause a new send. Disabling future
				// sending must not discard this submitter's safe expired receipt.
				_, e = f.pool.Exec(context.Background(), `UPDATE mailboxes SET send_policy='disabled' WHERE id=$1`, d.MailboxID)
				must(t, e)
			}
			if branch == "outer-found" {
				// SQL restores only this already-consumed draft fixture to
				// reach the outer FindOutboundSubmission branch through the
				// actual Router. There is no public draft restoration API.
				raw, e := json.Marshal(d.Payload)
				must(t, e)
				_, e = f.pool.Exec(context.Background(), `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload,revision) VALUES($1,$2,$3,$4,$5,$6)`, d.ID, f.tenant.ID, f.employee.ID, d.MailboxID, raw, d.Revision)
				must(t, e)
			}
			rr := draftSubmitHTTP(t, h, token, d.ID.String(), "replay-content", d.Revision)
			r5ReplaySafeReceipt(t, rr, id)
			if r5ReplayCounts(t, f) != before {
				t.Fatal("expired-content replay created another durable submission")
			}
		})
	}
}

func TestR5SubmissionReplayRejectsChangedIntent(t *testing.T) {
	for _, mode := range []string{"consumed-different-key", "outer-different-payload"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			h := seedDraftSubmit(t, f)
			d := r5ReplayDraft(t, f)
			token := r3Token(t, f.employee)
			first := draftSubmitHTTP(t, h, token, d.ID.String(), "replay-intent", d.Revision)
			if first.Code != http.StatusCreated {
				t.Fatalf("initial submit=%d %s", first.Code, first.Body.String())
			}
			before := r5ReplayCounts(t, f)
			key := "different-consumption-intent"
			if mode == "outer-different-payload" {
				key = "replay-intent"
				d.Payload.Subject = "different payload intent"
				raw, e := json.Marshal(d.Payload)
				must(t, e)
				// Test-only source restoration, not a product recovery API.
				_, e = f.pool.Exec(context.Background(), `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload,revision) VALUES($1,$2,$3,$4,$5,$6)`, d.ID, f.tenant.ID, f.employee.ID, d.MailboxID, raw, d.Revision)
				must(t, e)
			}
			rr := draftSubmitHTTP(t, h, token, d.ID.String(), key, d.Revision)
			if rr.Code != http.StatusConflict {
				t.Fatalf("changed replay intent must conflict, got %d %s", rr.Code, rr.Body.String())
			}
			if r5ReplayCounts(t, f) != before {
				t.Fatal("changed intent modified or duplicated original submission")
			}
		})
	}
}

// Two real POSTs both miss the outer lookup before either enqueue acquires
// its tenant parent. One creates the job; the other takes the inner existing
// receipt branch. An isolated trigger expires the newly-created sent item.
func TestR5SubmissionInnerConcurrentReplayUsesSafeReceipt(t *testing.T) {
	f := seedCompany(t)
	h := seedDraftSubmit(t, f)
	d := r5ReplayDraft(t, f)
	token := r3Token(t, f.employee)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, e := f.pool.Exec(ctx, `CREATE FUNCTION r5_expire_replay_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.expires_at=clock_timestamp()-interval '1 second'; RETURN NEW; END $$; CREATE TRIGGER r5_expire_replay_item BEFORE INSERT ON sent_mail_items FOR EACH ROW EXECUTE FUNCTION r5_expire_replay_item()`)
	must(t, e)
	gate, e := f.pool.Begin(ctx)
	must(t, e)
	defer gate.Rollback(context.Background())
	_, e = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, e)
	first, one := r5ReplayRequest(ctx, h, token, d, "replay-inner")
	second, two := r5ReplayRequest(ctx, h, token, d, "replay-inner")
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var n int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT id FROM tenants%'`, int32(gate.Conn().PgConn().PID())).Scan(&n))
		if n == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both Router submissions never reached enqueue parent wait")
		case <-ticker.C:
		}
	}
	must(t, gate.Rollback(ctx))
	r5AwaitOperation(t, ctx, first)
	r5AwaitOperation(t, ctx, second)
	created, replayed := one, two
	if one.Code == http.StatusOK {
		created, replayed = two, one
	}
	if created.Code != http.StatusCreated || replayed.Code != http.StatusOK {
		t.Fatalf("concurrent create/replay statuses=%d/%d bodies=%s/%s", one.Code, two.Code, one.Body.String(), two.Body.String())
	}
	id := r3Data[models.OutboundJob](t, created).ID
	r5ReplaySafeReceipt(t, replayed, id)
	if counts := r5ReplayCounts(t, f); counts != [4]int{1, 1, 2, 1} {
		t.Fatalf("concurrent replay duplicated submission effects: %v", counts)
	}
}

func TestR5SubmissionCompanyPostRejectsAPIKeyReplay(t *testing.T) {
	f := seedCompany(t)
	h := seedDraftSubmit(t, f)
	d := r5ReplayDraft(t, f)
	token := r3Token(t, f.employee)
	first := draftSubmitHTTP(t, h, token, d.ID.String(), "replay-key-route", d.Revision)
	if first.Code != http.StatusCreated {
		t.Fatalf("initial submit=%d", first.Code)
	}
	k, _ := r5LegacyKey(t, f)
	before := r5ReplayCounts(t, f)
	// Removing send:write retains a supported read-only scope. Empty scopes
	// violate the production CHECK and are not a valid revocation fixture.
	for _, scopes := range []string{`["send:write"]`, `["send:read"]`} {
		_, e := f.pool.Exec(context.Background(), `UPDATE tenant_api_keys SET scopes=$2::jsonb WHERE id=$1`, k.ID, scopes)
		must(t, e)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/company/drafts/"+d.ID.String()+"/submit", strings.NewReader(`{"expected_revision":1}`))
		r.Header.Set("X-API-Key", "tm_content_"+k.ID.String())
		r.Header.Set("Idempotency-Key", "replay-key-route")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("company POST must require JWT even before/after Key scope revocation: %d %s", rr.Code, rr.Body.String())
		}
	}
	if r5ReplayCounts(t, f) != before {
		t.Fatal("Key POST changed historical submission")
	}
}
