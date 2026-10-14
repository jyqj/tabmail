package ingest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

const nextReplayRaw = "Subject: replay\r\n\r\naccepted private bytes"

// These tests enter the actual replay worker after durable Accept/Claim. The
// fake ledger observes delivery/hold/retry semantics; SQL fencing and rollback
// remain the responsibility of the existing real PostgreSQL regressions.
type nextReplayLedger struct {
	*testutil.FakeStore
	lists, deliveries int
}

func (s *nextReplayLedger) ListIngressTargets(ctx context.Context, id uuid.UUID) ([]store.IngressTarget, error) {
	s.lists++
	return s.FakeStore.ListIngressTargets(ctx, id)
}
func (s *nextReplayLedger) DeliverIngress(ctx context.Context, c *store.IngressClaim, m *models.Message, maxMailbox, daily int) (bool, error) {
	s.deliveries++
	return s.FakeStore.DeliverIngress(ctx, c, m, maxMailbox, daily)
}

type nextReplayReader struct {
	read     func([]byte) (int, error)
	closeErr error
	onClose  func()
	closes   atomic.Int64
	closed   chan struct{}
	once     sync.Once
}

func (r *nextReplayReader) Read(p []byte) (int, error) { return r.read(p) }
func (r *nextReplayReader) Close() error {
	r.closes.Add(1)
	if r.onClose != nil {
		r.onClose()
	}
	if r.closed != nil {
		r.once.Do(func() { close(r.closed) })
	}
	return r.closeErr
}

type nextReplayObjects struct {
	*testutil.MemoryObjectStore
	reader        io.ReadCloser
	getErr        error
	onGet         func()
	gets, deletes int
}

func (o *nextReplayObjects) Get(context.Context, string) (io.ReadCloser, error) {
	o.gets++
	if o.onGet != nil {
		o.onGet()
	}
	return o.reader, o.getErr
}
func (o *nextReplayObjects) Delete(ctx context.Context, key string) error {
	o.deletes++
	return o.MemoryObjectStore.Delete(ctx, key)
}

type nextReplayFixture struct {
	svc    *Service
	ledger *nextReplayLedger
	obj    *nextReplayObjects
	reader *nextReplayReader
	claim  *store.IngressClaim
}

func newNextReplayFixture(t *testing.T, raw []byte, maxMessageBytes int) *nextReplayFixture {
	t.Helper()
	st, memory, _, svc, alice, bob := newCoreFixture(t, func(p *models.Plan) { p.MaxMessageBytes = maxMessageBytes }, true, nil)
	result, err := svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{alice.FullAddress, bob.FullAddress}}, raw)
	if err != nil || !result.Queued {
		t.Fatalf("durable accept: %+v %v", result, err)
	}
	claim, err := st.ClaimIngress(context.Background())
	if err != nil || claim == nil {
		t.Fatalf("claim accepted receipt: %v", err)
	}
	reader := &nextReplayReader{read: bytes.NewReader(raw).Read}
	objects := &nextReplayObjects{MemoryObjectStore: memory, reader: reader}
	ledger := &nextReplayLedger{FakeStore: st}
	svc.obj, svc.store = objects, ledger
	return &nextReplayFixture{svc, ledger, objects, reader, claim}
}

func (f *nextReplayFixture) run(ctx context.Context) error {
	return f.svc.processReceipt(ctx, f.ledger, f.claim)
}

