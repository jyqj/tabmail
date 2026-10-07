package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// These faults come from the existing standard-library SMTP pipe exchange.
// Store faults below wrap the shipping recipient loop and the existing FakeStore;
// they prove caller error/ordering contracts, not PostgreSQL durability.
func r5RecipientReplyCause(t *testing.T, code int) error {
	t.Helper()
	return r5SMTPCauseRun(t, context.Background(), "rcpt", func(_ net.Conn, peer net.Conn, _ *bufio.Reader) error {
		_, err := fmt.Fprintf(peer, "%d recipient refused\r\n", code)
		return err
	}, nil)
}

func TestR5RecipientErrorChainUncertainTerminalFailures(t *testing.T) {
	for _, alreadyUncertain := range []bool{false, true} {
		for _, lostLease := range []bool{false, true} {
			t.Run(fmt.Sprintf("existing=%v/lostLease=%v", alreadyUncertain, lostLease), func(t *testing.T) {
				st, svc, j, adapter := r5CheckpointOrderFixture(t)
				fault := errors.New("terminal checkpoint unavailable")
				if lostLease {
					fault = store.ErrDeliveryTokenMismatch
				}
				st.markErr = fault
				var networkErr error
				if alreadyUncertain {
					started, err := st.FakeStore.BeginOutboundRecipient(context.Background(), j.ID, j.DeliveryToken, "one@example.test")
					if err != nil || !started {
						t.Fatalf("seed in-flight recipient: %v, %v", started, err)
					}
				} else {
					networkErr = r5SMTPCauseRun(t, context.Background(), "final", func(net.Conn, net.Conn, *bufio.Reader) error { return nil }, nil)
					adapter.err = networkErr
				}
				err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil)
				if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, fault) {
					t.Fatalf("terminal write failure erased uncertain classification or storage cause: %v", err)
				}
				if !alreadyUncertain && (!errors.Is(err, networkErr) || !errors.Is(err, io.EOF)) {
					t.Fatalf("terminal write failure erased original SMTP cause: %v", err)
				}
				if state := r5CheckpointOrderRecipient(t, st, j); state != delivery.Uncertain {
					t.Fatalf("failed marker rewrote recipient outcome: %s", state)
				}
				calls := 1
				if alreadyUncertain {
					calls = 0
				}
				st.markErr = nil
				if err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil); err != nil {
					t.Fatalf("successful uncertain terminal marker is not a retryable result: %v", err)
				}
				if adapter.calls != calls {
					t.Fatalf("uncertain recipient retransmitted: calls=%d want=%d", adapter.calls, calls)
				}
				if err := st.RequeueOutboundJob(context.Background(), j.ID); !errors.Is(err, store.ErrOutboundNotRetryable) {
					t.Fatalf("uncertain job became manually retryable: %v", err)
				}
			})
		}
	}
}

func TestR5RecipientErrorChainCompletionFailures(t *testing.T) {
	for _, code := range []int{250, 451, 550} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			st, svc, j, adapter := r5CheckpointOrderFixture(t)
			fault := errors.New("recipient completion unavailable")
			st.checkpointErr = fault
			if code != 250 {
				adapter.err = r5RecipientReplyCause(t, code)
			}
			err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil)
			if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, fault) {
				t.Fatalf("completion must retain uncertainty and storage cause: %v", err)
			}
			if code != 250 {
				var reply *textproto.Error
				if !errors.Is(err, adapter.err) || !errors.As(err, &reply) || reply.Code != code {
					t.Fatalf("completion failure erased original SMTP rejection: %v", err)
				}
			}
			if state := r5CheckpointOrderRecipient(t, st, j); state != delivery.Uncertain {
				t.Fatalf("failed completion rewrote recipient outcome: %s", state)
			}
			st.checkpointErr = nil
			if err := svc.deliverRecipients(context.Background(), j, j.DeliveryToken, nil); err != nil {
				t.Fatal(err)
			}
			if adapter.calls != 1 {
				t.Fatalf("uncheckpointed result triggered another delivery: %d", adapter.calls)
			}
		})
	}
}

