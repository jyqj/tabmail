package realtime

import (
	"context"
	"testing"
	"time"

	"tabmail/internal/models"
)

type recorderStub struct {
	ch chan *models.MonitorEvent
}

func (r *recorderStub) CreateMonitorEvent(_ context.Context, e *models.MonitorEvent) error {
	cp := *e
	r.ch <- &cp
	return nil
}

func TestHubPublishDeliversToMailboxAndGlobalSubscribers(t *testing.T) {
	recorder := &recorderStub{ch: make(chan *models.MonitorEvent, 1)}
	hub := NewHub(2, recorder)

	mailboxCh, unsubscribeMailbox := hub.Subscribe("user@mail.test")
	defer unsubscribeMailbox()
	globalCh, unsubscribeGlobal := hub.Subscribe("")
	defer unsubscribeGlobal()

	hub.Publish(Event{
		Type:      EventMessage,
		Mailbox:   "user@mail.test",
		MessageID: "msg-1",
		Subject:   "hello",
		Size:      123,
	})

	select {
	case ev := <-mailboxCh:
		if ev.Mailbox != "user@mail.test" || ev.MessageID != "msg-1" || ev.At.IsZero() {
			t.Fatalf("unexpected mailbox event: %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting mailbox event")
	}

	select {
	case ev := <-globalCh:
		if ev.Type != EventMessage || ev.Subject != "hello" {
			t.Fatalf("unexpected global event: %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting global event")
	}

	select {
	case recorded := <-recorder.ch:
		if recorded.Mailbox != "user@mail.test" || recorded.MessageID != "msg-1" {
			t.Fatalf("unexpected recorded event: %#v", recorded)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting recorder event")
	}
}

func TestHubGlobalSubscriptionReplaysHistory(t *testing.T) {
	hub := NewHub(2, nil)

	hub.Publish(Event{Type: EventMessage, Mailbox: "first@mail.test", MessageID: "a"})
	hub.Publish(Event{Type: EventDelete, Mailbox: "second@mail.test", MessageID: "b"})

	ch, unsubscribe := hub.Subscribe("")
	defer unsubscribe()

	var got []Event
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			got = append(got, ev)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for history replay")
		}
	}

	if len(got) != 2 || got[0].MessageID != "a" || got[1].MessageID != "b" {
		t.Fatalf("unexpected replay order: %#v", got)
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	hub := NewHub(1, nil)
	ch, unsubscribe := hub.Subscribe("user@mail.test")
	unsubscribe()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}
