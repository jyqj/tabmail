package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type r5IngestMonitorRecorder func(context.Context, *models.MonitorEvent) error

func (f r5IngestMonitorRecorder) CreateMonitorEvent(ctx context.Context, event *models.MonitorEvent) error {
	return f(ctx, event)
}

type r5IngestMonitorStore struct {
	*testutil.FakeStore
	afterCommit func()
	commits     int
}

func (s *r5IngestMonitorStore) CreateMessageWithQuota(ctx context.Context, message *models.Message, maximum int, ensure func(context.Context) error) (bool, error) {
	inserted, err := s.FakeStore.CreateMessageWithQuota(ctx, message, maximum, ensure)
	if inserted && err == nil {
		s.commits++
		if s.afterCommit != nil {
			s.afterCommit()
		}
	}
	return inserted, err
}

func (s *r5IngestMonitorStore) DeliverIngress(ctx context.Context, claim *store.IngressClaim, message *models.Message, maximum, daily int) (bool, error) {
	inserted, err := s.FakeStore.DeliverIngress(ctx, claim, message, maximum, daily)
	if inserted && err == nil {
		s.commits++
		if s.afterCommit != nil {
			s.afterCommit()
		}
	}
	return inserted, err
}

// These are the real immediate Accept and durable Run paths. The store wrapper
// cancels only after its message mutation has completed, before monitor history
// begins, so cancellation must not turn a committed message into a lost event.
func TestR5IngestMonitorFollowsCallerCancellation(t *testing.T) {
	for _, mode := range []string{"immediate", "durable"} {
		for _, phase := range []string{"before-monitor", "during-monitor", "caller-deadline"} {
			t.Run(mode+"/"+phase, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					base, obj, svc := newDurableCleanupService(t, 1024*1024, true)
					st := &r5IngestMonitorStore{FakeStore: base}
					svc.store = st
					svc.objects = rawobject.NewStore(obj, st)
					svc.dispatcher = nil
					svc.durable = mode == "durable"
					type requestKey struct{}
					parent := context.WithValue(context.Background(), requestKey{}, "owned-ingest-request")
					var ctx context.Context
					var cancel context.CancelFunc
					if phase == "caller-deadline" {
						ctx, cancel = context.WithTimeout(parent, 100*time.Millisecond)
					} else {
						ctx, cancel = context.WithCancel(parent)
					}
					if phase == "before-monitor" {
						st.afterCommit = cancel
					}
					started := make(chan context.Context, 1)
					release := make(chan struct{})
					releaseRecorder := sync.OnceFunc(func() { close(release) })
					svc.hub = realtime.NewHub(1, r5IngestMonitorRecorder(func(recordCtx context.Context, _ *models.MonitorEvent) error {
						started <- recordCtx
						select {
						case <-recordCtx.Done():
							return recordCtx.Err()
						case <-release:
							return nil
						}
					}))
					events, unsubscribe := svc.hub.Subscribe("user@mail.test")
					defer unsubscribe()
					envelope := Envelope{Source: "smtp", MailFrom: "sender@mail.test", Recipients: []string{"user@mail.test"}}
					raw := []byte("Subject: committed before monitor\r\n\r\nowned message")
					if svc.durable {
						result, err := svc.Accept(context.Background(), envelope, raw)
						if err != nil || !result.Queued {
							t.Fatalf("durable acceptance: %#v %v", result, err)
						}
					}
					done := make(chan struct{})
					var opErr error
					go func() {
						defer close(done)
						if svc.durable {
							svc.Run(ctx)
						} else {
							var result AcceptResult
							result, opErr = svc.Accept(ctx, envelope, raw)
							if opErr == nil && result.Delivered != 1 {
								opErr = errors.New("immediate acceptance did not report committed delivery")
							}
						}
					}()
					defer func() { cancel(); releaseRecorder(); <-done }()
					var recordCtx context.Context
					select {
					case recordCtx = <-started:
					case <-time.After(time.Second):
						t.Fatal("service did not reach monitor publication")
					}
					wantCause := error(context.Canceled)
					if phase == "caller-deadline" {
						time.Sleep(100 * time.Millisecond)
						wantCause = context.DeadlineExceeded
					} else {
						cancel()
					}
					synctest.Wait()
					if recordCtx.Value(requestKey{}) != "owned-ingest-request" || !errors.Is(recordCtx.Err(), wantCause) {
						t.Errorf("monitor detached from ingest caller: value=%v error=%v want=%v", recordCtx.Value(requestKey{}), recordCtx.Err(), wantCause)
					}
					select {
					case <-done:
					default:
						t.Error("ingest remains blocked after its caller was canceled")
					}
					releaseRecorder()
					<-done
					if opErr != nil || st.commits != 1 {
						t.Fatalf("monitor cancellation changed message commit: commits=%d err=%v", st.commits, opErr)
					}
					mailbox, err := st.GetMailboxByAddress(context.Background(), "user@mail.test")
					if err != nil || mailbox == nil {
						t.Fatalf("committed mailbox missing: %v", err)
					}
					messages, total, err := st.ListMessages(context.Background(), mailbox.ID, models.Page{Page: 1, PerPage: 10})
					if err != nil || total != 1 || len(messages) != 1 {
						t.Fatalf("committed message missing or duplicated: total=%d err=%v", total, err)
					}
					if exists, err := obj.Exists(context.Background(), messages[0].RawObjectKey); err != nil || !exists {
						t.Errorf("monitor cancellation lost original bytes: exists=%v err=%v", exists, err)
					}
					select {
					case event := <-events:
						if event.Type != realtime.EventMessage || event.MessageID != messages[0].ID.String() {
							t.Errorf("wrong committed live event: %#v", event)
						}
					default:
						t.Error("monitor cancellation suppressed committed live event")
					}
				})
			})
		}
	}
}
