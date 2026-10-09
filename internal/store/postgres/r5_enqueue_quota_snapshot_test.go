package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// A zero send quota means unlimited, not denied. All limits in these tests
// come from real Router PermissionLoader -> SubmitDraft -> SubmitAuthorized;
// no test manufactures a stale OutboundQuotaReservation.
func r5QuotaDraft(t *testing.T, h http.Handler, f *companyFixture, token string) *company.Draft {
	t.Helper()
	d := r3Data[company.Draft](t, r3HTTP(t, h, token, "POST", "/api/v1/company/drafts", company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{To: []string{"synthetic@quota.test"}, Subject: "quota fixture", TextBody: "synthetic quota fixture"}}, 200))
	return &d
}

func r5QuotaSubmitRequest(t *testing.T, ctx context.Context, h http.Handler, token string, d *company.Draft, key string) (<-chan error, *httptest.ResponseRecorder) {
	t.Helper()
	raw, e := json.Marshal(map[string]int{"expected_revision": d.Revision})
	must(t, e)
	req := httptest.NewRequest("POST", "/api/v1/company/drafts/"+d.ID.String()+"/submit", bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	rr := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { h.ServeHTTP(rr, req); done <- nil }()
	return done, rr
}

func r5QuotaRequireResponse(t *testing.T, rr *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("quota submit status=%d want=%d body=%s", rr.Code, status, rr.Body.String())
	}
	if status == http.StatusTooManyRequests && !strings.Contains(rr.Body.String(), "QUOTA_EXCEEDED") {
		t.Fatalf("non-quota 429 is not a quota proof: %s", rr.Body.String())
	}
}

func r5QuotaRequireEffects(t *testing.T, f *companyFixture, d *company.Draft, jobs, drafts int) {
	t.Helper()
	var counts [5]int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM outbound_recipients WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='outbound.submit'),(SELECT count(*) FROM mail_drafts WHERE id=$2)`, f.tenant.ID, d.ID).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4]))
	for i, n := range counts {
		want := jobs
		if i == 4 {
			want = drafts
		}
		if n != want {
			t.Fatalf("quota effects jobs/assets/recipients/audits/drafts=%v want jobs=%d drafts=%d", counts, jobs, drafts)
		}
	}
}

func TestR5EnqueueQuotaUsesCurrentPermissionAfterParentWait(t *testing.T) {
	for _, mode := range []string{"profile-lower", "unlimited-to-finite", "finite-to-unlimited", "override-insert", "override-delete-finite", "override-delete-unlimited"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			initial := 31
			if mode == "unlimited-to-finite" || mode == "override-delete-unlimited" {
				initial = 0
			}
			if mode == "finite-to-unlimited" || mode == "override-delete-finite" {
				initial = 1
			}
			_, e := f.pool.Exec(ctx, `UPDATE permission_profiles SET daily_send_quota=$2 WHERE id=$1`, p.ID, initial)
			must(t, e)
			if strings.HasPrefix(mode, "override-delete-") {
				value := 31
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, DailySendQuota: &value}))
			}
			h := seedDraftSubmit(t, f)
			token := r3Token(t, f.employee)
			first := r5QuotaDraft(t, h, f, token)
			r5QuotaRequireResponse(t, draftSubmitHTTP(t, h, token, first.ID.String(), "quota-prior", first.Revision), 201)
			next := r5QuotaDraft(t, h, f, token)
			gate, e := f.pool.Begin(ctx)
			must(t, e)
			defer gate.Rollback(context.Background())
			_, e = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
			must(t, e)
			done, rr := r5QuotaSubmitRequest(t, ctx, h, token, next, "quota-current")
			r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "SELECT id FROM tenants")
			switch mode {
			case "profile-lower", "unlimited-to-finite":
				_, e = gate.Exec(ctx, `UPDATE permission_profiles SET daily_send_quota=1 WHERE id=$1`, p.ID)
			case "finite-to-unlimited":
				_, e = gate.Exec(ctx, `UPDATE permission_profiles SET daily_send_quota=0 WHERE id=$1`, p.ID)
			case "override-insert":
				_, e = gate.Exec(ctx, `INSERT INTO user_permission_overrides(user_id,daily_send_quota) VALUES($1,1) ON CONFLICT(user_id) DO UPDATE SET daily_send_quota=1`, f.employee.ID)
			default:
				_, e = gate.Exec(ctx, `DELETE FROM user_permission_overrides WHERE user_id=$1`, f.employee.ID)
			}
			must(t, e)
			must(t, gate.Commit(ctx))
			r5AwaitOperation(t, ctx, done)
			want := http.StatusTooManyRequests
			if mode == "finite-to-unlimited" || mode == "override-delete-unlimited" {
				want = http.StatusCreated
			}
			r5QuotaRequireResponse(t, rr, want)
			jobs, drafts := 1, 1
			if want == http.StatusCreated {
				jobs, drafts = 2, 0
			}
			r5QuotaRequireEffects(t, f, next, jobs, drafts)
			effective, e := f.st.EffectivePermission(ctx, f.employee.ID)
			must(t, e)
			if !effective.CanSend || effective.DailySendQuota != (map[bool]int{true: 0, false: 1}[want == http.StatusCreated]) {
				t.Fatalf("fixture did not retain intended current send quota: %+v", effective)
			}
		})
	}
}

func TestR5EnqueueQuotaChangeOrdersThroughInsert(t *testing.T) {
	for _, initial := range []int{31, 0} {
		t.Run(fmt.Sprintf("initial-%d", initial), func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			p.DailySendQuota = initial
			must(t, f.st.UpdatePermissionProfile(ctx, p))
			h := seedDraftSubmit(t, f)
			token := r3Token(t, f.employee)
			first := r5QuotaDraft(t, h, f, token)
			r5QuotaRequireResponse(t, draftSubmitHTTP(t, h, token, first.ID.String(), "quota-first", first.Revision), 201)
			next := r5QuotaDraft(t, h, f, token)
			gate := r5PauseCommand(t, f, ctx, "outbound_jobs", "INSERT", "ROW")
			done, rr := r5QuotaSubmitRequest(t, ctx, h, token, next, "quota-late")
			writer := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "INSERT INTO outbound_jobs")
			changed := make(chan error, 1)
			go func() { value := *p; value.DailySendQuota = 1; changed <- f.st.UpdatePermissionProfile(ctx, &value) }()
			ordered, early := r5SnapshotWaitOrDone(t, f, ctx, writer, changed)
			must(t, early)
			must(t, gate.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
			if ordered {
				r5AwaitOperation(t, ctx, changed)
				r5QuotaRequireResponse(t, rr, 201)
				r5QuotaRequireEffects(t, f, next, 2, 0)
			} else {
				r5QuotaRequireResponse(t, rr, 429)
				r5QuotaRequireEffects(t, f, next, 1, 1)
			}
			// A late change ordered after this accepted send must govern the next
			// fresh request; it is not allowed to retain the old profile indefinitely.
			final := r5QuotaDraft(t, h, f, token)
			r5QuotaRequireResponse(t, draftSubmitHTTP(t, h, token, final.ID.String(), "quota-after", final.Revision), 429)
			t.Logf("quota_change_waited=%v", ordered)
		})
	}
}

func TestR5EnqueueQuotaUnlimitedToFiniteSharedUser(t *testing.T) {
	f := seedCompany(t)
	p := r5SnapshotProfile(t, f, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, e := f.pool.Exec(ctx, `UPDATE permission_profiles SET daily_send_quota=0 WHERE id=$1`, p.ID)
	must(t, e)
	h := seedDraftSubmit(t, f)
	token := r3Token(t, f.employee)
	drafts := []*company.Draft{r5QuotaDraft(t, h, f, token), r5QuotaDraft(t, h, f, token)}
	gate, e := f.pool.Begin(ctx)
	must(t, e)
	defer gate.Rollback(context.Background())
	_, e = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, e)
	type pending struct {
		done <-chan error
		rr   *httptest.ResponseRecorder
	}
	requests := make([]pending, 2)
	for i, d := range drafts {
		done, rr := r5QuotaSubmitRequest(t, ctx, h, token, d, fmt.Sprintf("quota-concurrent-%d", i))
		requests[i] = pending{done, rr}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND query LIKE '%SELECT id FROM tenants%' AND $1::int=ANY(pg_blocking_pids(pid))`, int32(gate.Conn().PgConn().PID())).Scan(&waiting))
		if waiting == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("two Router submits did not reach the final parent fence")
		case <-ticker.C:
		}
	}
	_, e = gate.Exec(ctx, `UPDATE permission_profiles SET daily_send_quota=1 WHERE id=$1`, p.ID)
	must(t, e)
	must(t, gate.Commit(ctx))
	accepted, rejected := 0, 0
	for i, r := range requests {
		r5AwaitOperation(t, ctx, r.done)
		switch r.rr.Code {
		case 201:
			accepted++
			r5QuotaRequireEffects(t, f, drafts[i], 1, 0)
		case 429:
			rejected++
			r5QuotaRequireResponse(t, r.rr, 429)
			r5QuotaRequireEffects(t, f, drafts[i], 1, 1)
		default:
			t.Errorf("concurrent quota submit: %d %s", r.rr.Code, r.rr.Body.String())
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("current shared-user quota1 admitted %d and quota-rejected %d", accepted, rejected)
	}
}

