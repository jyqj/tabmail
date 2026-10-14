package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

func r5NextHeaderMessage(headers map[string]string) Message {
	return Message{From: "sender@example.test", To: []string{"recipient@example.test"}, Subject: "Header boundary", TextBody: "message body", MessageID: "<header-boundary@example.test>", Headers: headers}
}

// RFC 5322 sections 2.1.1 and 2.2 define the wire line budget and allowed
// field-body controls. These inputs are frozen independently of product helpers.
func TestR5NextCustomHeaderRejectsInvalidWireValues(t *testing.T) {
	for b := 0; b <= 127; b++ {
		if b == '\t' || b == '\r' || b == '\n' || (b >= 32 && b < 127) {
			continue
		}
		t.Run(fmt.Sprintf("control-%02x", b), func(t *testing.T) {
			raw, err := Build(r5NextHeaderMessage(map[string]string{"X-Client-Value": "left" + string(byte(b)) + "right"}))
			if err == nil || raw != nil {
				t.Fatalf("forbidden control %02x produced sendable MIME", b)
			}
			if strings.Contains(err.Error(), "left") || strings.Contains(err.Error(), "right") {
				t.Fatal("header validation error disclosed its private value")
			}
		})
	}
	for name, value := range map[string]string{"invalid-utf8": string([]byte{0xff}), "truncated-utf8": string([]byte{0xe4, 0xbd})} {
		t.Run(name, func(t *testing.T) {
			raw, err := Build(r5NextHeaderMessage(map[string]string{"X-Client-Value": value}))
			if err == nil || raw != nil {
				t.Fatal("invalid UTF-8 produced sendable MIME")
			}
		})
	}
}

func TestR5NextCustomHeaderWireLength(t *testing.T) {
	for _, key := range []string{"X", "X-Audit", strings.Repeat("X", 126)} {
		for _, length := range []int{997, 998, 999, 4096} {
			t.Run(fmt.Sprintf("key-%d/line-%d", len(key), length), func(t *testing.T) {
				value := strings.Repeat("a", length-len(key)-2)
				raw, err := Build(r5NextHeaderMessage(map[string]string{key: value}))
				if length > 998 {
					if err == nil || raw != nil {
						t.Fatal("overlong custom field produced sendable MIME")
					}
					return
				}
				if err != nil || !bytes.Contains(raw, []byte(key+": "+value+"\r\n")) {
					t.Fatalf("legal boundary was rejected or rewritten: %v", err)
				}
			})
		}
	}
	for _, n := range []int{330, 331} {
		t.Run(fmt.Sprintf("utf8-%d", n), func(t *testing.T) {
			value := strings.Repeat("信", n)
			raw, err := Build(r5NextHeaderMessage(map[string]string{"X-Utf8": value}))
			if n == 331 {
				if err == nil || raw != nil {
					t.Fatal("line budget counted Unicode runes instead of wire octets")
				}
				return
			}
			if err != nil || !bytes.Contains(raw, []byte("X-Utf8: "+value+"\r\n")) {
				t.Fatalf("valid UTF-8 field changed: %v", err)
			}
		})
	}
}

func TestR5NextCustomHeaderCompatibility(t *testing.T) {
	input := map[string]string{
		"Reply-To": "Support <support@example.test>", "References": "<one@example.test> <two@example.test>",
		"X-Spacing": "one\t  two", "X-Unicode": "邮件通知", "X-Empty": "",
		"X-Sanitized": "value\r\ncontinued", "Bcc": "ignored\x00private", "Bad Header": strings.Repeat("x", 4096),
	}
	before, _ := json.Marshal(input)
	raw, err := Build(r5NextHeaderMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"Reply-To": input["Reply-To"], "References": input["References"], "X-Spacing": input["X-Spacing"], "X-Unicode": input["X-Unicode"], "X-Sanitized": "valuecontinued"} {
		if got := message.Header.Get(key); got != want {
			t.Errorf("legal field %s changed: got=%q want=%q", key, got, want)
		}
	}
	if message.Header.Get("Bcc") != "" || bytes.Contains(raw, []byte("ignored")) || bytes.Contains(raw, []byte("Bad Header:")) {
		t.Fatal("ignored header acquired wire authority")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("header validation mutated the caller's map")
	}
}

