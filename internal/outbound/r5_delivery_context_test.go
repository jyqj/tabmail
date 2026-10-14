package outbound

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// Each barrier is reached by a real TCP peer before cancellation. The peer does
// not send the awaited reply, so cancellation must interrupt production I/O.
type r5SMTPBarrier struct {
	listener net.Listener
	reached  chan struct{}
	closed   chan struct{}
	release  chan struct{}
}

func r5StartSMTPBarrier(t *testing.T, stage string) *r5SMTPBarrier {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &r5SMTPBarrier{l, make(chan struct{}), make(chan struct{}), make(chan struct{})}
	t.Cleanup(func() {
		close(b.release)
		_ = l.Close()
		select {
		case <-b.closed:
		case <-time.After(time.Second):
			t.Error("fixture did not stop")
		}
	})
	go func() {
		defer close(b.closed)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		// Fixture cleanup also interrupts an outstanding socket read.
		done := make(chan struct{})
		defer close(done)
		watch := func(c net.Conn) {
			go func() {
				select {
				case <-b.release:
					_ = c.Close()
				case <-done:
				}
			}()
		}
		watch(conn)

		if strings.HasPrefix(stage, "reconnect_") {
			r := bufio.NewReader(conn)
			_, _ = io.WriteString(conn, "220 local.test ESMTP\r\n")
			if _, err = r.ReadString('\n'); err != nil {
				return
			}
			_, _ = io.WriteString(conn, "250-local.test\r\n250 STARTTLS\r\n")
			if _, err = r.ReadString('\n'); err != nil {
				return
			}
			_, _ = io.WriteString(conn, "220 begin TLS\r\n")
			if _, err = r.ReadByte(); err != nil {
				return
			}
			_, _ = io.WriteString(conn, "invalid TLS record\r\n")
			_ = conn.Close()
			conn, err = l.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			watch(conn)
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			stage = strings.TrimPrefix(stage, "reconnect_")
		}
		r := bufio.NewReader(conn)
		stall := func() { close(b.reached); _, _ = io.Copy(io.Discard, r) }
		write := func(s string) { _, _ = io.WriteString(conn, s) }
		if stage == "implicit_tls" {
			_, err = r.ReadByte()
			if err == nil {
				stall()
			}
			return
		}
		if stage == "greeting" {
			stall()
			return
		}
		write("220 local.test ESMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				if stage == "starttls" {
					write("250-local.test\r\n250 STARTTLS\r\n")
				} else {
					write("250 local.test\r\n")
				}
			case strings.HasPrefix(line, "STARTTLS"):
				write("220 begin TLS\r\n")
				_, err = r.ReadByte()
				if err == nil {
					stall()
				}
				return
			case strings.HasPrefix(line, "MAIL FROM:"):
				if stage == "mail" {
					stall()
					return
				}
				write("250 OK\r\n")
			case strings.HasPrefix(line, "RCPT TO:"):
				if stage == "rcpt" {
					stall()
					return
				}
				write("250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				if stage == "data" {
					stall()
					return
				}
				write("354 send data\r\n")
				if stage == "write" {
					close(b.reached)
					<-b.release
					return
				}
				for {
					line, err = r.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				if stage == "final" {
					stall()
					return
				}
				write("250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				stall()
				return
			default:
				write("500 unexpected\r\n")
			}
		}
	}()
	return b
}

func r5RelayConfig(b *r5SMTPBarrier, stage string) config.Outbound {
	host, port, _ := net.SplitHostPort(b.listener.Addr().String())
	n, _ := strconv.Atoi(port)
	cfg := config.Outbound{RelayHost: host, RelayPort: n}
	if stage == "implicit_tls" {
		cfg.RelayTLS = "tls"
	}
	if stage == "starttls" {
		cfg.RelayTLS = "starttls"
	}
	return cfg
}