func (f *nextReplayFixture) assertTargets(t *testing.T, state string, attempts int) {
	t.Helper()
	targets, err := f.ledger.FakeStore.ListIngressTargets(context.Background(), f.claim.Job.ID)
	if err != nil || len(targets) != 2 {
		t.Fatalf("target ledger: %v %v", targets, err)
	}
	for _, target := range targets {
		if target.State != state || target.Attempts != attempts {
			t.Errorf("target state=%s attempts=%d, want %s/%d", target.State, target.Attempts, state, attempts)
		}
	}
	if state != "delivered" {
		if f.ledger.deliveries != 0 {
			t.Errorf("failed source reached %d delivery calls", f.ledger.deliveries)
		}
		n, err := f.ledger.CountAllMessages(context.Background())
		if err != nil || n != 0 {
			t.Errorf("failed source stored messages=%d: %v", n, err)
		}
		audits, err := f.ledger.ListAuditEntries(context.Background(), 100)
		if err != nil || len(audits) != 0 {
			t.Errorf("failed source wrote delivery audit: %v", err)
		}
	}
	if exists, err := f.obj.MemoryObjectStore.Exists(context.Background(), f.claim.Job.RawObjectKey); err != nil || !exists || f.obj.deletes != 0 {
		t.Errorf("accepted original was deleted: exists=%v deletes=%d err=%v", exists, f.obj.deletes, err)
	}
}

func TestNextIngressReplayObjectFailuresKeepOwnershipAndCauses(t *testing.T) {
	getErr, readErr, closeErr := errors.New("get fault"), errors.New("read fault"), errors.New("close fault")
	for _, mode := range []string{"get", "get-and-close", "missing-reader", "read-and-close", "close"} {
		t.Run(mode, func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			var causes []error
			switch mode {
			case "get":
				f.obj.getErr, causes = getErr, []error{getErr}
			case "get-and-close":
				f.obj.getErr, f.reader.closeErr, causes = getErr, closeErr, []error{getErr, closeErr}
			case "missing-reader":
				f.obj.reader = nil
			case "read-and-close":
				f.reader.read = func(p []byte) (int, error) { return copy(p, nextReplayRaw), readErr }
				f.reader.closeErr, causes = closeErr, []error{readErr, closeErr}
			case "close":
				f.reader.closeErr, causes = closeErr, []error{closeErr}
			}
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("object failure panicked: %v", p)
				}
			}()
			err := f.run(context.Background())
			if err == nil {
				t.Error("object failure became successful replay")
			}
			for _, cause := range causes {
				if !errors.Is(err, cause) {
					t.Errorf("missing %v from %v", cause, err)
				}
			}
			wantCloses := int64(1)
			if f.obj.reader == nil {
				wantCloses = 0
			}
			if f.reader.closes.Load() != wantCloses {
				t.Errorf("reader closes=%d, want %d", f.reader.closes.Load(), wantCloses)
			}
			f.assertTargets(t, "pending", 1)
		})
	}
}

func TestNextIngressReplayCancellationPreventsDelivery(t *testing.T) {
	for _, stage := range []string{"before-call", "get", "final-read", "close"} {
		t.Run(stage, func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getErr, closeErr := errors.New("cancelled get fault"), errors.New("cancelled close fault")
			switch stage {
			case "before-call":
				cancel()
			case "get":
				f.obj.onGet, f.obj.getErr, f.reader.closeErr = cancel, getErr, closeErr
			case "final-read":
				f.reader.read = func(p []byte) (int, error) { cancel(); return copy(p, nextReplayRaw), io.EOF }
			case "close":
				f.reader.onClose = cancel
			}
			err := f.run(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled replay result: %v", err)
			}
			wantCloses := int64(1)
			if stage == "before-call" {
				wantCloses = 0
				if f.obj.gets != 0 || f.ledger.lists != 0 {
					t.Errorf("pre-cancelled replay performed I/O: gets=%d lists=%d", f.obj.gets, f.ledger.lists)
				}
			}
			if stage == "get" && (!errors.Is(err, getErr) || !errors.Is(err, closeErr)) {
				t.Errorf("cancelled open lost original failures: %v", err)
			}
			if f.reader.closes.Load() != wantCloses {
				t.Errorf("reader closes=%d, want %d", f.reader.closes.Load(), wantCloses)
			}
			f.assertTargets(t, "pending", 0)
		})
	}
}

