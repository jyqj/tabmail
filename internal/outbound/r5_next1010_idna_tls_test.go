package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/mail"
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
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

type next1010IDNAWire struct {
	sni string
	raw []byte
	err error
}

// Trust one private certificate with only the expected A-label DNS SAN. The
// client must verify that name and send it as SNI. Only the final socket address
// is redirected to this owned loopback listener; SMTP and STARTTLS are real.
func next1010IDNAPeer(t *testing.T, serverName, from string, to []string) (string, *x509.CertPool, <-chan next1010IDNAWire) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "IDNA private test peer"},
		DNSNames: []string{serverName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	result, finished := make(chan next1010IDNAWire, 1), make(chan struct{})
	go func() {
		defer close(finished)
		out := next1010IDNAWire{}
		out.err = func() error {
			raw, err := listener.Accept()
			if err != nil {
				return err
			}
			defer raw.Close()
			stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
			defer stop()
			if err := raw.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			var conn net.Conn = raw
			reader := textproto.NewReader(bufio.NewReader(conn))
			write := func(reply string) error { _, err := fmt.Fprint(conn, reply+"\r\n"); return err }
			command := func(want, reply string) error {
				line, err := reader.ReadLine()
				if err != nil || line != want {
					return fmt.Errorf("IDNA SMTP command=%q, want=%q: %v", line, want, err)
				}
				return write(reply)
			}
			if err := write("220 idna.test ESMTP"); err != nil {
				return err
			}
			if err := command("EHLO localhost", "250-idna.test\r\n250 STARTTLS"); err != nil {
				return err
			}
			if err := command("STARTTLS", "220 begin TLS"); err != nil {
				return err
			}
			secure := tls.Server(raw, serverTLS)
			if err := secure.HandshakeContext(ctx); err != nil {
				return err
			}
			out.sni = secure.ConnectionState().ServerName
			conn, reader = secure, textproto.NewReader(bufio.NewReader(secure))
			if err := command("EHLO localhost", "250-idna.test\r\n250-SMTPUTF8\r\n250 8BITMIME"); err != nil {
				return err
			}
			if err := command("MAIL FROM:<"+from+"> BODY=8BITMIME SMTPUTF8", "250 sender accepted"); err != nil {
				return err
			}
			for _, recipient := range to {
				if err := command("RCPT TO:<"+recipient+">", "250 recipient accepted"); err != nil {
					return err
				}
			}
			if err := command("DATA", "354 continue"); err != nil {
				return err
			}
			lines, err := reader.ReadDotLines()
			if err != nil {
				return err
			}
			out.raw = []byte(strings.Join(lines, "\r\n") + "\r\n")
			if err := write("250 accepted"); err != nil {
				return err
			}
			return command("QUIT", "221 bye")
		}()
		result <- out
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("IDNA SMTP peer did not stop")
		}
	})
	return listener.Addr().String(), roots, result
}