type r5NextHeaderEnqueueStore struct {
	*r5QuotedReplayStore
	enqueues int
}

func (s *r5NextHeaderEnqueueStore) CreateOutboundJobAuthorized(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation, draft *store.DraftConsumption, validate store.OutboundEnqueueValidator) (bool, error) {
	s.enqueues++
	return s.FakeStore.CreateOutboundJobAuthorized(ctx, job, quota, draft, validate)
}

func TestR5NextCustomHeaderSubmitBeforeEnqueue(t *testing.T) {
	for name, bad := range map[string]string{"nul": "private\x00value", "del": "private\x7fvalue", "overlong": strings.Repeat("a", 1000)} {
		t.Run(name, func(t *testing.T) {
			st := &r5NextHeaderEnqueueStore{r5QuotedReplayStore: &r5QuotedReplayStore{FakeStore: testutil.NewFakeStore()}}
			req := quotaTestSendRequest(uuid.New(), uuid.New())
			seedQuotaSender(t, st.FakeStore, &req)
			req.Headers = map[string]string{"X-Client-Value": bad}
			req.IdempotencyKey = "header-boundary-retry"
			req.Quota.UserDaily = &store.OutboundUserDailyQuota{UserID: req.UserID, Since: time.Now().Add(-time.Hour), Limit: 1}
			svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, nopGovernance{}, zerolog.Nop())
			job, err := svc.Submit(context.Background(), req)
			if err == nil || job != nil || st.enqueues != 0 {
				t.Errorf("invalid header reached transactional enqueue: job=%v err=%v calls=%d", job != nil, err, st.enqueues)
			}
			if typed, ok := app.As(err); !ok || typed.Kind != app.KindBadRequest {
				t.Errorf("invalid header did not return the HTTP-400 app classification: %v", err)
			}
			_, total, countErr := st.ListOutboundJobsScoped(context.Background(), authz.OwnerListFilter{TenantID: req.TenantID, AllInTenant: true}, models.Page{Page: 1, PerPage: 10})
			if countErr != nil || total != 0 {
				t.Errorf("invalid header consumed a durable submission: total=%d err=%v", total, countErr)
			}
			req.Headers["X-Client-Value"] = "corrected"
			job, err = svc.Submit(context.Background(), req)
			if err != nil || job == nil || st.enqueues != 1 {
				t.Fatalf("corrected same-key submission lost its quota or idempotency key: job=%v err=%v calls=%d", job != nil, err, st.enqueues)
			}
		})
	}
}

func TestR5NextCustomHeaderLegacyJobRebuild(t *testing.T) {
	raw, err := Build(messageFromJob(&models.OutboundJob{MailFrom: "sender@example.test", To: []string{"recipient@example.test"}, Subject: "Legacy queue", TextBody: "body", HeadersJSON: json.RawMessage(`{"X-Client-Value":"legacy\u0000value"}`)}))
	if err == nil || raw != nil {
		t.Fatal("legacy queued invalid custom header escaped the shared builder")
	}
}

func TestR5NextCustomHeaderSMTPBoundary(t *testing.T) {
	for _, mode := range []string{"relay", "direct"} {
		t.Run(mode, func(t *testing.T) {
			msg := r5NextHeaderMessage(map[string]string{"X-Audit": strings.Repeat("a", 989), "References": "<one@example.test> <two@example.test>"})
			raw, err := Build(msg)
			if err != nil {
				t.Fatal(err)
			}
			addr, peer := r5AdvanceHeaderPeer(t, msg.To)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "relay" {
				tcp := addr.(*net.TCPAddr)
				err = DeliverRelay(ctx, config.Outbound{RelayHost: tcp.IP.String(), RelayPort: tcp.Port, RelayTLS: "none"}, msg.From, msg.To, raw)
			} else {
				err = deliverDirectMX(ctx, "127.0.0.1", addr.String(), msg.From, msg.To, raw, false)
			}
			if err != nil {
				t.Fatalf("legal custom fields rejected by actual SMTP: %v", err)
			}
			got := <-peer
			if got.err != nil {
				t.Fatal(got.err)
			}
			message, err := mail.ReadMessage(bytes.NewReader(got.raw))
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range msg.Headers {
				if !reflect.DeepEqual(message.Header.Get(key), want) {
					t.Fatalf("SMTP changed %s", key)
				}
			}
		})
	}
}
