package testutil_test

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"strconv"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
	"testing"
	"time"
)

func TestR5SMTPRealRelayPhasesAndCleanup(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		behavior  testutil.R5SMTPBehavior
		code      int
		uncertain bool
	}{
		{name: "accepted"},
		{name: "rcpt_temporary", behavior: testutil.R5SMTPBehavior{RCPTCode: 451}, code: 451},
		{name: "rcpt_permanent", behavior: testutil.R5SMTPBehavior{RCPTCode: 550}, code: 550},
		{name: "data_rejected", behavior: testutil.R5SMTPBehavior{DataCode: 554}, code: 554},
		{name: "final_reply_lost", behavior: testutil.R5SMTPBehavior{DropFinal: true}, uncertain: true},
		{name: "accepted_quit_lost", behavior: testutil.R5SMTPBehavior{DropQUIT: true}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			server, err := testutil.StartR5SMTP(ctx, scenario.behavior)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			select {
			case <-server.Ready():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			err = outbound.DeliverRelay(ctx, server.Config(), "sender@fixture.test", []string{"recipient@fixture.test"}, []byte("Subject: fixture\r\n\r\nSynthetic body\r\n"))
			if scenario.uncertain {
				if !errors.Is(err, store.ErrOutboundUncertain) {
					t.Fatalf("actual DATA boundary did not retain uncertainty: %v", err)
				}
			} else if scenario.code != 0 {
				var smtpError *textproto.Error
				if !errors.As(err, &smtpError) || smtpError.Code != scenario.code {
					t.Fatalf("actual SMTP reply mismatch: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			events := server.Events()
			if len(events) < 4 {
				t.Fatal("real SMTP transport stages were not observed")
			}
			if err = server.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-server.Closed():
			case <-ctx.Done():
				t.Fatal("SMTP owned cleanup did not terminate")
			}
			cfg := server.Config()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(cfg.RelayHost, fmtPort(cfg.RelayPort)), time.Second)
			if err == nil {
				conn.Close()
				t.Fatal("closed SMTP listener still accepted a connection")
			}
		})
	}
}
func fmtPort(n int) string { return strconv.Itoa(n) }
func TestR5SMTPRequiresBoundedOwnership(t *testing.T) {
	if _, err := testutil.StartR5SMTP(context.Background(), testutil.R5SMTPBehavior{}); err == nil {
		t.Fatal("unbounded server accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	cancel()
	if _, err := testutil.StartR5SMTP(ctx, testutil.R5SMTPBehavior{}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled ownership accepted")
	}
}
