package messageapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/testutil"
)

type r5MessageMonitorRecorder func(context.Context, *models.MonitorEvent) error

func (f r5MessageMonitorRecorder) CreateMonitorEvent(ctx context.Context, event *models.MonitorEvent) error {
	return f(ctx, event)
}

type r5MessageMonitorStore struct {
	*testutil.FakeStore
	afterCommit func()
	commits     int
}

func (s *r5MessageMonitorStore) DeleteMessage(ctx context.Context, id uuid.UUID) error {
	err := s.FakeStore.DeleteMessage(ctx, id)
	if err == nil {
		s.commits++
		if s.afterCommit != nil {
			s.afterCommit()
		}
	}
	return err
}

func (s *r5MessageMonitorStore) PurgeMailbox(ctx context.Context, id uuid.UUID) error {
	err := s.FakeStore.PurgeMailbox(ctx, id)
	if err == nil {
		s.commits++
		if s.afterCommit != nil {
			s.afterCommit()
		}
	}
	return err
}

func TestR5MessageMonitorFollowsCallerCancellation(t *testing.T) {
	for _, operation := range []string{"delete", "purge"} {
		for _, phase := range []string{"before-monitor", "during-monitor", "caller-deadline"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					base, svc, tenant, mailbox := seededMessageService(t, models.AccessAPIKey, nil)
					message := &models.Message{ID: uuid.New(), TenantID: tenant.ID, ZoneID: mailbox.ZoneID, MailboxID: mailbox.ID, Subject: "owned committed message"}
					base.SeedMessage(message)
					st := &r5MessageMonitorStore{FakeStore: base}
					svc.store = st
					svc.objects = rawobject.NewStore(svc.obj, st)
					type requestKey struct{}
					parent := context.WithValue(context.Background(), requestKey{}, "owned-message-request")
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
					svc.hub = realtime.NewHub(1, r5MessageMonitorRecorder(func(recordCtx context.Context, _ *models.MonitorEvent) error {
						started <- recordCtx
						select {
						case <-recordCtx.Done():
							return recordCtx.Err()
						case <-release:
							return nil
						}
					}))
					events, unsubscribe := svc.hub.Subscribe(mailbox.FullAddress)
					defer unsubscribe()
					done := make(chan struct{})
					var opErr error
					go func() {
						defer close(done)
						viewer := Viewer{Tenant: tenant, IsAdmin: true, AuthMode: AuthModeUser}
						if operation == "delete" {
							opErr = svc.DeleteMessage(ctx, mailbox.FullAddress, message.ID, viewer, "owned-admin")
						} else {
							opErr = svc.PurgeMailbox(ctx, mailbox.FullAddress, viewer, "owned-admin")
						}
					}()
					defer func() { cancel(); releaseRecorder(); <-done }()
					var recordCtx context.Context
					select {
					case recordCtx = <-started:
					case <-time.After(time.Second):
						t.Fatal("message mutation did not reach monitor publication")
					}
					wantCause := error(context.Canceled)
					if phase == "caller-deadline" {
						time.Sleep(100 * time.Millisecond)
						wantCause = context.DeadlineExceeded
					} else {
						cancel()
					}
					synctest.Wait()
					if recordCtx.Value(requestKey{}) != "owned-message-request" || !errors.Is(recordCtx.Err(), wantCause) {
						t.Errorf("monitor detached from message caller: value=%v error=%v want=%v", recordCtx.Value(requestKey{}), recordCtx.Err(), wantCause)
					}
					select {
					case <-done:
					default:
						t.Error("message mutation remains blocked after caller cancellation")
					}
					releaseRecorder()
					<-done
					if opErr != nil || st.commits != 1 {
						t.Fatalf("monitor cancellation changed message mutation: commits=%d err=%v", st.commits, opErr)
					}
					if remaining, err := st.GetMessage(context.Background(), message.ID); err != nil || remaining != nil {
						t.Fatalf("deleted message was restored or still exists: %#v %v", remaining, err)
					}
					select {
					case event := <-events:
						wantType := realtime.EventDelete
						if operation == "purge" {
							wantType = realtime.EventPurge
						}
						if event.Type != wantType || event.Mailbox != mailbox.FullAddress {
							t.Errorf("wrong committed mutation event: %#v", event)
						}
					default:
						t.Error("monitor cancellation suppressed committed mutation event")
					}
				})
			})
		}
	}
}
