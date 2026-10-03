package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type committedRetryProjectionFailure struct {
	*testutil.FakeStore
	committed                bool
	receiptReadsBeforeCommit int
	displayFailures          int
	requeues                 int
}

func (s *committedRetryProjectionFailure) RequeueOutboundJob(context.Context, uuid.UUID) error {
	panic("HTTP used unguarded retry")
}
func (s *committedRetryProjectionFailure) RequeueOutboundJobAuthorized(ctx context.Context, a authz.Actor, j *models.OutboundJob, v store.OutboundRetryValidator) (*models.OutboundJob, error) {
	s.requeues++
	out, e := s.FakeStore.RequeueOutboundJobAuthorized(ctx, a, j, v)
	s.committed = e == nil
	return out, e
}

// Fault the shipping post-commit receipt seam, not a content predicate the
// new handler never invokes. Admission succeeds before atomic requeue; only
// the subsequent display lookup fails.
func (s *committedRetryProjectionFailure) GetOutboundReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	if s.committed {
		s.displayFailures++
		return nil, errors.New("post-commit receipt lookup failed")
	}
	s.receiptReadsBeforeCommit++
	return s.FakeStore.GetOutboundReceipt(ctx, a, id, scope)
}

func TestAtomicRetryCommittedResponseStaysSuccessfulAndRestricted(t *testing.T) {
	f := newOutboundAccessFixture(t)
	ctx := context.Background()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: f.tenantID, Domain: "retry.test", IsVerified: true, MXVerified: true}
	f.st.SeedZone(zone)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, FullAddress: "sender@retry.test", OwnerUserID: &f.userA.ID}
	f.st.SeedMailbox(mb)
	j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, To: []string{"to@fixture.test"}, RcptTo: []string{"to@fixture.test", "hidden@fixture.test"}, BCC: []string{"hidden@fixture.test"}, TextBody: "PRIVATE_RETRY_CONTENT", State: models.OutboundDead}
	if e := f.st.CreateOutboundJob(ctx, j); e != nil {
		t.Fatal(e)
	}
	st := &committedRetryProjectionFailure{FakeStore: f.st}
	h := NewOutboundHandler(outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop()), st, zerolog.Nop())
	rr := doOutboundHandlerRequest(t, f.st, h.RetryJob, "POST", "/api/v1/outbound/"+j.ID.String()+"/retry", map[string]string{"id": j.ID.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != 200 || !st.committed || st.receiptReadsBeforeCommit != 1 || st.requeues != 1 || st.displayFailures != 1 {
		t.Fatalf("committed retry reported %d", rr.Code)
	}
	receipt := outboundReceiptData(t, rr)
	if receipt.ID != j.ID || receipt.State != models.OutboundPending || receipt.Status != "needs_attention" || receipt.Progress.Completeness != "unknown" || receipt.Progress.Counts != nil || receipt.Capabilities == nil || receipt.Capabilities.ViewContent || receipt.Capabilities.Retry || receipt.Capabilities.RetryBlockReason != "unknown" {
		t.Fatalf("post-commit display failure fabricated success evidence: %+v", receipt)
	}
	if receipt.TenantID != nil || receipt.CreatedAt != nil || receipt.UpdatedAt != nil || receipt.AttemptCount != nil || receipt.NextRetry != nil {
		t.Fatal("fallback invented current display metadata")
	}
	for _, private := range []string{"PRIVATE_RETRY_CONTENT", "hidden@fixture.test", "to@fixture.test", mb.FullAddress, "content_redacted"} {
		if strings.Contains(rr.Body.String(), private) {
			t.Fatal("post-commit minimal receipt leaked source or restored removed field", private)
		}
	}

	stored, e := f.st.GetOutboundJob(ctx, j.ID)
	if e != nil || stored.State != models.OutboundPending {
		t.Fatal("retry did not persist once")
	}
}
