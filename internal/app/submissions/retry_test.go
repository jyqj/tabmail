package submissions

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"testing"
)

type retryReaderFailure struct {
	store.OutboundRetryReader
	calls   int
	failure error
}

func (r *retryReaderFailure) GetUser(context.Context, uuid.UUID) (*models.User, error) {
	r.calls++
	return nil, r.failure
}

type retryTransactionProbe struct {
	store.Store
	reader *retryReaderFailure
	calls  int
}

func (p *retryTransactionProbe) RequeueOutboundJob(context.Context, uuid.UUID) error {
	panic("unguarded retry used")
}
func (p *retryTransactionProbe) GetUser(context.Context, uuid.UUID) (*models.User, error) {
	panic("authorization escaped transaction reader")
}
func (p *retryTransactionProbe) RequeueOutboundJobAuthorized(ctx context.Context, a authz.Actor, j *models.OutboundJob, v store.OutboundRetryValidator) (*models.OutboundJob, error) {
	p.calls++
	if v == nil {
		panic("missing validation")
	}
	if e := v(ctx, p.reader, j); e != nil {
		return nil, e
	}
	panic("failed reader unexpectedly authorized")
}

func TestAtomicRetryPolicyUsesOnlyTransactionalReader(t *testing.T) {
	f := newSubmissionFixture(t)
	failure := errors.New("transaction reader failure")
	reader := &retryReaderFailure{failure: failure}
	probe := &retryTransactionProbe{Store: f.st, reader: reader}
	svc := NewService(nil, probe, nil, zerolog.Nop())
	j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, State: models.OutboundDead}
	out, e := svc.RetryOutboundJob(context.Background(), userActor(f.owner), j)
	if out != nil || !errors.Is(e, failure) || reader.calls != 1 || probe.calls != 1 {
		t.Fatal("retry policy did not fail atomically via the supplied reader")
	}
}
func TestAtomicRetryMissingValidatorCannotMutate(t *testing.T) {
	f := newSubmissionFixture(t)
	j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, State: models.OutboundDead}
	if e := f.st.CreateOutboundJob(context.Background(), j); e != nil {
		t.Fatal(e)
	}
	if out, e := f.st.RequeueOutboundJobAuthorized(context.Background(), userActor(f.owner), j, nil); out != nil || e == nil {
		t.Fatal("nil validator accepted")
	}
	current, e := f.st.GetOutboundJob(context.Background(), j.ID)
	if e != nil || current.State != models.OutboundDead {
		t.Fatal("failed validation modified job")
	}
}
