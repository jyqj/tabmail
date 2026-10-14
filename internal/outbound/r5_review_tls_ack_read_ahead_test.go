package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"tabmail/internal/config"
)

// Buffer the final application record and close_notify so a real TCP read can
// contain the complete authenticated SMTP reply and the first alert byte.
type r5ReviewTLSAckWire struct {
	net.Conn
	sent    int
	hold    bool
	pending bytes.Buffer
}

func (c *r5ReviewTLSAckWire) Write(p []byte) (int, error) {
	if c.hold {
		n, err := c.pending.Write(p)
		c.sent += n
		return n, err
	}
	n, err := c.Conn.Write(p)
	c.sent += n
	return n, err
}

// This is a compatibility control for an independent review, not an added
// product requirement: a complete final 250 has already confirmed acceptance.
func TestR5ReviewSMTPCompleteTLSAckSurvivesAlertReadAhead(t *testing.T) {
	const wireLimit = 1 << 20
	serverTLS, trust := r5TrustedTLS(t)
	serverTLS.MinVersion, serverTLS.MaxVersion = tls.VersionTLS12, tls.VersionTLS12
	trust.MinVersion, trust.MaxVersion = tls.VersionTLS12, tls.VersionTLS12
	serverTLS.DynamicRecordSizingDisabled = true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type observation struct {
		bodyComplete   bool
		finalRecordEnd int
		alertBytes     int
		err            error
	}
	done := make(chan observation, 1)
	go func() {
		out := observation{}
		out.err = func() error {
			raw, err := ln.Accept()
			if err != nil {
				return err
			}
			defer raw.Close()
			if err := raw.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			wire := &r5ReviewTLSAckWire{Conn: raw}
			secure := tls.Server(wire, serverTLS)
			if err := secure.HandshakeContext(ctx); err != nil {
				return err
			}
			reader := bufio.NewReader(secure)
			if _, err := io.WriteString(secure, "220 fixture\r\n"); err != nil {
				return err
			}
			for _, exchange := range []struct{ command, reply string }{
				{"EHLO localhost\r\n", "250 fixture\r\n"},
				{"MAIL FROM:<sender@example.test>\r\n", "250 sender\r\n"},
				{"RCPT TO:<reader@recipient.test>\r\n", "250 recipient\r\n"},
				{"DATA\r\n", "354 follows\r\n"},
			} {
				line, err := reader.ReadString('\n')
				if err != nil || line != exchange.command {
					return fmt.Errorf("SMTP command=%q want=%q error=%v", line, exchange.command, err)
				}
				if _, err := io.WriteString(secure, exchange.reply); err != nil {
					return err
				}
			}
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				if line == ".\r\n" {
					out.bodyComplete = true
					break
				}
			}
			padding := "250-" + strings.Repeat("x", 250) + "\r\n"
			before := wire.sent
			if _, err := io.WriteString(secure, padding); err != nil {
				return err
			}
			overhead := wire.sent - before - len(padding)
			if overhead < 1 || overhead > 128 {
				return fmt.Errorf("unexpected real TLS record overhead %d", overhead)
			}
			for wire.sent+overhead+len(padding) <= wireLimit-128 {
				if _, err := io.WriteString(secure, padding); err != nil {
					return err
				}
			}
			finalLen := wireLimit - 1 - wire.sent - overhead
			if finalLen < 6 || finalLen > 512 {
				return fmt.Errorf("invalid final record length %d", finalLen)
			}
			wire.hold = true
			if _, err := io.WriteString(secure, "250 "+strings.Repeat("x", finalLen-6)+"\r\n"); err != nil {
				return err
			}
			out.finalRecordEnd = wire.sent
			if err := secure.CloseWrite(); err != nil {
				return err
			}
			out.alertBytes = wire.sent - out.finalRecordEnd
			if out.finalRecordEnd != wireLimit-1 || out.alertBytes < 5 {
				return fmt.Errorf("incorrect authenticated ACK/alert placement: %+v", out)
			}
			if err := raw.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			if _, err := raw.Write(wire.pending.Bytes()); err != nil {
				return err
			}
			_, _ = io.Copy(io.Discard, reader)
			return nil
		}()
		done <- out
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	err = deliverRelayTLS(ctx, config.Outbound{RelayHost: host, RelayPort: portNumber, RelayTLS: "tls"}, "sender@example.test", []string{"reader@recipient.test"}, []byte("body\r\n"), trust)
	if err != nil {
		t.Errorf("complete authenticated final 250 became failed delivery while reading the following TLS alert: %v", err)
	}
	select {
	case out := <-done:
		if out.err != nil || !out.bodyComplete || out.finalRecordEnd != wireLimit-1 {
			t.Errorf("real TLS fixture did not reach complete DATA acknowledgement: %+v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("real TLS fixture did not join")
	}
}
