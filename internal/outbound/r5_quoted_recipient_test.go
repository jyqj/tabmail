package outbound

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/config"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

var r5QuotedRecipientCases = []struct{ name, input, envelope, identity string }{
	{"at", `"Team@Desk"@Recipient.test`, `"team@desk"@recipient.test`, "team@desk@recipient.test"},
	{"space", `"Team Desk"@Recipient.test`, `"team desk"@recipient.test`, "team desk@recipient.test"},
	{"comma", `"Team,Desk"@Recipient.test`, `"team,desk"@recipient.test`, "team,desk@recipient.test"},
	{"quote", `"Team\"Desk"@Recipient.test`, `"team\"desk"@recipient.test`, `team"desk@recipient.test`},
	{"backslash", `"Team\\Desk"@Recipient.test`, `"team\\desk"@recipient.test`, `team\desk@recipient.test`},
	{"unnecessary_quote_control", `"Simple"@Recipient.test`, "simple@recipient.test", "simple@recipient.test"},
	{"display_control", `Person <Simple+Tag@Recipient.test>`, "simple+tag@recipient.test", "simple+tag@recipient.test"},
}

type r5QuotedAdapter func(context.Context, *models.OutboundJob, []byte) (*DeliveryResult, error)

func (r5QuotedAdapter) Name() string { return "controlled-quoted-recipient-peer" }
func (f r5QuotedAdapter) Deliver(ctx context.Context, j *models.OutboundJob, raw []byte) (*DeliveryResult, error) {
	return f(ctx, j, raw)
}

type r5QuotedPeerResult struct {
	address string
	raw     []byte
	err     error
}

// The complete SMTP conversation runs over an owned TCP socket. SMTP and MIME
// production code are unchanged by the fixture; only DNS and the destination
// port are locally supplied. The metadata ledger is the existing FakeStore.
func r5QuotedPeer(t *testing.T, recipient string) (string, <-chan r5QuotedPeerResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan r5QuotedPeerResult, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out := r5QuotedPeerResult{}
		out.err = func() error {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			reader := textproto.NewReader(bufio.NewReader(conn))
			write := func(line string) error { _, err := fmt.Fprint(conn, line+"\r\n"); return err }
			if err := write("220 loopback.test ESMTP"); err != nil {
				return err
			}
			for _, exchange := range []struct{ command, response string }{
				{"EHLO localhost", "250 loopback.test"},
				{"MAIL FROM:<sender@example.test>", "250 sender accepted"},
				{"RCPT TO:<" + recipient + ">", "250 recipient accepted"},
				{"DATA", "354 message follows"},
			} {
				line, err := reader.ReadLine()
				if err != nil {
					return err
				}
				if line != exchange.command {
					return fmt.Errorf("SMTP command = %q, want %q", line, exchange.command)
				}
				if strings.HasPrefix(line, "RCPT TO:") {
					out.address = strings.TrimSuffix(strings.TrimPrefix(line, "RCPT TO:<"), ">")
				}
				if err := write(exchange.response); err != nil {
					return err
				}
			}
			out.raw, err = reader.ReadDotBytes()
			if err != nil {
				return err
			}
			if err := write("250 accepted"); err != nil {
				return err
			}
			if line, err := reader.ReadLine(); err != nil || line != "QUIT" {
				return fmt.Errorf("QUIT = %q: %w", line, err)
			}
			return write("221 bye")
		}()
		result <- out
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("quoted-recipient peer did not exit")
		}
	})
	return listener.Addr().String(), result
}

