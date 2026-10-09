package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"reflect"
	"strings"
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

// Every resolver and session in this file is an in-memory per-call dependency.
// These tests never resolve DNS, dial a socket, or stand up an SMTP listener.
func TestR5NullMXPermanentWithoutSession(t *testing.T) {
	for _, requireTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("requireTLS=%v", requireTLS), func(t *testing.T) {
			t.Parallel()
			resolves, sessions := 0, 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"one@null.test", "two@null.test"}, []byte("synthetic"), requireTLS,
				func(_ context.Context, domain string) ([]*net.MX, error) {
					resolves++
					if domain != "null.test" {
						t.Fatalf("lookup domain=%q", domain)
					}
					return []*net.MX{{Pref: 0, Host: "."}}, nil
				},
				func(_ context.Context, host, addr, from string, to []string, mime []byte, tls bool) error {
					sessions++
					return fmt.Errorf("offline connect failure for host=%q addr=%q", host, addr)
				})
			var reply *textproto.Error
			if sessions != 0 || resolves != 1 || !errors.As(err, &reply) || reply.Code != 556 || !strings.HasPrefix(reply.Msg, "5.1.10 ") {
				t.Fatalf("null MX: resolves=%d sessions=%d error=%v reply=%+v", resolves, sessions, err, reply)
			}
		})
	}
}

func TestR5NullMXMalformedRootIsTemporaryWithoutSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records []*net.MX
	}{
		{"nonzero", []*net.MX{{Pref: 1, Host: "."}}},
		{"maximum_preference", []*net.MX{{Pref: 65535, Host: "."}}},
		{"duplicate", []*net.MX{{Pref: 0, Host: "."}, {Pref: 0, Host: "."}}},
		{"mixed_root_first", []*net.MX{{Pref: 0, Host: "."}, {Pref: 10, Host: "mx.good.test."}}},
		{"mixed_root_last", []*net.MX{{Pref: 10, Host: "mx.good.test."}, {Pref: 0, Host: "."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"one@invalid.test"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) { return tc.records, nil },
				func(context.Context, string, string, string, []string, []byte, bool) error {
					sessions++
					return nil
				})
			var reply *textproto.Error
			if err == nil || sessions != 0 || errors.As(err, &reply) {
				t.Fatalf("malformed root: sessions=%d error=%v reply=%+v", sessions, err, reply)
			}
		})
	}
}

func TestR5NullMXOrdinaryRoutingPreserved(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records []*net.MX
		hosts   []string
	}{
		{"single", []*net.MX{{Pref: 10, Host: "mx.one.test."}}, []string{"mx.one.test"}},
		{"multiple_resolver_order", []*net.MX{{Pref: 20, Host: "mx.first.test."}, {Pref: 10, Host: "mx.second.test."}}, []string{"mx.first.test", "mx.second.test"}},
		{"empty_success_fallback", nil, []string{"route.test"}},
	} {
		for _, requireTLS := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/requireTLS=%v", tc.name, requireTLS), func(t *testing.T) {
				t.Parallel()
				ctx := context.WithValue(context.Background(), struct{}{}, "per-call")
				from, to, mime := "sender@example.test", []string{"one@route.test", "two@route.test"}, []byte("synthetic bytes")
				var hosts []string
				err := deliverDirectWith(ctx, from, to, mime, requireTLS,
					func(gotCtx context.Context, domain string) ([]*net.MX, error) {
						if gotCtx != ctx || domain != "route.test" {
							t.Fatalf("resolver context/domain changed: %v %q", gotCtx, domain)
						}
						return tc.records, nil
					},
					func(gotCtx context.Context, host, addr, gotFrom string, gotTo []string, gotMIME []byte, tls bool) error {
						hosts = append(hosts, host)
						if gotCtx != ctx || addr != host+":25" || gotFrom != from || !reflect.DeepEqual(gotTo, to) || !reflect.DeepEqual(gotMIME, mime) || tls != requireTLS {
							t.Fatalf("session arguments changed: host=%q addr=%q from=%q to=%v mime=%q TLS=%v", host, addr, gotFrom, gotTo, gotMIME, tls)
						}
						if len(hosts) < len(tc.hosts) {
							return &textproto.Error{Code: 451, Msg: "4.3.0 next route"}
						}
						return nil
					})
				if err != nil || !reflect.DeepEqual(hosts, tc.hosts) {
					t.Fatalf("routing: hosts=%v want=%v error=%v", hosts, tc.hosts, err)
				}
			})
		}
	}
}

