package outbound

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/textproto"
	"tabmail/internal/config"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// Fault-only fixture around the shipping recipient loop; not PostgreSQL or
// SMTP acceptance evidence. All normal ledger/fencing behavior delegates to
// the existing FakeStore rather than a second delivery service.
type r5CheckpointOrderStore struct {
	*testutil.FakeStore
	mu                                 sync.Mutex
	events                             []string
	checkpointErr, attemptErr, markErr error
	authorizationErr                   error
	attemptEntered                     chan struct{}
	attemptBlock                       bool
}

func (s *r5CheckpointOrderStore) event(e string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}
func (s *r5CheckpointOrderStore) trace() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}
func (s *r5CheckpointOrderStore) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	if s.authorizationErr != nil {
		return nil, s.authorizationErr
	}
	return s.FakeStore.GetUser(ctx, id)
}
func (s *r5CheckpointOrderStore) BeginOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address string) (bool, error) {
	s.event("begin")
	return s.FakeStore.BeginOutboundRecipient(ctx, id, token, address)
}
func (s *r5CheckpointOrderStore) CompleteOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address, state string, code int, diagnostic string) error {
	s.event("complete:" + state)
	if s.checkpointErr != nil {
		return s.checkpointErr
	}
	return s.FakeStore.CompleteOutboundRecipient(ctx, id, token, address, state, code, diagnostic)
}
func (s *r5CheckpointOrderStore) CreateOutboundAttempt(ctx context.Context, a *models.OutboundAttempt) error {
	s.event("attempt")
	if s.attemptEntered != nil {
		close(s.attemptEntered)
	}
	if s.attemptBlock {
		<-ctx.Done()
		return ctx.Err()
	}
	if s.attemptErr != nil {
		return s.attemptErr
	}
	return s.FakeStore.CreateOutboundAttempt(ctx, a)
}
func (s *r5CheckpointOrderStore) MarkOutboundJobSent(ctx context.Context, id uuid.UUID, token *uuid.UUID, code int, response, messageID string) error {
	s.event("mark_sent")
	if s.markErr != nil {
		return s.markErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.FakeStore.MarkOutboundJobSent(ctx, id, token, code, response, messageID)
}
func (s *r5CheckpointOrderStore) MarkOutboundJobFailed(ctx context.Context, id uuid.UUID, token *uuid.UUID, reason string, dead bool) error {
	s.event("mark_failed")
	if s.markErr != nil {
		return s.markErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.FakeStore.MarkOutboundJobFailed(ctx, id, token, reason, dead)
}

type r5CheckpointOrderAdapter struct {
	st    *r5CheckpointOrderStore
	err   error
	calls int
	after func()
}

func (*r5CheckpointOrderAdapter) Name() string { return "checkpoint-order-fault" }
func (a *r5CheckpointOrderAdapter) Deliver(_ context.Context, j *models.OutboundJob, _ []byte) (*DeliveryResult, error) {
	a.st.event("adapter")
	a.calls++
	result := &DeliveryResult{Adapter: a.Name(), SMTPCode: 250, StartedAt: time.Now(), FinishedAt: time.Now()}
	if a.err != nil {
		result.SMTPCode = 0
		result.Error = a.err.Error()
	}
	if a.after != nil {
		a.after()
	}
	return result, a.err
}
func r5CheckpointOrderFixture(t *testing.T) (*r5CheckpointOrderStore, *Service, *models.OutboundJob, *r5CheckpointOrderAdapter) {
	t.Helper()
	st := &r5CheckpointOrderStore{FakeStore: testutil.NewFakeStore()}
	req := quotaTestSendRequest(uuid.New(), uuid.New())
	seedQuotaSender(t, st.FakeStore, &req)
	req.To = []string{"one@example.test"}
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 5, RetryDelay: time.Nanosecond}, st, nopGovernance{}, zerolog.Nop())
	a := &r5CheckpointOrderAdapter{st: st}
	svc.adapter = a
	job, err := svc.Submit(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := st.ClaimOutboundJobs(context.Background(), time.Now(), 1)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("claim: %v %+v", err, jobs)
	}
	return st, svc, jobs[0], a
}
func r5CheckpointOrderRecipient(t *testing.T, st *r5CheckpointOrderStore, job *models.OutboundJob) string {
	t.Helper()
	rows, err := st.ListOutboundRecipients(context.Background(), job.TenantID, job.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger: %v %+v", err, rows)
	}
	return rows[0].State
}
func r5CheckpointOrderWantTrace(t *testing.T, st *r5CheckpointOrderStore, want []string) {
	t.Helper()
	if got := st.trace(); !reflect.DeepEqual(got, want) {
		t.Fatalf("shipping recipient trace=%v want=%v", got, want)
	}
}

