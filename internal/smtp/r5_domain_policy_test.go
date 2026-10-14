package smtp

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

func TestR5SMTPOriginPolicyRootDot(t *testing.T) {
	for _, tc := range []struct {
		name, sender, pattern, code string
	}{
		{"exact_domain", "sender@blocked.example.", "blocked.example", "550"},
		{"wildcard_domain", "sender@sub.blocked.example.", "*.blocked.example", "550"},
		{"root_dot_pattern", "sender@blocked.example", "blocked.example.", "550"},
		{"unrelated_domain", "sender@allowed.example.", "blocked.example", "250"},
		{"exact_does_not_expand", "sender@sub.blocked.example.", "blocked.example", "250"},
		{"null_reverse_path", "", "*", "250"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			rv := resolver.New(st, policy.NamingFull, true)
			pol := models.SMTPPolicy{DefaultAccept: true, DefaultStore: true, RejectOriginDomains: []string{tc.pattern}}
			ingestSvc := ingest.NewService(st, testutil.NewMemoryObjectStore(), rv, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), pol, 24, nil, config.Ingest{}, zerolog.Nop())
			s := NewServer(config.SMTP{Domain: "mx.example.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: 1024}, ingestSvc, rv, zerolog.Nop())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			started := r5Start(s, context.Background())
			r5Await(t, s.ready)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.Shutdown(ctx); err != nil {
					t.Errorf("SMTP join: %v", err)
				}
				if err := r5Result(t, started); err != nil {
					t.Errorf("SMTP serve: %v", err)
				}
			})
			conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			r := bufio.NewReader(conn)
			r5CloudWire(t, conn, r, "", "220")
			r5CloudWire(t, conn, r, "EHLO client.example.test", "250")
			r5CloudWire(t, conn, r, "MAIL FROM:<"+tc.sender+">", tc.code)
		})
	}
}