func TestR5QuotedRecipientSubmitLedgerMIMEAndSMTP(t *testing.T) {
	for _, tc := range r5QuotedRecipientCases {
		for role, roleName := range []string{"to", "cc", "bcc"} {
			for _, mode := range []string{"relay", "direct"} {
				t.Run(tc.name+"/"+roleName+"/"+mode, func(t *testing.T) {
					st := testutil.NewFakeStore()
					req := quotaTestSendRequest(uuid.New(), uuid.New())
					seedQuotaSender(t, st, &req)
					req.To, req.CC, req.BCC = nil, nil, nil
					groups := []*[]string{&req.To, &req.CC, &req.BCC}
					*groups[role] = []string{tc.input}
					svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, nopGovernance{}, zerolog.Nop())
					job, err := svc.Submit(context.Background(), req)
					if err != nil {
						t.Fatalf("valid quoted recipient was rejected before enqueue: %v", err)
					}
					if !reflect.DeepEqual(job.RcptTo, []string{tc.envelope}) || (*groups[role])[0] != tc.input {
						t.Fatalf("recipient identity or caller-owned input changed: envelope=%q input=%q", job.RcptTo, *groups[role])
					}
					persisted, err := st.GetOutboundJob(context.Background(), job.ID)
					if err != nil || !reflect.DeepEqual(persisted.RcptTo, job.RcptTo) {
						t.Fatalf("queue identity = %+v: %v", persisted, err)
					}
					addr, peerDone := r5QuotedPeer(t, tc.envelope)
					var dnsDomains []string
					if mode == "relay" {
						host, port, err := net.SplitHostPort(addr)
						if err != nil {
							t.Fatal(err)
						}
						number, err := strconv.Atoi(port)
						if err != nil {
							t.Fatal(err)
						}
						svc.adapter = NewRelayAdapter(config.Outbound{RelayHost: host, RelayPort: number, RelayTLS: "none"})
					} else {
						dns := (&r5ImplicitDNS{mx: "ordinary"}).resolver(t)
						lookup := smtpMXLookup(dns)
						svc.adapter = r5QuotedAdapter(func(ctx context.Context, current *models.OutboundJob, raw []byte) (*DeliveryResult, error) {
							err := deliverDirectWith(ctx, current.MailFrom, current.RcptTo, raw, false,
								func(ctx context.Context, domain string) ([]*net.MX, error) {
									dnsDomains = append(dnsDomains, domain)
									return lookup(ctx, domain)
								},
								func(ctx context.Context, host, _ string, from string, to []string, raw []byte, required bool) error {
									if host != "mx.explicit.test" {
										return fmt.Errorf("unexpected actual DNS exchange %q", host)
									}
									return deliverDirectMX(ctx, host, addr, from, to, raw, required)
								})
							return &DeliveryResult{Adapter: "direct_mx", SMTPCode: 250}, err
						})
					}
					claimed, err := st.ClaimOutboundJobs(context.Background(), time.Now(), 1)
					if err != nil || len(claimed) != 1 {
						t.Fatalf("claim: %v %v", claimed, err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
					defer cancel()
					work := &workqueue.Job[*outboundJob]{ID: job.ID, Attempts: claimed[0].Attempts,
						Payload: &outboundJob{OutboundJob: claimed[0]}, Lease: workqueue.Lease{Token: claimed[0].DeliveryToken}}
					if err := svc.processOne(ctx, work); err != nil {
						t.Fatalf("queued recipient delivery failed: %v", err)
					}
					var peer r5QuotedPeerResult
					select {
					case peer = <-peerDone:
					case <-ctx.Done():
						t.Fatal("actual SMTP peer did not complete")
					}
					if peer.err != nil || peer.address != tc.envelope {
						t.Fatalf("SMTP identity=%q: %v", peer.address, peer.err)
					}
					if mode == "direct" && !reflect.DeepEqual(dnsDomains, []string{"recipient.test"}) {
						t.Errorf("DNS used the quoted local part: %q", dnsDomains)
					}
					message, err := mail.ReadMessage(bytes.NewReader(peer.raw))
					if err != nil {
						t.Fatal(err)
					}
					if message.Header.Get("Bcc") != "" {
						t.Error("BCC leaked to MIME")
					}
					if role < 2 {
						addresses, err := message.Header.AddressList([]string{"To", "Cc"}[role])
						if err != nil || len(addresses) != 1 || addresses[0].Address != tc.identity {
							t.Errorf("MIME changed address meaning: %v %v", addresses, err)
						}
					} else if bytes.Contains(peer.raw, []byte(tc.envelope)) || bytes.Contains(peer.raw, []byte(tc.identity)) {
						t.Error("BCC identity appeared in wire content")
					}
					rows, err := st.ListOutboundRecipients(ctx, job.TenantID, job.ID)
					if err != nil || len(rows) != 1 || rows[0].Address != tc.envelope || rows[0].State != delivery.Accepted {
						t.Errorf("recipient completion=%+v: %v", rows, err)
					}
					persisted, err = st.GetOutboundJob(ctx, job.ID)
					if err != nil || persisted.State != models.OutboundSent {
						t.Errorf("job did not reach real sent transition: %+v %v", persisted, err)
					}
				})
			}
		}
	}
}

type r5QuotedReplayStore struct{ *testutil.FakeStore }

// Sequential fake persistence lookup tests the production request digest and
// replay path. It makes no claim about PostgreSQL idempotency locking.
func (s *r5QuotedReplayStore) FindOutboundSubmission(ctx context.Context, tenant uuid.UUID, actor, key, hash string) (*models.OutboundJob, error) {
	jobs, _, err := s.ListOutboundJobsScoped(ctx, authz.OwnerListFilter{TenantID: tenant, AllInTenant: true}, models.Page{Page: 1, PerPage: 100})
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.SubmitActor == actor && job.IdempotencyKey == key {
			if job.RequestHash != hash {
				return nil, app.Conflict("different request")
			}
			return job, nil
		}
	}
	return nil, nil
}

