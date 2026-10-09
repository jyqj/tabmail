package config_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
)

const r5WebhookCIDRsChildKey = "TABMAIL_R5_WEBHOOK_CIDRS_LOAD_CHILD"
const r5WebhookCIDRsCaseKey = "TABMAIL_R5_WEBHOOK_CIDRS_LOAD_CASE"

// Run the real Load entrypoint in this test binary's child process. The child
// gets an explicit, minimal environment, not os.Environ, inherited TABMAIL
// settings, a production .env file, or production credentials. No TestMain or
// default-skip helper is needed; the same selected test performs child checks.
func TestR5WebhookCanonicalCIDRsLoad(t *testing.T) {
	if os.Getenv(r5WebhookCIDRsChildKey) == "1" {
		r5AssertWebhookCIDRsLoadChild(t, os.Getenv(r5WebhookCIDRsCaseKey))
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		canonical *string
	}{
		{name: "absent"},
		{name: "empty", canonical: r5WebhookCIDRsValue("")},
		{name: "canonical_wins", canonical: r5WebhookCIDRsValue("127.0.0.1/32,::1/128")},
		{name: "invalid_canonical_not_rescued", canonical: r5WebhookCIDRsValue("invalid-canonical-cidr")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestR5WebhookCanonicalCIDRsLoad$", "-test.count=1", "-test.timeout=4s", "-test.parallel=1")
			cmd.Env = []string{
				"GOMAXPROCS=2",
				r5WebhookCIDRsChildKey + "=1",
				r5WebhookCIDRsCaseKey + "=" + test.name,
				"TABMAIL_ROLE=worker",
				"TABMAIL_OBJECT_STORE=fs",
				"TABMAIL_DATADIR=/synthetic/webhook-config-only",
				"TABMAIL_MAILBOX_TOKEN_SECRET=synthetic-mailbox-secret-210319",
				"TABMAIL_JWT_SECRET=synthetic-distinct-jwt-secret-491027",
				"TABMAIL_DB_DSN=postgres://synthetic@127.0.0.1:1/synthetic?sslmode=disable",
				"TABMAIL_REDIS_ADDR=127.0.0.1:1",
				// A valid generic CIDR must neither authorize absence/empty nor
				// replace an invalid or different canonical deployment policy.
				"ALLOWED_CIDRS=10.0.0.0/8",
				"TABMAIL_WEBHOOK_ALLOWED_CID_RS=172.16.0.0/12",
			}
			if test.canonical != nil {
				cmd.Env = append(cmd.Env, "TABMAIL_WEBHOOK_ALLOWED_CIDRS="+*test.canonical)
			}
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("real Load child failed (case=%s deadline=%v): %v\n%s", test.name, ctx.Err(), err, output)
			}
		})
	}
}

func r5WebhookCIDRsValue(value string) *string { return &value }

func r5AssertWebhookCIDRsLoadChild(t *testing.T, scenario string) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("real Load rejected the explicit synthetic baseline: %v", err)
	}
	var expected string
	switch scenario {
	case "absent", "empty":
		expected = ""
	case "canonical_wins":
		expected = "127.0.0.1/32,::1/128"
	case "invalid_canonical_not_rescued":
		expected = "invalid-canonical-cidr"
	default:
		t.Fatalf("unknown controlled Load scenario: %q", scenario)
	}
	if cfg.Webhook.AllowedCIDRs != expected {
		t.Fatalf("real Load inherited/widened/replaced canonical policy: got %q want %q", cfg.Webhook.AllowedCIDRs, expected)
	}
	if scenario != "invalid_canonical_not_rescued" {
		return
	}
	// Exercise the existing hooks constructor's fail-closed result without any
	// socket: even a regression replacing the invalid CIDR with generic policy
	// cannot authorize the hard-denied unspecified URL used by this fixture.
	d := hooks.New(hooks.Config{
		URLs:         "http://0.0.0.0:1/synthetic-never-connected",
		AllowedCIDRs: cfg.Webhook.AllowedCIDRs,
		Timeout:      100 * time.Millisecond,
		MaxRetries:   1,
	}, zerolog.Nop())
	d.Publish(hooks.Event{Type: "synthetic.config.invalid"})
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if letters := d.DeadLetters(1); len(letters) == 1 {
			if letters[0].LastError != "webhook destination policy: invalid_cidr" {
				t.Fatalf("invalid canonical CIDR was rescued or lost fail-closed error: %s", letters[0].LastError)
			}
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("invalid canonical CIDR did not terminate the synthetic delivery")
		case <-ticker.C:
		}
	}
}
