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

// The real go-smtp parser accepts empty DNS labels. MAIL must reject them
// before an exact origin policy comparison, without reparsing the decoded
// local part as the more restricted mailbox naming syntax.
func TestR5SMTPSenderDomainLabels(t *testing.T) {
	for _, tc := range []struct {
		name, sender, pattern, option, code string
		enableUTF8                          bool
	}{
		{name: "double_root_dot_bypass", sender: "sender@blocked.example..", code: "501"},
		{name: "triple_root_dot_bypass", sender: "sender@blocked.example...", code: "501"},
		{name: "interior_empty_label", sender: "sender@blocked..example", code: "501"},
		{name: "leading_empty_label", sender: "sender@.blocked.example", code: "501"},
		{name: "root_only_domain", sender: "sender@.", code: "501"},
		{name: "all_empty_labels", sender: "sender@..", code: "501"},
		{name: "empty_domain_parser_control", sender: "sender@", code: "501"},
		{name: "valid_plain_domain", sender: "sender@allowed.example", code: "250"},
		{name: "valid_single_root_dot", sender: "sender@allowed.example.", code: "250"},
		{name: "single_root_dot_policy_control", sender: "sender@blocked.example.", code: "550"},
		{name: "null_reverse_path", pattern: "*", code: "250"},
		{name: "ipv4_literal", sender: "sender@[192.0.2.7]", code: "250"},
		{name: "ipv6_literal", sender: "sender@[IPv6:2001:db8::1]", code: "250"},
		{name: "ipv6_ipv4_literal", sender: "sender@[IPv6:::ffff:192.0.2.7]", code: "250"},
		{name: "quoted_local_space", sender: `"sender name"@allowed.example`, code: "250"},
		{name: "quoted_local_at", sender: `"sender@elsewhere.example"@allowed.example.`, code: "250"},
		{name: "quoted_local_repeated_dots", sender: `"sender..name"@allowed.example`, code: "250"},
		{name: "existing_utf8_local", sender: "发送者@allowed.example", code: "250"},
		{name: "existing_utf8_domain", sender: "sender@例子.测试", code: "250"},
		{name: "enabled_smtputf8", sender: "发送者@例子.测试.", option: " SMTPUTF8", enableUTF8: true, code: "250"},
		{name: "enabled_smtputf8_empty_label", sender: "发送者@例子..测试", option: " SMTPUTF8", enableUTF8: true, code: "501"},
		{name: "disabled_smtputf8_control", sender: "发送者@例子.测试", option: " SMTPUTF8", code: "504"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pattern := tc.pattern
			if pattern == "" {
				pattern = "blocked.example"
			}
			st := testutil.NewFakeStore()
			rv := resolver.New(st, policy.NamingFull, true)
			pol := models.SMTPPolicy{DefaultAccept: true, DefaultStore: true, RejectOriginDomains: []string{pattern}}
			ingestSvc := ingest.NewService(st, testutil.NewMemoryObjectStore(), rv, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), pol, 24, nil, config.Ingest{}, zerolog.Nop())
			s := NewServer(config.SMTP{Domain: "mx.example.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: 1024}, ingestSvc, rv, zerolog.Nop())
			// Production currently leaves this capability disabled. Exercise
			// the embedded server's existing opt-in without enabling it there.
			s.inner.EnableSMTPUTF8 = tc.enableUTF8
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
			command := "MAIL FROM:<" + tc.sender + ">" + tc.option
			if _, err := io.WriteString(conn, command+"\r\n"); err != nil {
				t.Fatal(err)
			}
			response, err := r.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("TCP C: %q; S: %q", command, response)
			if !strings.HasPrefix(response, tc.code+" ") {
				t.Fatalf("MAIL response %q, want %s", response, tc.code)
			}
		})
	}
}