func TestNextIngressReplayCancellationUnblocksOwnedRead(t *testing.T) {
	for _, initialChunk := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-read", true: "after-progress"}[initialChunk], func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			readErr, closeErr := errors.New("blocked read fault"), errors.New("blocked close fault")
			f.reader.closed, f.reader.closeErr = make(chan struct{}), closeErr
			calls := 0
			f.reader.read = func(p []byte) (int, error) {
				calls++
				if initialChunk && calls == 1 {
					return copy(p, "first"), nil
				}
				close(entered)
				<-f.reader.closed
				return copy(p, "late bytes"), readErr
			}
			done := make(chan error, 1)
			go func() { done <- f.run(ctx) }()
			<-entered
			cancel()
			var err error
			select {
			case err = <-done:
			case <-time.After(time.Second):
				t.Error("cancellation did not unblock the owned reader")
				_ = f.reader.Close()
				err = <-done // Join even the controlled red-baseline worker.
			}
			for _, cause := range []error{context.Canceled, readErr, closeErr} {
				if !errors.Is(err, cause) {
					t.Errorf("cancelled replay lost %v: %v", cause, err)
				}
			}
			if f.reader.closes.Load() != 1 {
				t.Errorf("reader closes=%d", f.reader.closes.Load())
			}
			f.assertTargets(t, "pending", 0)
		})
	}
}

func TestNextIngressReplayNoProgressIsBoundedAfterAnyChunk(t *testing.T) {
	for _, initialChunk := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-read", true: "after-progress"}[initialChunk], func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			calls, empties := 0, 0
			probeErr := errors.New("reader exceeded the empty-read budget")
			f.reader.read = func(p []byte) (int, error) {
				calls++
				if initialChunk && calls == 1 {
					return copy(p, "first"), nil
				}
				empties++
				if empties > 100 {
					return 0, probeErr // Keep the unfixed baseline finite.
				}
				return 0, nil
			}
			err := f.run(context.Background())
			if !errors.Is(err, io.ErrNoProgress) || empties != 100 || f.reader.closes.Load() != 1 {
				t.Errorf("unbounded no-progress replay: empty=%d close=%d err=%v", empties, f.reader.closes.Load(), err)
			}
			f.assertTargets(t, "pending", 1)
		})
	}
}

func TestNextIngressReplayTransientEmptyReadsKeepProgressBudget(t *testing.T) {
	f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
	calls, offset := 0, 0
	f.reader.read = func(p []byte) (int, error) {
		calls++
		if calls%100 != 0 {
			return 0, nil
		}
		if offset == len(nextReplayRaw) {
			return 0, io.EOF
		}
		p[0] = nextReplayRaw[offset]
		offset++
		return 1, nil
	}
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if offset != len(nextReplayRaw) || f.reader.closes.Load() != 1 {
		t.Fatal("progressing source was not read and closed")
	}
	f.assertTargets(t, "delivered", 1)
}

func TestNextIngressReplayInvalidCountsFailWithoutPanic(t *testing.T) {
	for _, overBuffer := range []bool{false, true} {
		t.Run(map[bool]string{false: "negative", true: "over-buffer"}[overBuffer], func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			f.reader.read = func(p []byte) (int, error) {
				copy(p, "invalid private bytes")
				if overBuffer {
					return len(p) + 1, nil
				}
				return -1, nil
			}
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("invalid reader count panicked: %v", p)
				}
			}()
			err := f.run(context.Background())
			if !errors.Is(err, io.ErrUnexpectedEOF) || f.reader.closes.Load() != 1 {
				t.Errorf("invalid adapter read not rejected: close=%d err=%v", f.reader.closes.Load(), err)
			}
			f.assertTargets(t, "pending", 1)
		})
	}
}