type r5RecipientCauseAdapter struct {
	calls  map[string]int
	faults map[string]error
}

func (*r5RecipientCauseAdapter) Name() string { return "recipient-cause-fault" }
func (a *r5RecipientCauseAdapter) Deliver(_ context.Context, j *models.OutboundJob, _ []byte) (*DeliveryResult, error) {
	if len(j.RcptTo) != 1 {
		return nil, errors.New("recipient isolation lost")
	}
	address := j.RcptTo[0]
	a.calls[address]++
	return nil, a.faults[address]
}

func TestR5RecipientErrorChainTemporarySummaryAndProgress(t *testing.T) {
	ctx := context.Background()
	st := &r5NullMXLedgerStore{FakeStore: testutil.NewFakeStore(), begin: map[string]int{}, complete: map[string]int{}}
	req := quotaTestSendRequest(uuid.New(), uuid.New())
	seedQuotaSender(t, st.FakeStore, &req)
	const prior = "already@example.test"
	const accepted = "accepted@example.test"
	const permanent = "permanent@example.test"
	const retryA = "temporary-a@example.test"
	const retryB = "temporary-b@example.test"
	req.To = []string{prior, accepted, permanent, retryA, retryB}
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 5, RetryDelay: time.Nanosecond}, st, nopGovernance{}, zerolog.Nop())
	firstCause := r5RecipientReplyCause(t, 451)
	secondCause := r5RecipientReplyCause(t, 452)
	a := &r5RecipientCauseAdapter{calls: map[string]int{}, faults: map[string]error{
		permanent: r5RecipientReplyCause(t, 550), retryA: firstCause, retryB: secondCause,
	}}
	svc.adapter = a
	job, err := svc.Submit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimOutboundJobs(ctx, time.Now(), 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claim: jobs=%v error=%v", claimed, err)
	}
	j := claimed[0]
	started, err := st.FakeStore.BeginOutboundRecipient(ctx, j.ID, j.DeliveryToken, prior)
	if err != nil || !started {
		t.Fatalf("seed accepted begin: started=%v error=%v", started, err)
	}
	if err := st.FakeStore.CompleteOutboundRecipient(ctx, j.ID, j.DeliveryToken, prior, delivery.Accepted, 250, "already accepted"); err != nil {
		t.Fatal(err)
	}
	err = svc.deliverRecipients(ctx, j, j.DeliveryToken, nil)
	if !errors.Is(err, firstCause) || !errors.Is(err, secondCause) || errors.Is(err, store.ErrOutboundUncertain) {
		t.Fatalf("temporary summary lost per-recipient causes or changed known results: %v", err)
	}
	assertStates := func(retryState string) {
		t.Helper()
		rows, err := st.ListOutboundRecipients(ctx, j.TenantID, j.ID)
		if err != nil || len(rows) != 5 {
			t.Fatalf("ledger: %+v error=%v", rows, err)
		}
		want := map[string]string{prior: delivery.Accepted, accepted: delivery.Accepted, permanent: delivery.Permanent, retryA: retryState, retryB: retryState}
		for _, row := range rows {
			if row.State != want[row.Address] {
				t.Fatalf("recipient %s state=%s want=%s", row.Address, row.State, want[row.Address])
			}
		}
	}
	assertStates(delivery.Temporary)
	a.faults = map[string]error{}
	if err := svc.deliverRecipients(ctx, j, j.DeliveryToken, nil); err != nil {
		t.Fatal(err)
	}
	assertStates(delivery.Accepted)
	for address, want := range map[string]int{prior: 0, accepted: 1, permanent: 1, retryA: 2, retryB: 2} {
		if a.calls[address] != want || st.begin[address] != want || st.complete[address] != want {
			t.Fatalf("recipient %s delivery/begin/complete = %d/%d/%d, want=%d", address, a.calls[address], st.begin[address], st.complete[address], want)
		}
	}
	stored, err := st.GetOutboundJob(ctx, j.ID)
	if err != nil || stored.State != models.OutboundFailed {
		t.Fatalf("permanent sibling must retain terminal failed job: %+v error=%v", stored, err)
	}
}
