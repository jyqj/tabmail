package postgres_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Use the reservation/completion commands, then sort their actual IDs so the
// protected prefix and later orphan match the shipping ORDER BY id exactly.
func r5GCProgressAttachments(t *testing.T, f *companyFixture, ctx context.Context, n int) []*company.Attachment {
	t.Helper()
	items := make([]*company.Attachment, 0, n)
	for i := 0; i < n; i++ {
		a, err := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: fmt.Sprintf("gc-progress-%d.txt", i), Size: 1})
		must(t, err)
		must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("x")))
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })
	return items
}

func r5GCProgressProtect(t *testing.T, f *companyFixture, ctx context.Context, items []*company.Attachment, kind string) {
	t.Helper()
	for start := 0; start < len(items); start += 10 {
		end := start + 10
		if end > len(items) {
			end = len(items)
		}
		ids := make([]uuid.UUID, 0, end-start)
		for _, a := range items[start:end] {
			ids = append(ids, a.ID)
		}
		if kind == "draft" {
			d, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "protected GC prefix", AttachmentIDs: ids}})
			must(t, err)
			// Sealing does not release JSON references or make them collectible.
			_, err = f.pool.Exec(ctx, `UPDATE mail_drafts SET sealed_at=clock_timestamp() WHERE id=$1`, d.ID)
			must(t, err)
			continue
		}
		job := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"recipient@gc-progress.test"}, To: []string{"recipient@gc-progress.test"}, AttachmentIDs: ids, Subject: "protected GC prefix", State: models.OutboundPending}
		must(t, f.st.CreateOutboundJob(ctx, job))
		switch kind {
		case "outbound":
			// The real enqueue created both pins. Remove only the sent relation
			// in this isolated fixture to exercise the outbound exclusion alone.
			_, err := f.pool.Exec(ctx, `DELETE FROM sent_asset_attachments WHERE asset_id=$1`, job.ID)
			must(t, err)
		case "sent":
			// Actual queue cleanup leaves the independently established sent pin.
			_, err := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, job.ID)
			must(t, err)
		default:
			t.Fatalf("unknown GC fixture protection %q", kind)
		}
	}
}

func r5GCProgressExpire(t *testing.T, f *companyFixture, ctx context.Context) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 hour' WHERE tenant_id=$1`, f.tenant.ID)
	must(t, err)
}

func TestR5AttachmentGCProgressPastProtectedPrefix(t *testing.T) {
	for _, kind := range []string{"draft", "outbound", "sent"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			items := r5GCProgressAttachments(t, f, ctx, 101)
			r5GCProgressProtect(t, f, ctx, items[:100], kind)
			r5GCProgressExpire(t, f, ctx)
			must(t, f.st.SweepCompanyMetadata(ctx))
			for _, a := range items[:100] {
				r5ReferenceCounts(t, f, ctx, a, 1, 0)
			}
			r5ReferenceCounts(t, f, ctx, items[100], 0, 1)
			// Repeated sweeps do not erase the protected prefix or duplicate work.
			must(t, f.st.SweepCompanyMetadata(ctx))
			r5ReferenceCounts(t, f, ctx, items[0], 1, 0)
			r5ReferenceCounts(t, f, ctx, items[100], 0, 1)
		})
	}
}

func TestR5AttachmentGCProgressAllProtectedTerminates(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 110)
	r5GCProgressProtect(t, f, ctx, items, "draft")
	r5GCProgressExpire(t, f, ctx)
	must(t, f.st.SweepCompanyMetadata(ctx))
	for _, a := range items {
		r5ReferenceCounts(t, f, ctx, a, 1, 0)
	}
}

