package smtp

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
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

func TestR5SMTPOriginRootDotGlobSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, sender, pattern, code string
	}{
		{"escaped_dot_rooted", "sender@blocked.example.", `blocked.example\.`, "550"},
		{"escaped_dot_plain", "sender@blocked.example", `blocked.example\.`, "550"},
		{"class_dot_rooted", "sender@blocked.example.", `blocked.example[.]`, "550"},
		{"class_dot_plain", "sender@blocked.example", `blocked.example[.]`, "550"},
		{"wildcard_escaped_dot", "sender@sub.blocked.example.", `*.blocked.example\.`, "550"},
		{"question_matches_root_dot", "sender@blocked.example.", `blocked.example?`, "550"},
		{"ordinary_wildcard", "sender@sub.blocked.example.", `*.blocked.example`, "550"},
		{"wildcard_keeps_parent_boundary", "sender@blocked.example.", `*.blocked.example\.`, "250"},
		{"invalid_escape", "sender@blocked.example.", `blocked.example\`, "250"},
		{"invalid_class", "sender@blocked.example.", `blocked.example[`, "250"},
		{"literal_exact", "sender@[192.0.2.7]", `\[192.0.2.7\]`, "550"},
		{"literal_has_no_root_dot_alias", "sender@[192.0.2.7]", `\[192.0.2.7\]\.`, "250"},
		{"empty_label_precedes_policy", "sender@blocked.example..", `blocked.example\.`, "501"},
		{"null_reverse_path", "", `*`, "250"},
		{"quoted_local_at", `"sender@elsewhere.example"@blocked.example.`, `blocked.example\.`, "550"},
		{"existing_utf8_domain", "sender@例子.测试.", `例子.测试\.`, "550"},
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
			command := "MAIL FROM:<" + tc.sender + ">"
			if _, err := io.WriteString(conn, command+"\r\n"); err != nil {
				t.Fatal(err)
			}
			response, err := r.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("TCP C: %q; reject pattern: %q; S: %q", command, tc.pattern, response)
			if !strings.HasPrefix(response, tc.code+" ") {
				t.Fatalf("MAIL response %q, want %s", response, tc.code)
			}
		})
	}
}