func TestR5NullMXResolverErrorsAndCancellationPreserved(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"timeout", &net.DNSError{Err: "offline timeout", Name: "error.test", IsTimeout: true, IsTemporary: true}},
		{"servfail", &net.DNSError{Err: "offline SERVFAIL", Name: "error.test", IsTemporary: true}},
		{"cancel", context.Canceled},
		{"deadline", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"one@error.test"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) { return nil, tc.err },
				func(context.Context, string, string, string, []string, []byte, bool) error { sessions++; return nil })
			var reply *textproto.Error
			if !errors.Is(err, tc.err) || errors.As(err, &reply) || sessions != 0 {
				t.Fatalf("resolver cause lost/class changed: sessions=%d error=%v", sessions, err)
			}
		})
	}
	t.Run("pre_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := deliverDirectWith(ctx, "sender@example.test", []string{"one@error.test"}, nil, false,
			func(context.Context, string) ([]*net.MX, error) {
				t.Fatal("canceled delivery resolved DNS")
				return nil, nil
			},
			func(context.Context, string, string, string, []string, []byte, bool) error {
				t.Fatal("canceled delivery opened session")
				return nil
			})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-cancel cause=%v", err)
		}
	})
}

func TestR5NullMXSessionErrorControls(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		attempts int
		code     int
	}{
		{"temporary_reply", &textproto.Error{Code: 451, Msg: "4.3.0 temporary"}, 2, 451},
		{"permanent_reply", &textproto.Error{Code: 550, Msg: "5.1.1 rejected"}, 2, 550},
		{"connect_failure", errors.New("offline connect failure"), 2, 0},
		{"uncertain_final_reply", fmt.Errorf("lost final reply: %w", store.ErrOutboundUncertain), 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"one@route.test"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) {
					return []*net.MX{{Host: "first.test."}, {Host: "second.test."}}, nil
				},
				func(context.Context, string, string, string, []string, []byte, bool) error { sessions++; return tc.err })
			var reply *textproto.Error
			code := 0
			if errors.As(err, &reply) {
				code = reply.Code
			}
			if sessions != tc.attempts || !errors.Is(err, tc.err) || code != tc.code {
				t.Fatalf("session error changed: sessions=%d want=%d error=%v code=%d", sessions, tc.attempts, err, code)
			}
		})
	}
	t.Run("cancel_during_session_stops_fallback", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sessions := 0
		err := deliverDirectWith(ctx, "sender@example.test", []string{"one@route.test"}, nil, true,
			func(context.Context, string) ([]*net.MX, error) {
				return []*net.MX{{Host: "first.test."}, {Host: "second.test."}}, nil
			},
			func(context.Context, string, string, string, []string, []byte, bool) error {
				sessions++
				cancel()
				return context.Canceled
			})
		if sessions != 1 || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation retried: sessions=%d error=%v", sessions, err)
		}
	})
}

type r5NullMXLedgerStore struct {
	*testutil.FakeStore
	begin, complete map[string]int
}

func (s *r5NullMXLedgerStore) BeginOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address string) (bool, error) {
	s.begin[address]++
	return s.FakeStore.BeginOutboundRecipient(ctx, id, token, address)
}

func (s *r5NullMXLedgerStore) CompleteOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address, state string, code int, diagnostic string) error {
	s.complete[address]++
	return s.FakeStore.CompleteOutboundRecipient(ctx, id, token, address, state, code, diagnostic)
}

type r5NullMXLedgerAdapter struct {
	calls, sessions map[string]int
}

func (*r5NullMXLedgerAdapter) Name() string { return "offline-null-mx" }
func (a *r5NullMXLedgerAdapter) Deliver(ctx context.Context, j *models.OutboundJob, mime []byte) (*DeliveryResult, error) {
	for _, rcpt := range j.RcptTo {
		a.calls[rcpt]++
	}
	err := deliverDirectWith(ctx, j.MailFrom, j.RcptTo, mime, true,
		func(_ context.Context, domain string) ([]*net.MX, error) {
			if domain == "null.test" {
				return []*net.MX{{Pref: 0, Host: "."}}, nil
			}
			return []*net.MX{{Pref: 10, Host: "mx." + domain + "."}}, nil
		},
		func(_ context.Context, host, addr, from string, to []string, mime []byte, requireTLS bool) error {
			a.sessions[to[0]]++
			if host == "" || (to[0] == "retry@temporary.test" && a.sessions[to[0]] == 1) {
				return &textproto.Error{Code: 451, Msg: "4.3.0 offline temporary failure"}
			}
			return nil
		})
	return nil, err
}

