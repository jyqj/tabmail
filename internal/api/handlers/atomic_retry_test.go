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
	committed bool
}

func (s *committedRetryProjectionFailure) RequeueOutboundJob(context.Context, uuid.UUID) error {
	panic("HTTP used unguarded retry")
}
func (s *committedRetryProjectionFailure) RequeueOutboundJobAuthorized(ctx context.Context, a authz.Actor, j *models.OutboundJob, v store.OutboundRetryValidator) (*models.OutboundJob, error) {
	out, e := s.FakeStore.RequeueOutboundJobAuthorized(ctx, a, j, v)
	s.committed = e == nil
	return out, e
}
func (s *committedRetryProjectionFailure) CanReadOutboundContent(ctx context.Context, a authz.Actor, j *models.OutboundJob) (bool, error) {
	if s.committed {
		return false, errors.New("post-commit authorization lookup failed")
	}
	return s.FakeStore.CanReadOutboundContent(ctx, a, j)
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
	if rr.Code != 200 || !st.committed {
		t.Fatalf("committed retry reported %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "PRIVATE_RETRY_CONTENT") || strings.Contains(rr.Body.String(), "hidden@fixture.test") || !strings.Contains(rr.Body.String(), `"content_redacted":true`) {
		t.Fatal("post-commit failure leaked content or omitted restricted receipt")
	}
	stored, e := f.st.GetOutboundJob(ctx, j.ID)
	if e != nil || stored.State != models.OutboundPending {
		t.Fatal("retry did not persist once")
	}
}
