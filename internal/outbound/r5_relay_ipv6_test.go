package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/models"
)

// Exercise the public relay adapter with actual numeric loopback hosts. The
// server records the SMTP envelope/body and signals the exact RCPT read before
// a cancellation, so parsing an invalid dial address cannot satisfy the oracle.
func r5RelayAddressPeer(t *testing.T, host string, stallRCPT bool) (config.Outbound, <-chan struct{}, <-chan error) {
	t.Helper()
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	done := make(chan error, 1)
	stop := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		_ = l.Close()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("relay fixture did not stop")
		}
	})
	go func() {
		defer close(finished)
		done <- func() error {
			conn, err := l.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				return err
			}
			closed := make(chan struct{})
			defer close(closed)
			go func() {
				select {
				case <-stop:
					_ = conn.Close()
				case <-closed:
				}
			}()
			r := bufio.NewReader(conn)
			write := func(s string) error { _, err := io.WriteString(conn, s); return err }
			if err := write("220 local.test ESMTP\r\n"); err != nil {
				return err
			}
			for _, command := range []struct{ line, reply string }{
				{"EHLO localhost\r\n", "250 local.test\r\n"},
				{"MAIL FROM:<from@example.test>\r\n", "250 sender OK\r\n"},
				{"RCPT TO:<to@example.test>\r\n", "250 recipient OK\r\n"},
				{"DATA\r\n", "354 send body\r\n"},
			} {
				line, err := r.ReadString('\n')
				if err != nil {
					return err
				}
				if line != command.line {
					return fmt.Errorf("command = %q, want %q", line, command.line)
				}
				if strings.HasPrefix(line, "RCPT ") && stallRCPT {
					close(reached)
					_, err := r.ReadByte()
					if !errors.Is(err, io.EOF) {
						return fmt.Errorf("canceled peer did not close: %w", err)
					}
					return nil
				}
				if err := write(command.reply); err != nil {
					return err
				}
			}
			var body []string
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return err
				}
				if line == ".\r\n" {
					break
				}
				body = append(body, line)
			}
			if !reflect.DeepEqual(body, []string{"Subject: address boundary\r\n", "\r\n", "body\r\n"}) {
				return fmt.Errorf("body changed: %q", body)
			}
			if err := write("250 accepted\r\n"); err != nil {
				return err
			}
			line, err := r.ReadString('\n')
			if err != nil || line != "QUIT\r\n" {
				return fmt.Errorf("QUIT = %q: %w", line, err)
			}
			return write("221 bye\r\n")
		}()
	}()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return config.Outbound{RelayHost: host, RelayPort: n, RelayTLS: "none"}, reached, done
}

func TestR5RelayNumericHostConnectAndCancel(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		for _, stallRCPT := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%v", host, stallRCPT), func(t *testing.T) {
				cfg, reached, peerDone := r5RelayAddressPeer(t, host, stallRCPT)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					result, err := NewRelayAdapter(cfg).Deliver(ctx, &models.OutboundJob{
						MailFrom: "from@example.test", RcptTo: []string{"to@example.test"},
					}, []byte("Subject: address boundary\r\n\r\nbody\r\n"))
					if err == nil && (result.SMTPCode != 250 || result.RemoteHost != host) {
						err = fmt.Errorf("accepted relay result changed: %+v", result)
					}
					done <- err
				}()
				if stallRCPT {
					select {
					case <-reached:
						cancel()
					case err := <-done:
						t.Fatalf("relay failed before actual RCPT: %v", err)
					case <-time.After(time.Second):
						t.Fatal("relay did not reach RCPT")
					}
				}
				select {
				case err := <-done:
					if stallRCPT {
						if !errors.Is(err, context.Canceled) {
							t.Fatalf("RCPT cancellation cause lost: %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("relay did not finish")
				}
				select {
				case err := <-peerDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("relay did not close its connection")
				}
			})
		}
	}
}
