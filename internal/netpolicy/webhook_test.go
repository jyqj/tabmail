package netpolicy

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestNormalizeWebhookURL(t *testing.T) {
	got, err := NormalizeWebhookURL(" HTTPS://Hooks.Example.COM.:443/path?q=1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://hooks.example.com:443/path?q=1" {
		t.Fatalf("unexpected canonical URL: %q", got)
	}

	for _, raw := range []string{
		"",
		"http://example.com/hook",
		"https://user:pass@example.com/hook",
		"https://example.com/hook#fragment",
		"https://localhost/hook",
		"https://127.0.0.1/hook",
		"https://10.0.0.1/hook",
		"https://100.64.0.1/hook",
		"https://169.254.169.254/latest/meta-data",
		"https://192.0.2.1/hook",
		"https://198.18.0.1/hook",
		"https://[::1]/hook",
		"https://[fc00::1]/hook",
		"https://[2001:db8::1]/hook",
	} {
		if _, err := NormalizeWebhookURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestBlockedWebhookAddressPolicy(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1",
		"169.254.1.1", "172.16.0.1", "192.168.1.1", "198.18.0.1",
		"224.0.0.1", "240.0.0.1", "::", "::1", "fc00::1", "fe80::1",
		"ff02::1", "64:ff9b::1", "2001::1", "2001:db8::1", "2002::1", "fec0::1",
	} {
		if !IsBlockedWebhookAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("expected %s to be blocked", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if IsBlockedWebhookAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("expected %s to be allowed", raw)
		}
	}
}

func TestPinnedWebhookClientRejectsPrivateDestinations(t *testing.T) {
	for _, rawURL := range []string{
		"http://example.com/hook",
		"https://127.0.0.1/hook",
		"https://[::1]/hook",
		"https://10.0.0.1/hook",
		"https://100.64.0.1/hook",
		"https://169.254.169.254/latest/meta-data",
	} {
		if _, err := NewPinnedWebhookClient(context.Background(), rawURL, time.Second); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}
