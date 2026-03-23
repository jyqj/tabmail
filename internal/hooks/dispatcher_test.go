package hooks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDispatcherPublishSendsSignedWebhook(t *testing.T) {
	type capturedRequest struct {
		header http.Header
		body   []byte
	}

	reqCh := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reqCh <- capturedRequest{
			header: r.Header.Clone(),
			body:   body,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := New(Config{
		URLs:       srv.URL,
		Secret:     "top-secret",
		Timeout:    time.Second,
		MaxRetries: 1,
		RetryDelay: time.Millisecond,
	}, zerolog.Nop())

	d.Publish(Event{
		Type:    "message.received",
		Mailbox: "user@mail.test",
		Subject: "hello",
	})

	select {
	case got := <-reqCh:
		if got.header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %q", got.header.Get("Content-Type"))
		}
		if got.header.Get("X-TabMail-Event") != "message.received" {
			t.Fatalf("unexpected event header: %q", got.header.Get("X-TabMail-Event"))
		}
		if got.header.Get("X-TabMail-Attempt") != "1" {
			t.Fatalf("unexpected attempt header: %q", got.header.Get("X-TabMail-Attempt"))
		}
		if got.header.Get("X-TabMail-Signature") != sign("top-secret", got.body) {
			t.Fatalf("signature mismatch")
		}

		var payload Event
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Type != "message.received" || payload.Mailbox != "user@mail.test" || payload.Subject != "hello" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		if payload.OccurredAt.IsZero() {
			t.Fatalf("expected occurred_at to be auto-filled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook request")
	}
}

func TestDispatcherDispatchRecordsDeadLettersAndTrims(t *testing.T) {
	d := New(Config{
		URLs:       "http://127.0.0.1:1",
		Timeout:    100 * time.Millisecond,
		MaxRetries: 2,
		RetryDelay: time.Millisecond,
		DeadLimit:  2,
	}, zerolog.Nop())

	for _, id := range []string{"job-1", "job-2", "job-3"} {
		d.dispatch(job{
			id:        id,
			url:       "http://127.0.0.1:1",
			payload:   []byte(`{"type":"message.received"}`),
			eventType: "message.received",
			created:   time.Now().UTC(),
		})
	}

	if got := d.DeadLetterSize(); got != 2 {
		t.Fatalf("expected dead-letter size 2, got %d", got)
	}

	letters := d.DeadLetters(10)
	if len(letters) != 2 {
		t.Fatalf("expected 2 dead letters, got %d", len(letters))
	}
	if letters[0].ID != "job-3" || letters[1].ID != "job-2" {
		t.Fatalf("unexpected dead-letter order: %#v", letters)
	}
	for _, dl := range letters {
		if dl.Attempts != 2 {
			t.Fatalf("expected attempts=2, got %d", dl.Attempts)
		}
		if dl.LastError == "" {
			t.Fatalf("expected non-empty last error")
		}
		if !strings.Contains(string(dl.Payload), "message.received") {
			t.Fatalf("unexpected payload: %s", string(dl.Payload))
		}
	}
}