func TestR5AttachmentGCProgressBusyRowsAndBatchLimit(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 125)
	r5GCProgressExpire(t, f, ctx)
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM mail_attachments WHERE id=$1 FOR SHARE`, items[0].ID)
	must(t, err)
	must(t, f.st.SweepCompanyMetadata(ctx))
	for i, a := range items {
		if i > 0 && i <= 100 {
			r5ReferenceCounts(t, f, ctx, a, 0, 1)
		} else {
			r5ReferenceCounts(t, f, ctx, a, 1, 0)
		}
	}
	must(t, hold.Rollback(ctx))
	must(t, f.st.SweepCompanyMetadata(ctx))
	for _, a := range items {
		r5ReferenceCounts(t, f, ctx, a, 0, 1)
	}
}

func TestR5AttachmentGCProgressConcurrentDraftFence(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 2)
	d, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "empty draft"}})
	must(t, err)
	// The writer validates while the attachment is live, takes FOR SHARE, and
	// then waits on the existing draft. Its first reference is not yet visible.
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, items[0].ID).Scan(&deadline))
	_, err = f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, items[1].ID)
	must(t, err)
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, d.ID)
	must(t, err)
	d.Payload.AttachmentIDs = []uuid.UUID{items[0].ID}
	done := make(chan error, 1)
	go func() { _, e := f.st.SaveMailDraft(ctx, f.u, *d); done <- e }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "UPDATE mail_drafts")
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1`, deadline).Scan(&expired))
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("server deadline was not reached")
		case <-ticker.C:
		}
	}
	// GC must skip the in-flight confirmed attachment and still collect the
	// unrelated orphan; a tenant-only lock would not protect this writer.
	must(t, f.st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, f, ctx, items[0], 1, 0)
	r5ReferenceCounts(t, f, ctx, items[1], 0, 1)
	must(t, hold.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	must(t, f.st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, f, ctx, items[0], 1, 0)
}

func TestR5AttachmentGCProgressOrphanFailureRollsBackBatch(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 102)
	r5GCProgressProtect(t, f, ctx, items[:100], "draft")
	r5GCProgressExpire(t, f, ctx)
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION r5_gc_progress_reject_orphan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled GC progress orphan failure'; END $$;
 CREATE TRIGGER r5_gc_progress_reject_orphan BEFORE INSERT ON orphan_objects FOR EACH ROW EXECUTE FUNCTION r5_gc_progress_reject_orphan()`)
	must(t, err)
	if err = f.st.SweepCompanyMetadata(ctx); err == nil {
		t.Fatal("orphan registration failure was swallowed")
	}
	for _, a := range items {
		r5ReferenceCounts(t, f, ctx, a, 1, 0)
	}
	_, err = f.pool.Exec(ctx, `DROP TRIGGER r5_gc_progress_reject_orphan ON orphan_objects`)
	must(t, err)
	must(t, f.st.SweepCompanyMetadata(ctx))
	for _, a := range items[:100] {
		r5ReferenceCounts(t, f, ctx, a, 1, 0)
	}
	for _, a := range items[100:] {
		r5ReferenceCounts(t, f, ctx, a, 0, 1)
	}
}

func TestR5AttachmentGCProgressReferenceLookupFailureIsClosed(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 1)
	r5GCProgressExpire(t, f, ctx)
	// Unknown reference eligibility is an SQL error, never an empty pin set.
	// This schema fault exists only in this test's disposable database.
	_, err := f.pool.Exec(ctx, `ALTER TABLE sent_asset_attachments RENAME COLUMN attachment_id TO r5_unavailable_attachment_id`)
	must(t, err)
	if err = f.st.SweepCompanyMetadata(ctx); err == nil {
		t.Fatal("unavailable reference authority was treated as unreferenced")
	}
	r5ReferenceCounts(t, f, ctx, items[0], 1, 0)
	_, err = f.pool.Exec(ctx, `ALTER TABLE sent_asset_attachments RENAME COLUMN r5_unavailable_attachment_id TO attachment_id`)
	must(t, err)
	must(t, f.st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, f, ctx, items[0], 0, 1)
}

func TestR5AttachmentGCProgressHeldOriginalSurvivesMetadataCollection(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items := r5GCProgressAttachments(t, f, ctx, 1)
	claim, _ := r5ClaimIngress(t, f)
	must(t, f.st.HoldIngressTarget(ctx, claim, f.personal.ID, "Retain original independently of attachment metadata"))
	// Fixture aliases the keys to exercise the raw-object authority. It does
	// not claim the upload command generates an ingress key or grants access.
	_, err := f.pool.Exec(ctx, `UPDATE mail_attachments SET object_key=$2,expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, items[0].ID, claim.Job.RawObjectKey)
	must(t, err)
	items[0].ObjectKey = claim.Job.RawObjectKey
	must(t, f.st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, f, ctx, items[0], 0, 1)
	called := false
	released, err := f.st.ReleaseRawObjectIfUnreferenced(ctx, claim.Job.RawObjectKey, func(context.Context) error { called = true; return nil })
	must(t, err)
	if released || called {
		t.Fatal("metadata collection allowed deletion of a held original")
	}
}