func TestR5RecipientCheckpointOrderKnownResults(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		deliveryErr error
	}{
		{"accepted", delivery.Accepted, nil},
		{"temporary", delivery.Temporary, &textproto.Error{Code: 451, Msg: "temporary"}},
		{"permanent", delivery.Permanent, &textproto.Error{Code: 550, Msg: "permanent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, svc, j, a := r5CheckpointOrderFixture(t)
			a.err = tc.deliveryErr
			st.attemptErr = errors.New("optional attempt unavailable")
			err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil)
			if tc.state == delivery.Temporary {
				if err == nil {
					t.Fatal("temporary delivery became success")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if state := r5CheckpointOrderRecipient(t, st, j); state != tc.state {
				t.Fatalf("state=%s want=%s", state, tc.state)
			}
			want := []string{"begin", "adapter", "complete:" + tc.state, "attempt"}
			if tc.state == delivery.Accepted {
				want = append(want, "mark_sent")
			}
			if tc.state == delivery.Permanent {
				want = append(want, "mark_failed")
			}
			r5CheckpointOrderWantTrace(t, st, want)
		})
	}
}

func TestR5RecipientCheckpointOrderBlockedTelemetryCancelKeepsAccepted(t *testing.T) {
	st, svc, j, a := r5CheckpointOrderFixture(t)
	st.attemptEntered = make(chan struct{})
	st.attemptBlock = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- svc.deliverRecipients(ctx, j, j.DeliveryToken, nil) }()
	select {
	case <-st.attemptEntered:
	case <-time.After(time.Second):
		t.Fatal("attempt barrier not reached")
	}
	state := r5CheckpointOrderRecipient(t, st, j)
	trace := st.trace()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("job-mark context error lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("context-aware telemetry did not stop")
	}
	if state != delivery.Accepted {
		t.Fatalf("telemetry entered before accepted durable checkpoint: state=%s trace=%v", state, trace)
	}
	st.attemptBlock = false
	st.attemptEntered = nil
	if err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil); err != nil {
		t.Fatal(err)
	}
	if a.calls != 1 {
		t.Fatalf("accepted recipient touched adapter again: %d", a.calls)
	}
	r5CheckpointOrderWantTrace(t, st, []string{"begin", "adapter", "complete:" + delivery.Accepted, "attempt", "mark_sent", "mark_sent"})
}

func TestR5RecipientCheckpointOrderFailureKeepsUncertainNoTelemetry(t *testing.T) {
	for _, mode := range []string{"checkpoint_fault", "lost_lease_at_checkpoint", "cancel_before_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			st, svc, j, a := r5CheckpointOrderFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := errors.New("checkpoint storage unavailable")
			if mode == "checkpoint_fault" || mode == "lost_lease_at_checkpoint" {
				if mode == "lost_lease_at_checkpoint" {
					fault = store.ErrDeliveryTokenMismatch
				}
				st.checkpointErr = fault
			} else {
				a.after = cancel
			}
			err := svc.deliverRecipients(ctx, j, j.DeliveryToken, nil)
			if !errors.Is(err, store.ErrOutboundUncertain) {
				t.Fatalf("uncheckpointed acceptance must be uncertain: %v", err)
			}
			if mode != "cancel_before_checkpoint" && !errors.Is(err, fault) {
				t.Fatalf("checkpoint error chain lost: %v", err)
			}
			if mode == "cancel_before_checkpoint" && !errors.Is(err, context.Canceled) {
				t.Fatalf("checkpoint cancellation cause lost: %v", err)
			}
			if state := r5CheckpointOrderRecipient(t, st, j); state != delivery.Uncertain {
				t.Fatalf("lost uncertain state: %s", state)
			}
			r5CheckpointOrderWantTrace(t, st, []string{"begin", "adapter", "complete:" + delivery.Accepted})
			// The existing uncertain marker blocks even a fresh context and valid token.
			if err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil); err != nil {
				t.Fatal(err)
			}
			if a.calls != 1 {
				t.Fatalf("checkpoint failure retried network: %d", a.calls)
			}
		})
	}
}

