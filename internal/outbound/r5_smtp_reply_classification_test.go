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

	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// The existing bounded net.Pipe fixture reads the complete DATA terminator
// before writing this final reply. The real net/smtp parser, not a constructed
// textproto.Error, determines the error passed to the production callers.
func r5SMTPFinalReply(t *testing.T, reply string) error {
	t.Helper()
	return r5SMTPCauseRun(t, context.Background(), "final", func(_ net.Conn, peer net.Conn, _ *bufio.Reader) error {
		if reply == "" {
			return nil
		}
		_, err := io.WriteString(peer, reply)
		return err
	}, nil)
}

func TestR5SMTPFinalReplyClassification(t *testing.T) {
	for _, tc := range []struct {
		code      int
		uncertain bool
	}{
		{100, true}, {199, true},
		{200, true}, {220, true}, {221, true}, {249, true},
		{250, false}, {251, true}, {252, true}, {299, true},
		{300, true}, {354, true}, {399, true},
		{400, false}, {421, false}, {451, false}, {499, false},
		{500, false}, {550, false}, {599, false},
		{600, true}, {999, true},
	} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			err := r5SMTPFinalReply(t, fmt.Sprintf("%d final response\r\n", tc.code))
			if tc.code == 250 {
				if err != nil {
					t.Fatalf("acknowledged DATA became retryable after peer cleanup: %v", err)
				}
				return
			}
			var reply *textproto.Error
			if !errors.As(err, &reply) || reply.Code != tc.code || reply.Msg != "final response" {
				t.Fatalf("DATA final reply lost its original protocol cause: %v", err)
			}
			if got := errors.Is(err, store.ErrOutboundUncertain); got != tc.uncertain {
				t.Fatalf("DATA final %d uncertainty=%t want=%t: %v", tc.code, got, tc.uncertain, err)
			}
		})
	}
	for _, raw := range []string{"000 final response\r\n", "099 final response\r\n", "garbled reply\r\n"} {
		t.Run(fmt.Sprintf("malformed_%q", raw), func(t *testing.T) {
			err := r5SMTPFinalReply(t, raw)
			var protocol textproto.ProtocolError
			if !errors.Is(err, store.ErrOutboundUncertain) || !errors.As(err, &protocol) {
				t.Fatalf("malformed DATA final reply lost uncertainty or protocol cause: %v", err)
			}
		})
	}
}

func TestR5SMTPFinalReplyStopsMXFallback(t *testing.T) {
	for _, raw := range []string{"251 may have accepted\r\n", "354 send data again\r\n", "999 invalid code\r\n", "garbled reply\r\n", ""} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			sessions := 0
			var firstErr error
			err := deliverDirectWith(context.Background(), "from@example.test", []string{"to@example.test"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) {
					return []*net.MX{{Pref: 10, Host: "first.invalid."}, {Pref: 20, Host: "second.invalid."}}, nil
				},
				func(context.Context, string, string, string, []string, []byte, bool) error {
					sessions++
					if sessions == 1 {
						firstErr = r5SMTPFinalReply(t, raw)
						return firstErr
					}
					return nil
				})
			if sessions != 1 || !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, firstErr) {
				t.Fatalf("ambiguous DATA final reply tried another MX or lost cause: sessions=%d error=%v first=%v", sessions, err, firstErr)
			}
		})
	}
}

// Feed the actual parsed SMTP result through the shipping recipient loop. The
// store is the existing in-memory fixture; this is not PostgreSQL evidence.
func TestR5SMTPFinalReplyRecipientClassification(t *testing.T) {
	for _, tc := range []struct {
		reply string
		state string
	}{
		{"250 accepted\r\n", delivery.Accepted},
		{"451 temporary rejection\r\n", delivery.Temporary},
		{"550 permanent rejection\r\n", delivery.Permanent},
		{"251 may have accepted\r\n", delivery.Uncertain},
		{"354 unexpected continuation\r\n", delivery.Uncertain},
		{"999 invalid code\r\n", delivery.Uncertain},
		{"garbled reply\r\n", delivery.Uncertain},
		{"", delivery.Uncertain},
	} {
		t.Run(fmt.Sprintf("%q", tc.reply), func(t *testing.T) {
			st, svc, job, adapter := r5CheckpointOrderFixture(t)
			adapter.err = r5SMTPFinalReply(t, tc.reply)
			err := svc.deliverRecipients(context.Background(), job, job.DeliveryToken, nil)
			if tc.state == delivery.Temporary {
				if err == nil {
					t.Fatal("temporary rejection lost the ordinary retry path")
				}
			} else if err != nil {
				t.Fatalf("recipient finalization: %v", err)
			}
			if got := r5CheckpointOrderRecipient(t, st, job); got != tc.state {
				t.Fatalf("actual DATA final reply classified as %s, want %s", got, tc.state)
			}
			if tc.state != delivery.Uncertain {
				return
			}
			stored, err := st.GetOutboundJob(context.Background(), job.ID)
			if err != nil || stored.State != models.OutboundFailed || stored.InFlightDomain == "" {
				t.Fatalf("unknown SMTP acceptance lost its terminal hold: job=%+v error=%v", stored, err)
			}
			if err := svc.ValidateJobAuthorization(context.Background(), stored); !errors.Is(err, store.ErrOutboundUncertain) {
				t.Fatalf("retry validation lost uncertainty: %v", err)
			}
			if err := st.RequeueOutboundJob(context.Background(), job.ID); !errors.Is(err, store.ErrOutboundNotRetryable) {
				t.Fatalf("unknown acceptance allowed manual requeue: %v", err)
			}
			svc.ensureWorker().ProcessBatch(context.Background())
			if adapter.calls != 1 {
				t.Fatalf("unknown acceptance delivered again: calls=%d", adapter.calls)
			}
		})
	}
}
