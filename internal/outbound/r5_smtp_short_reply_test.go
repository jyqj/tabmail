package outbound

import (
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

// The shipping net/smtp client receives these short fragments only after the
// fixture has read the complete DATA terminator. No byte budget is exhausted.
func TestR5AdvanceSMTPShortFinalReply(t *testing.T) {
	for _, code := range []int{250, 251, 451, 550} {
		for _, ending := range []string{"", "\r"} {
			t.Run(fmt.Sprintf("%d/%q", code, ending), func(t *testing.T) {
				err := r5SMTPFinalReply(t, fmt.Sprintf("%d final response%s", code, ending))
				var protocol *textproto.Error
				if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, io.EOF) || errors.As(err, &protocol) {
					t.Fatalf("truncated final response became authoritative or lost EOF: %v", err)
				}
			})
		}
	}
	for _, code := range []int{250, 451, 550} {
		t.Run(fmt.Sprintf("complete/%d", code), func(t *testing.T) {
			err := r5SMTPFinalReply(t, fmt.Sprintf("%d final response\r\n", code))
			if code == 250 {
				if err != nil {
					t.Fatalf("complete acceptance became retryable: %v", err)
				}
				return
			}
			var protocol *textproto.Error
			if errors.Is(err, store.ErrOutboundUncertain) || !errors.As(err, &protocol) || protocol.Code != code {
				t.Fatalf("complete negative response changed classification: %v", err)
			}
		})
	}
}

func TestR5AdvanceSMTPShortFinalReplyStopsRedelivery(t *testing.T) {
	for _, code := range []int{250, 451, 550} {
		t.Run(fmt.Sprintf("mx/%d", code), func(t *testing.T) {
			calls := 0
			err := deliverDirectWith(context.Background(), "from@example.test", []string{"to@example.test"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) {
					return []*net.MX{{Host: "first.invalid."}, {Host: "second.invalid."}}, nil
				},
				func(context.Context, string, string, string, []string, []byte, bool) error {
					calls++
					if calls == 1 {
						return r5SMTPFinalReply(t, fmt.Sprintf("%d final response", code))
					}
					return nil
				})
			if calls != 1 || !errors.Is(err, store.ErrOutboundUncertain) {
				t.Fatalf("truncated final status permitted acceptance or another MX: calls=%d error=%v", calls, err)
			}
		})
		t.Run(fmt.Sprintf("recipient/%d", code), func(t *testing.T) {
			st, svc, job, adapter := r5CheckpointOrderFixture(t)
			adapter.err = r5SMTPFinalReply(t, fmt.Sprintf("%d final response", code))
			err := svc.deliverRecipients(context.Background(), job, job.DeliveryToken, nil)
			if err != nil {
				t.Errorf("uncertain recipient finalization failed: %v", err)
			}
			if state := r5CheckpointOrderRecipient(t, st, job); state != delivery.Uncertain {
				t.Errorf("truncated final response stored as %s", state)
			}
			stored, err := st.GetOutboundJob(context.Background(), job.ID)
			if err != nil || stored.State != models.OutboundFailed || stored.InFlightDomain == "" {
				t.Fatalf("truncated response lost its recovery hold: state=%+v error=%v", stored, err)
			}
			if err := svc.ValidateJobAuthorization(context.Background(), stored); !errors.Is(err, store.ErrOutboundUncertain) {
				t.Errorf("authorization permits retry after truncated response: %v", err)
			}
			if err := st.RequeueOutboundJob(context.Background(), job.ID); !errors.Is(err, store.ErrOutboundNotRetryable) {
				t.Errorf("manual retry permitted after truncated response: %v", err)
			}
			svc.ensureWorker().ProcessBatch(context.Background())
			if adapter.calls != 1 {
				t.Errorf("worker repeated delivery after truncated response: %d", adapter.calls)
			}
		})
	}
}