func TestR5RecipientCheckpointOrderUncertainTerminalBeforeTelemetry(t *testing.T) {
	st, svc, j, a := r5CheckpointOrderFixture(t)
	a.err = fmt.Errorf("lost final reply: %w", store.ErrOutboundUncertain)
	st.attemptEntered = make(chan struct{})
	st.attemptBlock = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- svc.deliverRecipients(ctx, j, j.DeliveryToken, nil) }()
	select {
	case <-st.attemptEntered:
	case <-time.After(time.Second):
		t.Fatal("attempt barrier missing")
	}
	stored, _ := st.GetOutboundJob(context.Background(), j.ID)
	state := r5CheckpointOrderRecipient(t, st, j)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("optional telemetry did not stop")
	}
	if stored.State != models.OutboundFailed || stored.InFlightDomain == "" || state != delivery.Uncertain {
		t.Fatalf("uncertain not terminal before telemetry: job=%+v recipient=%s", stored, state)
	}
	r5CheckpointOrderWantTrace(t, st, []string{"begin", "adapter", "mark_failed", "attempt"})
	if err := st.RequeueOutboundJob(context.Background(), j.ID); !errors.Is(err, store.ErrOutboundNotRetryable) {
		t.Fatalf("uncertain manually retried: %v", err)
	}
	if a.calls != 1 {
		t.Fatal("uncertain retransmitted")
	}
}

func TestR5RecipientCheckpointOrderJobMarkErrorsAreNotTelemetry(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprintf("uncertain=%v", uncertain), func(t *testing.T) {
			st, svc, j, a := r5CheckpointOrderFixture(t)
			fault := errors.New("job mark unavailable")
			st.markErr = fault
			st.attemptErr = errors.New("telemetry unavailable")
			if uncertain {
				a.err = store.ErrOutboundUncertain
			}
			err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil)
			if !errors.Is(err, fault) {
				t.Fatalf("real job-mark error swallowed: %v", err)
			}
			want := []string{"begin", "adapter"}
			if uncertain {
				want = append(want, "mark_failed")
			} else {
				want = append(want, "complete:"+delivery.Accepted, "attempt", "mark_sent")
			}
			r5CheckpointOrderWantTrace(t, st, want)
		})
	}
}

func TestR5RecipientCheckpointOrderFencingBeforeNetwork(t *testing.T) {
	for _, mode := range []string{"wrong_token", "expired_lease", "wrong_tenant", "revoked_sender"} {
		t.Run(mode, func(t *testing.T) {
			st, svc, j, a := r5CheckpointOrderFixture(t)
			token := j.DeliveryToken
			switch mode {
			case "wrong_token":
				other := uuid.New()
				token = &other
			case "expired_lease":
				past := time.Now().Add(-time.Second)
				j.LeaseUntil = &past
				if err := st.FakeStore.CreateOutboundJob(context.Background(), j); err != nil {
					t.Fatal(err)
				}
			case "wrong_tenant":
				j.TenantID = uuid.New()
			case "revoked_sender":
				u, err := st.GetUser(context.Background(), *j.SenderUserID)
				if err != nil {
					t.Fatal(err)
				}
				u.IsActive = false
				if err := st.UpdateUser(context.Background(), u); err != nil {
					t.Fatal(err)
				}
			}
			err := svc.deliverRecipients(context.Background(), j, token, nil)
			if mode == "revoked_sender" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid fence accepted")
			}
			if a.calls != 0 {
				t.Fatalf("invalid %s reached network %d times", mode, a.calls)
			}
			for _, event := range st.trace() {
				if event == "attempt" || event == "adapter" || event == "complete:"+delivery.Accepted {
					t.Fatalf("invalid fence mutated acceptance: %v", st.trace())
				}
			}
		})
	}
}

func TestR5RecipientCheckpointOrderAuthorizationStoreErrorPreserved(t *testing.T) {
	st, svc, j, a := r5CheckpointOrderFixture(t)
	fault := errors.New("current authorization database unavailable")
	st.authorizationErr = fault
	st.attemptErr = errors.New("optional telemetry unavailable")
	err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil)
	if !errors.Is(err, fault) {
		t.Fatalf("authorization store failure swallowed: %v", err)
	}
	if a.calls != 0 || len(st.trace()) != 0 {
		t.Fatalf("authorization error continued delivery: calls=%d trace=%v", a.calls, st.trace())
	}
	if state := r5CheckpointOrderRecipient(t, st, j); state != delivery.Pending {
		t.Fatalf("authorization failure changed ledger: %s", state)
	}
}