func TestR5DeliveryContextRelayCancelStages(t *testing.T) {
	for _, stage := range []string{"implicit_tls", "greeting", "starttls", "mail", "rcpt", "data", "write", "final", "quit"} {
		t.Run(stage, func(t *testing.T) {
			b := r5StartSMTPBarrier(t, stage)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mime := []byte("Subject: test\r\n\r\nhello\r\n")
			if stage == "write" {
				mime = []byte(strings.Repeat("large body line\r\n", 1<<20))
			}
			result := make(chan error, 1)
			adapter := NewRelayAdapter(r5RelayConfig(b, stage))
			go func() {
				_, err := adapter.Deliver(ctx, &models.OutboundJob{MailFrom: "from@example.test", RcptTo: []string{"to@example.test"}}, mime)
				result <- err
			}()
			select {
			case <-b.reached:
			case <-time.After(2 * time.Second):
				t.Fatal("production adapter did not reach barrier")
			}
			cancel()
			select {
			case err := <-result:
				if stage == "quit" {
					if err != nil {
						t.Fatalf("250 accepted must survive canceled QUIT: %v", err)
					}
				} else {
					if err == nil {
						t.Fatal("canceled delivery succeeded")
					}
					if stage == "final" {
						if !errors.Is(err, store.ErrOutboundUncertain) {
							t.Fatalf("lost final reply must stay uncertain: %v", err)
						}
					} else if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation cause missing: %v", err)
					}
				}
			case <-time.After(400 * time.Millisecond):
				t.Fatal("cancel did not interrupt SMTP stage")
			}
			if stage != "write" {
				select {
				case <-b.closed:
				case <-time.After(400 * time.Millisecond):
					t.Fatal("production connection not closed")
				}
			}
		})
	}
}

func TestR5DeliveryContextPreCanceledAdapters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := &models.OutboundJob{MailFrom: "from@example.test", RcptTo: []string{"to@example.test"}}
	for _, adapter := range []DeliveryAdapter{NewDirectAdapter(true), NewRelayAdapter(config.Outbound{RelayHost: "127.0.0.1", RelayPort: 1, RelayTLS: "tls"})} {
		_, err := adapter.Deliver(ctx, job, nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: %v", adapter.Name(), err)
		}
	}
}

