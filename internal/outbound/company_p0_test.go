package outbound

import (
	"bufio"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestP0DataAcceptedQuitDisconnectedIsSuccess(t *testing.T) {
	clientConn, server := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = server.Close() })
	_ = clientConn.SetDeadline(time.Now().Add(5 * time.Second))
	_ = server.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		w := bufio.NewWriter(server)
		r := bufio.NewReader(server)
		write := func(s string) { _, _ = fmt.Fprint(w, s); _ = w.Flush() }
		write("220 local.test ESMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				write("250 local.test\r\n")
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				write("250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				write("354 send data\r\n")
				for {
					l, e := r.ReadString('\n')
					if e != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
				}
				write("250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				return
			default:
				write("500 unexpected\r\n")
			}
		}
	}()
	c, err := smtp.NewClient(clientConn, "local.test")
	if err != nil {
		t.Fatal(err)
	}
	err = sendSMTP(c, "from@example.test", []string{"to@example.test"}, []byte("Subject: test\r\n\r\nhello\r\n"))
	<-done
	if err != nil {
		t.Fatalf("DATA already accepted; QUIT disconnect must not trigger retry: %v", err)
	}
}