func TestR5NullMXRecipientPermanentAndRetryIdempotency(t *testing.T) {
	ctx := context.Background()
	st := &r5NullMXLedgerStore{FakeStore: testutil.NewFakeStore(), begin: map[string]int{}, complete: map[string]int{}}
	req := quotaTestSendRequest(uuid.New(), uuid.New())
	seedQuotaSender(t, st.FakeStore, &req)
	req.To = []string{"accepted@already.test", "null@null.test", "pending@valid.test", "retry@temporary.test"}
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 5, RetryDelay: time.Nanosecond}, st, nopGovernance{}, zerolog.Nop())
	a := &r5NullMXLedgerAdapter{calls: map[string]int{}, sessions: map[string]int{}}
	svc.adapter = a
	job, err := svc.Submit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimOutboundJobs(ctx, time.Now(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: jobs=%v error=%v", claimed, err)
	}
	j := claimed[0]
	started, err := st.FakeStore.BeginOutboundRecipient(ctx, j.ID, j.DeliveryToken, "accepted@already.test")
	if err != nil || !started {
		t.Fatalf("seed accepted begin: started=%v error=%v", started, err)
	}
	if err := st.FakeStore.CompleteOutboundRecipient(ctx, j.ID, j.DeliveryToken, "accepted@already.test", delivery.Accepted, 250, "already accepted"); err != nil {
		t.Fatal(err)
	}
	if err := svc.deliverRecipients(ctx, j, j.DeliveryToken, []byte("synthetic")); err == nil {
		t.Fatal("temporary sibling did not keep ordinary retry path")
	}
	assertLedger := func(wantRetry string) {
		t.Helper()
		rows, err := st.ListOutboundRecipients(ctx, job.TenantID, job.ID)
		if err != nil || len(rows) != 4 {
			t.Fatalf("ledger: rows=%v error=%v", rows, err)
		}
		want := map[string]string{"accepted@already.test": delivery.Accepted, "null@null.test": delivery.Permanent, "pending@valid.test": delivery.Accepted, "retry@temporary.test": wantRetry}
		for _, row := range rows {
			if row.State != want[row.Address] {
				t.Fatalf("recipient %q state=%q want=%q code=%d", row.Address, row.State, want[row.Address], row.SMTPCode)
			}
			if row.Address == "null@null.test" && (row.SMTPCode != 556 || !strings.Contains(row.Diagnostic, "5.1.10") || row.Attempts != 1) {
				t.Fatalf("null-MX permanent checkpoint=%+v", row)
			}
		}
	}
	assertLedger(delivery.Temporary)
	if err := st.MarkOutboundJobRetry(ctx, j.ID, j.DeliveryToken, "temporary sibling", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	assertLedger(delivery.Accepted)
	stored, err := st.GetOutboundJob(ctx, job.ID)
	if err != nil || stored.State != models.OutboundFailed || !strings.Contains(stored.LastError, "permanently rejected") || stored.InFlightDomain != "" {
		t.Fatalf("ordinary permanent-failure terminal path: job=%+v error=%v", stored, err)
	}
	for _, address := range req.To {
		want := 1
		if address == "accepted@already.test" {
			want = 0
		} else if address == "retry@temporary.test" {
			want = 2
		}
		if a.calls[address] != want || st.begin[address] != want || st.complete[address] != want {
			t.Fatalf("recipient %q repeated/missing delivery: adapter=%d begin=%d complete=%d want=%d", address, a.calls[address], st.begin[address], st.complete[address], want)
		}
	}
	if a.sessions["null@null.test"] != 0 {
		t.Fatalf("null MX opened %d sessions", a.sessions["null@null.test"])
	}
	svc.ensureWorker().ProcessBatch(ctx)
	if a.calls["null@null.test"] != 1 || a.calls["pending@valid.test"] != 1 || a.calls["retry@temporary.test"] != 2 {
		t.Fatalf("terminal job retransmitted: %v", a.calls)
	}
	// Existing manual job requeue does not reset completed recipient states.
	// Even a deliberately reprocessed job must never touch the adapter again.
	if err := st.RequeueOutboundJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	assertLedger(delivery.Accepted)
	stored, err = st.GetOutboundJob(ctx, job.ID)
	if err != nil || stored.State != models.OutboundFailed {
		t.Fatalf("requeued completed ledger: job=%+v error=%v", stored, err)
	}
	if a.calls["accepted@already.test"] != 0 || a.calls["null@null.test"] != 1 || a.calls["pending@valid.test"] != 1 || a.calls["retry@temporary.test"] != 2 || a.sessions["null@null.test"] != 0 {
		t.Fatalf("manual requeue retransmitted completed recipients: calls=%v sessions=%v", a.calls, a.sessions)
	}
}