func TestR5DeliveryContextDirectMXCancelStages(t *testing.T) {
	for _, stage := range []string{"greeting", "starttls", "mail", "rcpt", "data", "write", "final", "quit", "reconnect_greeting", "reconnect_rcpt", "reconnect_final", "reconnect_quit"} {
		t.Run(stage, func(t *testing.T) {
			b := r5StartSMTPBarrier(t, stage)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mime := []byte("Subject: test\r\n\r\nhello\r\n")
			if stage == "write" {
				mime = []byte(strings.Repeat("large body line\r\n", 1<<20))
			}
			result := make(chan error, 1)
			go func() {
				result <- deliverDirectMX(ctx, "127.0.0.1", b.listener.Addr().String(), "from@example.test", []string{"to@example.test"}, mime, stage == "starttls")
			}()
			select {
			case <-b.reached:
			case err := <-result:
				t.Fatalf("did not reach barrier: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("direct session did not reach barrier")
			}
			cancel()
			select {
			case err := <-result:
				if strings.HasSuffix(stage, "quit") {
					if err != nil {
						t.Fatalf("accepted DATA must survive canceled QUIT: %v", err)
					}
				} else if strings.HasSuffix(stage, "final") {
					if !errors.Is(err, store.ErrOutboundUncertain) {
						t.Fatalf("final reply uncertainty lost: %v", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatalf("context cancellation lost: %v", err)
				}
			case <-time.After(400 * time.Millisecond):
				t.Fatal("direct cancellation did not interrupt I/O")
			}
			if stage != "write" {
				select {
				case <-b.closed:
				case <-time.After(400 * time.Millisecond):
					t.Fatal("direct connection did not close")
				}
			}
		})
	}
}

func TestR5DeliveryContextRelayDeadline(t *testing.T) {
	for _, stage := range []string{"implicit_tls", "greeting", "rcpt", "final", "quit"} {
		t.Run(stage, func(t *testing.T) {
			b := r5StartSMTPBarrier(t, stage)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- DeliverRelay(ctx, r5RelayConfig(b, stage), "from@example.test", []string{"to@example.test"}, []byte("body\r\n"))
			}()
			select {
			case <-b.reached:
			case <-time.After(time.Second):
				t.Fatal("deadline test did not reach stage")
			}
			select {
			case err := <-done:
				if stage == "quit" {
					if err != nil {
						t.Fatal(err)
					}
				} else if stage == "final" {
					if !errors.Is(err, store.ErrOutboundUncertain) {
						t.Fatal(err)
					}
				} else if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline cause lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("deadline did not bound SMTP")
			}
		})
	}
}

func TestR5DeliveryContextReleaseIsIdempotent(t *testing.T) {
	b := r5StartSMTPBarrier(t, "greeting")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, release, err := dialSMTPContext(ctx, b.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn
	done := make(chan struct{})
	go func() { release(); release(); cancel(); release(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("release left a cancellation watcher blocked")
	}
}

func TestR5DeliveryContextDirectTLSRequiredNoDowngrade(t *testing.T) {
	b := r5StartSMTPBarrier(t, "quit")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := deliverDirectMX(ctx, "127.0.0.1", b.listener.Addr().String(), "from@example.test", []string{"to@example.test"}, []byte("body\r\n"), true)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS required but not supported") {
		t.Fatalf("TLS required policy changed: %v", err)
	}
	select {
	case <-b.closed:
	case <-time.After(time.Second):
		t.Fatal("rejected plaintext connection leaked")
	}
	select {
	case <-b.reached:
		t.Fatal("sent DATA despite required TLS")
	default:
	}
}

// The test CA is explicitly trusted only by the per-call private production
// helper. Public entry points retain platform trust and must reject this leaf.
func r5TrustedTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "R5 private test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafPub, leafKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, leafPub, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}}}, &tls.Config{ServerName: "127.0.0.1", RootCAs: roots}
}

func r5StartTrustedSMTP(t *testing.T, implicit, stallRCPT bool) (*r5SMTPBarrier, *tls.Config) {
	t.Helper()
	serverTLS, clientTLS := r5TrustedTLS(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &r5SMTPBarrier{l, make(chan struct{}), make(chan struct{}), make(chan struct{})}
	t.Cleanup(func() {
		close(b.release)
		_ = l.Close()
		select {
		case <-b.closed:
		case <-time.After(time.Second):
			t.Error("TLS fixture did not stop")
		}
	})
	go func() {
		defer close(b.closed)
		raw, err := l.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-b.release:
				_ = raw.Close()
			case <-done:
			}
		}()
		var conn net.Conn = raw
		encrypted := false
		if implicit {
			c := tls.Server(raw, serverTLS)
			if c.Handshake() != nil {
				return
			}
			conn = c
			encrypted = true
		}
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = io.WriteString(conn, s) }
		write("220 local.test ESMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				if encrypted {
					write("250 local.test\r\n")
				} else {
					write("250-local.test\r\n250 STARTTLS\r\n")
				}
			case strings.HasPrefix(line, "STARTTLS"):
				write("220 begin TLS\r\n")
				c := tls.Server(raw, serverTLS)
				if c.Handshake() != nil {
					return
				}
				conn = c
				r = bufio.NewReader(c)
				encrypted = true
			case strings.HasPrefix(line, "MAIL FROM:"):
				write("250 OK\r\n")
			case strings.HasPrefix(line, "RCPT TO:"):
				if !encrypted {
					return
				}
				if stallRCPT {
					close(b.reached)
					_, _ = io.Copy(io.Discard, r)
					return
				}
				write("250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				write("354 send data\r\n")
				for {
					line, err = r.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				write("250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				write("221 bye\r\n")
				return
			default:
				write("500 unexpected\r\n")
			}
		}
	}()
	return b, clientTLS
}

func TestR5DeliveryContextTrustedTLSSuccessAndCancel(t *testing.T) {
	for _, transport := range []string{"relay_tls", "relay_starttls", "direct_starttls"} {
		for _, cancelAtRCPT := range []bool{false, true} {
			name := transport + "/success"
			if cancelAtRCPT {
				name = transport + "/cancel_encrypted_rcpt"
			}
			t.Run(name, func(t *testing.T) {
				b, clientTLS := r5StartTrustedSMTP(t, transport == "relay_tls", cancelAtRCPT)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan error, 1)
				go func() {
					if transport == "direct_starttls" {
						result <- deliverDirectMXTLS(ctx, "127.0.0.1", b.listener.Addr().String(), "from@example.test", []string{"to@example.test"}, []byte("body\r\n"), true, clientTLS)
					} else {
						cfg := r5RelayConfig(b, "starttls")
						if transport == "relay_tls" {
							cfg.RelayTLS = "tls"
						}
						result <- deliverRelayTLS(ctx, cfg, "from@example.test", []string{"to@example.test"}, []byte("body\r\n"), clientTLS)
					}
				}()
				if cancelAtRCPT {
					select {
					case <-b.reached:
					case err := <-result:
						t.Fatalf("TLS did not reach encrypted RCPT: %v", err)
					case <-time.After(time.Second):
						t.Fatal("TLS barrier missing")
					}
					cancel()
				}
				select {
				case err := <-result:
					if cancelAtRCPT {
						if !errors.Is(err, context.Canceled) {
							t.Fatal(err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("TLS transport did not terminate")
				}
				select {
				case <-b.closed:
				case <-time.After(time.Second):
					t.Fatal("TLS connection leaked")
				}
			})
		}
	}
}

func TestR5DeliveryContextPublicTLSRejectsPrivateCA(t *testing.T) {
	b, _ := r5StartTrustedSMTP(t, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := DeliverRelay(ctx, r5RelayConfig(b, "implicit_tls"), "from@example.test", []string{"to@example.test"}, []byte("body\r\n"))
	if err == nil {
		t.Fatal("public TLS silently trusted private CA")
	}
	var verify *tls.CertificateVerificationError
	if !errors.As(err, &verify) {
		t.Fatalf("expected certificate verification failure: %v", err)
	}
}

func TestR5DeliveryContextActualDialControlCancel(t *testing.T) {
	b := r5StartSMTPBarrier(t, "greeting")
	reached := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &net.Dialer{ControlContext: func(ctx context.Context, network, address string, c syscall.RawConn) error {
		close(reached)
		<-ctx.Done()
		return ctx.Err()
	}}
	result := make(chan error, 1)
	go func() {
		conn, release, err := dialSMTPWithConnector(ctx, b.listener.Addr().String(), dialer.DialContext)
		if conn != nil {
			release()
		}
		result <- err
	}()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("actual TCP dial did not reach socket-control barrier")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("actual DialContext did not cancel")
	}
	// No accepted socket exists: cleanup closes the fixture listener.
}

func TestR5DeliveryContextOwnedDialSocketCancel(t *testing.T) {
	b := r5StartSMTPBarrier(t, "greeting")
	reached := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connector := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		close(reached)
		<-ctx.Done()
		return conn, nil
	}
	done := make(chan error, 1)
	go func() {
		conn, release, err := dialSMTPWithConnector(ctx, b.listener.Addr().String(), connector)
		if conn != nil {
			release()
			release()
		}
		done <- err
	}()
	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("real dial did not create owned socket")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("post-dial cancellation cause missing: %v", err)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("owned socket cleanup did not terminate")
	}
	select {
	case <-b.closed:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("owned socket not closed after cancellation")
	}
}
