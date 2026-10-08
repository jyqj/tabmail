package realtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"tabmail/internal/models"
)

type r5MonitorRecorderFunc func(context.Context, *models.MonitorEvent) error

func (f r5MonitorRecorderFunc) CreateMonitorEvent(ctx context.Context, event *models.MonitorEvent) error {
	return f(ctx, event)
}

// A blocked monitor INSERT is auxiliary work after the live event has already
// been emitted. It must not hold an ingest acknowledgement or worker forever.
func TestR5MonitorRecorderReleasesPublisherAtDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan context.Context, 1)
		release := make(chan struct{})
		finished := make(chan struct{})
		recorder := r5MonitorRecorderFunc(func(ctx context.Context, _ *models.MonitorEvent) error {
			started <- ctx
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
		hub := NewHub(2, recorder)
		mailbox, unsubscribeMailbox := hub.Subscribe("user@mail.test")
		defer unsubscribeMailbox()
		global, unsubscribeGlobal := hub.Subscribe("")
		defer unsubscribeGlobal()
		go func() {
			hub.Publish(Event{Type: EventMessage, Mailbox: "user@mail.test", MessageID: "committed-message"})
			close(finished)
		}()
		recordingContext := <-started
		synctest.Wait()

		// The recorder must not delay either existing subscribers or new
		// subscribers replaying history while persistence is blocked.
		replay, unsubscribeReplay := hub.Subscribe("")
		defer unsubscribeReplay()
		for name, events := range map[string]<-chan Event{"mailbox": mailbox, "global": global, "history": replay} {
			select {
			case event := <-events:
				if event.MessageID != "committed-message" || event.At.IsZero() {
					t.Errorf("%s received wrong live event: %#v", name, event)
				}
			default:
				t.Errorf("%s was held behind monitor persistence", name)
			}
		}
		select {
		case <-finished:
			t.Error("Publish returned without joining the active recorder")
		default:
		}

		time.Sleep(5 * time.Second)
		synctest.Wait()
		select {
		case <-finished:
			if !errors.Is(recordingContext.Err(), context.DeadlineExceeded) {
				t.Errorf("blocked recorder ended without deadline cancellation: %v", recordingContext.Err())
			}
		default:
			t.Error("Publish is still blocked after the monitor persistence budget")
		}
		// Let the same test join the historical unbounded implementation too.
		close(release)
		<-finished
	})
}

func TestR5MonitorRecorderReleasesContextAfterFastResult(t *testing.T) {
	for name, result := range map[string]error{"stored": nil, "storage-error": errors.New("storage unavailable")} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var recordedContext context.Context
				var recorded models.MonitorEvent
				calls := 0
				hub := NewHub(1, r5MonitorRecorderFunc(func(ctx context.Context, event *models.MonitorEvent) error {
					recordedContext = ctx
					recorded = *event
					calls++
					return result
				}))
				before := time.Now()
				hub.Publish(Event{
					Type: EventDelete, Mailbox: "user@mail.test", MessageID: "deleted-message",
					Sender: "sender@mail.test", Subject: "subject", Size: 17,
				})
				if calls != 1 || time.Since(before) != 0 {
					t.Fatalf("fast recorder was retried or delayed: calls=%d elapsed=%s", calls, time.Since(before))
				}
				if !errors.Is(recordedContext.Err(), context.Canceled) {
					t.Errorf("completed recorder context remains live: %v", recordedContext.Err())
				}
				want := models.MonitorEvent{
					Type: "delete", Mailbox: "user@mail.test", MessageID: "deleted-message",
					Sender: "sender@mail.test", Subject: "subject", Size: 17, At: before.UTC(),
				}
				if !reflect.DeepEqual(recorded, want) {
					t.Fatalf("monitor event changed: got %#v want %#v", recorded, want)
				}
				replay, unsubscribe := hub.Subscribe("")
				defer unsubscribe()
				select {
				case event := <-replay:
					if event.MessageID != want.MessageID || event.Type != EventDelete || !event.At.Equal(want.At) {
						t.Errorf("fast recorder result changed live history: %#v", event)
					}
				default:
					t.Error("live event missing from history after recorder result")
				}
			})
		})
	}
}