// Submit, durable envelope serialization, MIME building, recipient completion,
// DNS packets, SMTPUTF8 and STARTTLS all use the shipping path. The store is the
// existing FakeStore; this does not claim PostgreSQL crash durability.
func TestNext1010IDNASubmissionTLSAndRecipientIdentity(t *testing.T) {
	for _, route := range []string{"explicit-mx", "implicit-mx"} {
		for role, roleName := range []string{"to", "cc", "bcc"} {
			t.Run(route+"/"+roleName, func(t *testing.T) {
				const input = `"Team @ Desk"@例子.test`
				const envelope = `"team @ desk"@例子.test`
				const identity = "team @ desk@例子.test"
				st := testutil.NewFakeStore()
				req := quotaTestSendRequest(uuid.New(), uuid.New())
				seedQuotaSender(t, st, &req)
				req.TextBody = "hello\r\n" // Keep complete MIME comparable after SMTP dot framing.
				req.To, req.CC, req.BCC = nil, nil, nil
				groups := []*[]string{&req.To, &req.CC, &req.BCC}
				*groups[role] = []string{input}
				svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, nopGovernance{}, zerolog.Nop())
				job, err := svc.Submit(t.Context(), req)
				if err != nil || job == nil || !reflect.DeepEqual(job.RcptTo, []string{envelope}) || (*groups[role])[0] != input {
					t.Fatalf("IDNA submission changed existing envelope semantics: job=%+v input=%q err=%v", job, *groups[role], err)
				}
				dns := &next1010IDNADNS{name: "xn--fsqu00a.test.", address: "a"}
				wantHost := "xn--fsqu00a.test"
				if route == "explicit-mx" {
					dns.mx, wantHost = "mx.xn--fsqu00a.test.", "mx.xn--fsqu00a.test"
				}
				rv := dns.resolver(t)
				sessions := 0
				var wire next1010IDNAWire
				svc.adapter = r5QuotedAdapter(func(ctx context.Context, current *models.OutboundJob, raw []byte) (*DeliveryResult, error) {
					err := deliverDirectWith(ctx, current.MailFrom, current.RcptTo, raw, true, smtpMXLookup(rv),
						func(ctx context.Context, host, destination, from string, to []string, raw []byte, required bool) error {
							sessions++
							if host != wantHost || destination != net.JoinHostPort(wantHost, "25") || !reflect.DeepEqual(to, []string{envelope}) || !required {
								t.Errorf("DNS/TLS route or envelope changed: host=%q destination=%q to=%q required=%t", host, destination, to, required)
							}
							addr, roots, done := next1010IDNAPeer(t, wantHost, from, []string{envelope})
							err := deliverDirectMXTLS(ctx, host, addr, from, to, raw, required, &tls.Config{ServerName: host, RootCAs: roots})
							select {
							case wire = <-done:
							case <-ctx.Done():
								return fmt.Errorf("IDNA SMTP observation: %w", ctx.Err())
							}
							if wire.err != nil || wire.sni != wantHost || !bytes.Equal(wire.raw, raw) {
								t.Errorf("real STARTTLS/SNIMIME changed: SNI=%q err=%v rawEqual=%t", wire.sni, wire.err, bytes.Equal(wire.raw, raw))
							}
							return err
						})
					result := &DeliveryResult{Adapter: "direct_mx"}
					if err == nil {
						result.SMTPCode = 250
					} else {
						result.Error = err.Error()
					}
					return result, err
				})
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				claimed, err := st.ClaimOutboundJobs(ctx, time.Now(), 1)
				if err != nil || len(claimed) != 1 {
					t.Fatalf("claim actual IDNA job: len=%d err=%v", len(claimed), err)
				}
				work := &workqueue.Job[*outboundJob]{ID: job.ID, Attempts: claimed[0].Attempts,
					Payload: &outboundJob{OutboundJob: claimed[0]}, Lease: workqueue.Lease{Token: claimed[0].DeliveryToken}}
				if err := svc.processOne(ctx, work); err != nil || sessions != 1 {
					t.Errorf("queued IDNA delivery: SMTP sessions=%d err=%v, want one accepted transaction", sessions, err)
				}
				dns.verify(t, route == "implicit-mx")
				rows, err := st.ListOutboundRecipients(ctx, job.TenantID, job.ID)
				if err != nil || len(rows) != 1 || rows[0].Address != envelope || rows[0].State != delivery.Accepted || rows[0].Attempts != 1 {
					t.Errorf("IDNA recipient completion changed identity/result: rows=%+v err=%v", rows, err)
				}
				stored, err := st.GetOutboundJob(ctx, job.ID)
				if err != nil || stored == nil || stored.State != models.OutboundSent || !reflect.DeepEqual(stored.RcptTo, []string{envelope}) {
					t.Errorf("IDNA queued identity or sent state lost: job=%+v err=%v", stored, err)
				}
				if sessions == 0 {
					return // Baseline failure already records the absent DNS/TLS effects.
				}
				message, err := mail.ReadMessage(bytes.NewReader(wire.raw))
				if err != nil {
					t.Fatal(err)
				}
				if message.Header.Get("Bcc") != "" {
					t.Error("IDNA BCC leaked to MIME")
				}
				if role < 2 {
					addresses, err := message.Header.AddressList([]string{"To", "Cc"}[role])
					if err != nil || len(addresses) != 1 || addresses[0].Address != identity {
						t.Errorf("IDNA rewrote visible MIME identity: addresses=%v err=%v", addresses, err)
					}
				} else if bytes.Contains(wire.raw, []byte("例子.test")) || bytes.Contains(wire.raw, []byte("xn--fsqu00a.test")) {
					t.Error("IDNA BCC domain leaked into raw MIME")
				}
			})
		}
	}
}