// A real wall-clock midnight crossing is not simulated here. These fixtures
// freeze historical job timestamps immediately before/at UTC midnight and
// prove the current Router uses that boundary, not a local calendar day.
func TestR5EnqueueQuotaUTCMidnightCountingBoundary(t *testing.T) {
	for _, boundary := range []string{"before", "at"} {
		t.Run(boundary, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			p.DailySendQuota = 1
			must(t, f.st.UpdatePermissionProfile(context.Background(), p))
			h := seedDraftSubmit(t, f)
			token := r3Token(t, f.employee)
			first := r5QuotaDraft(t, h, f, token)
			rr := draftSubmitHTTP(t, h, token, first.ID.String(), "quota-day-first", first.Revision)
			r5QuotaRequireResponse(t, rr, 201)
			id := r3Data[struct {
				ID uuid.UUID `json:"id"`
			}](t, rr).ID
			now := time.Now().UTC()
			midnight := now.Truncate(24 * time.Hour)
			created := midnight
			if boundary == "before" {
				created = midnight.Add(-time.Microsecond)
			}
			_, e := f.pool.Exec(context.Background(), `UPDATE outbound_jobs SET created_at=$2 WHERE id=$1`, id, created)
			must(t, e)
			next := r5QuotaDraft(t, h, f, token)
			want := 429
			if boundary == "before" {
				want = 201
			}
			rr = draftSubmitHTTP(t, h, token, next.ID.String(), "quota-day-next", next.Revision)
			if time.Now().UTC().Truncate(24*time.Hour) != midnight {
				t.Fatal("fixture actually crossed UTC midnight; rerun rather than claim the frozen-day comparison")
			}
			r5QuotaRequireResponse(t, rr, want)
		})
	}
}