func TestNextIngressReplayPreservesReceiptIntegrity(t *testing.T) {
	for _, mode := range []string{"exact", "data-and-eof", "short", "wrong-hash", "matching-prefix-with-tail"} {
		t.Run(mode, func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			raw := []byte(nextReplayRaw)
			switch mode {
			case "short":
				raw = raw[:len(raw)-1]
			case "wrong-hash":
				raw[len(raw)-1] ^= 1
			case "matching-prefix-with-tail":
				raw = append(raw, bytes.Repeat([]byte("x"), 1024)...)
			}
			input, total := bytes.NewReader(raw), 0
			f.reader.read = func(p []byte) (int, error) {
				n, err := input.Read(p)
				total += n
				if mode == "data-and-eof" && input.Len() == 0 {
					err = io.EOF
				}
				return n, err
			}
			err := f.run(context.Background())
			if mode == "exact" || mode == "data-and-eof" {
				if err != nil {
					t.Fatal(err)
				}
				f.assertTargets(t, "delivered", 1)
			} else {
				var permanent permanentIngress
				if !errors.As(err, &permanent) {
					t.Errorf("corruption was not held: %v", err)
				}
				f.assertTargets(t, "held", 1)
			}
			if mode == "matching-prefix-with-tail" && total != len(nextReplayRaw)+1 {
				t.Errorf("read %d bytes, want exact receipt size+1", total)
			}
			if f.reader.closes.Load() != 1 {
				t.Errorf("reader closes=%d", f.reader.closes.Load())
			}
		})
	}
}

func TestNextIngressReplayInvalidSizeDoesNotOpenObject(t *testing.T) {
	for _, size := range []int64{-1, math.MaxInt64} {
		t.Run(map[bool]string{true: "negative", false: "overflow"}[size < 0], func(t *testing.T) {
			f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
			f.claim.RawSize = size
			err := f.run(context.Background())
			var permanent permanentIngress
			if !errors.As(err, &permanent) || f.obj.gets != 0 {
				t.Errorf("invalid size entered object I/O: gets=%d err=%v", f.obj.gets, err)
			}
			f.assertTargets(t, "held", 1)
		})
	}
}

func TestNextIngressReplayKeepsConfiguredLargeOriginal(t *testing.T) {
	raw := bytes.Repeat([]byte("x"), 25*1024*1024+1)
	f := newNextReplayFixture(t, raw, len(raw))
	if err := f.run(context.Background()); err != nil {
		t.Fatalf("configured original above MIME parse limit was rejected: %v", err)
	}
	f.assertTargets(t, "delivered", 1)
	targets, _ := f.ledger.FakeStore.ListIngressTargets(context.Background(), f.claim.Job.ID)
	for _, target := range targets {
		message, err := f.ledger.GetMessage(context.Background(), *target.MessageID)
		if err != nil || message == nil || message.Size != int64(len(raw)) || message.RawObjectKey != f.claim.Job.RawObjectKey {
			t.Errorf("large original receipt identity/size changed: %v", err)
		}
	}
}

func TestNextIngressReplayKeepsCurrentTenantSizePolicy(t *testing.T) {
	f := newNextReplayFixture(t, []byte(nextReplayRaw), len(nextReplayRaw)-1)
	err := f.run(context.Background())
	var permanent permanentIngress
	if !errors.As(err, &permanent) || permanent != "tenant size limit exceeded; accepted bytes retained" {
		t.Errorf("current tenant policy changed: %v", err)
	}
	f.assertTargets(t, "held", 1)
}

func TestNextIngressReplayReadFailureDoesNotRepeatDeliveredTarget(t *testing.T) {
	f := newNextReplayFixture(t, []byte(nextReplayRaw), 1<<20)
	targets, _ := f.ledger.FakeStore.ListIngressTargets(context.Background(), f.claim.Job.ID)
	if err := f.svc.deliverTarget(context.Background(), f.ledger, f.claim, targets[0], []byte(nextReplayRaw), envelopeContent{}); err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("retry source unavailable")
	f.reader.read = func([]byte) (int, error) { return 0, readErr }
	if err := f.run(context.Background()); !errors.Is(err, readErr) {
		t.Fatalf("read failure result: %v", err)
	}
	targets, _ = f.ledger.FakeStore.ListIngressTargets(context.Background(), f.claim.Job.ID)
	if targets[0].State != "delivered" || targets[0].Attempts != 1 || targets[1].State != "pending" || targets[1].Attempts != 1 || f.ledger.deliveries != 1 {
		t.Fatalf("replay changed completed target: %+v deliveries=%d", targets, f.ledger.deliveries)
	}
	if f.reader.closes.Load() != 1 || f.obj.deletes != 0 {
		t.Fatal("failed replay leaked its reader or deleted the original")
	}
}