func TestR5QuotedRecipientEquivalentSpellingReplay(t *testing.T) {
	for _, tc := range r5QuotedRecipientCases {
		t.Run(tc.name, func(t *testing.T) {
			st := &r5QuotedReplayStore{testutil.NewFakeStore()}
			req := quotaTestSendRequest(uuid.New(), uuid.New())
			seedQuotaSender(t, st.FakeStore, &req)
			req.To, req.IdempotencyKey = []string{tc.input}, "same-quoted-command"
			svc := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop())
			first, replay, err := svc.SubmitWithReplay(context.Background(), req)
			if err != nil || replay {
				t.Fatalf("first submission=%+v replay=%v: %v", first, replay, err)
			}
			req.To = []string{"Another Display <" + tc.envelope + ">"}
			second, replay, err := svc.SubmitWithReplay(context.Background(), req)
			if err != nil || !replay || second.ID != first.ID {
				t.Fatalf("equivalent spelling did not replay: %+v %v %v", second, replay, err)
			}
			count, err := st.CountOutboundSince(context.Background(), req.TenantID, req.UserID, time.Time{})
			if err != nil || count != 1 {
				t.Errorf("replay enqueued another message: %d %v", count, err)
			}
		})
	}
}

func TestR5QuotedRecipientWorkerSuppressionIdentity(t *testing.T) {
	for _, tc := range r5QuotedRecipientCases {
		t.Run(tc.name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			req := quotaTestSendRequest(uuid.New(), uuid.New())
			seedQuotaSender(t, st, &req)
			svc := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop())
			job, err := svc.Submit(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			// A persisted canonical recipient is independent of Submit's parsing.
			// This exposes the worker's identity check even on the old submit code.
			job.To, job.RcptTo = []string{tc.envelope}, []string{tc.envelope}
			job.ContentDigest = contentDigest(job)
			if err := svc.ValidateJobAuthorization(context.Background(), job); err != nil {
				t.Fatalf("synthetic persisted recipient must reach suppression: %v", err)
			}
			if err := st.CreateOutboundJob(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if err := st.AddSuppression(context.Background(), &models.SuppressionEntry{TenantID: req.TenantID, Address: tc.identity, Reason: "synthetic bounce"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			svc.adapter = r5QuotedAdapter(func(context.Context, *models.OutboundJob, []byte) (*DeliveryResult, error) {
				calls++
				return &DeliveryResult{SMTPCode: 250}, nil
			})
			claimed, err := st.ClaimOutboundJobs(context.Background(), time.Now(), 1)
			if err != nil || len(claimed) != 1 {
				t.Fatalf("claim: %v %v", claimed, err)
			}
			if err := svc.deliverRecipients(context.Background(), claimed[0], claimed[0].DeliveryToken, nil); err != nil {
				t.Fatal(err)
			}
			rows, err := st.ListOutboundRecipients(context.Background(), req.TenantID, job.ID)
			if calls != 0 || err != nil || len(rows) != 1 || rows[0].State != delivery.Permanent {
				t.Fatalf("suppression identity bypass: adapter=%d rows=%+v err=%v", calls, rows, err)
			}
		})
	}
}
