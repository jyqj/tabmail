package outbound

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"tabmail/internal/config"
	"testing"
	"time"
)

// Once DATA is accepted, a broken QUIT reply must not turn the delivery into
// a retry (and duplicate a business email at the recipient).
func TestRelayAcceptedDataIgnoresQuitDisconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := textproto.NewReader(bufio.NewReader(conn))
		fmt.Fprint(conn, "220 fixture SMTP\r\n")
		for {
			line, err := reader.ReadLine()
			if err != nil {
				done <- err
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				fmt.Fprint(conn, "250 OK\r\n")
			case line == "DATA":
				fmt.Fprint(conn, "354 send data\r\n")
				if _, err = reader.ReadDotBytes(); err != nil {
					done <- err
					return
				}
				fmt.Fprint(conn, "250 queued\r\n")
			case line == "QUIT":
				done <- nil
				return
			default:
				done <- fmt.Errorf("unexpected command %q", line)
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = DeliverRelay(ctx, config.Outbound{RelayHost: "127.0.0.1", RelayPort: ln.Addr().(*net.TCPAddr).Port, RelayTLS: "none"}, "sender@example.test", []string{"recipient@example.test"}, []byte("Subject: test\r\n\r\nBody\r\n"))
	if err != nil {
		t.Fatalf("accepted message should not be retried: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
