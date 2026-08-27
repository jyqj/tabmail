// Package netpolicy centralizes network destination policy for outbound
// integrations. Keeping URL normalization and connect-time DNS validation in
// one package prevents create-time and delivery-time SSRF rules from drifting.
package netpolicy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
)

var blockedWebhookPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// NormalizeWebhookURL validates and canonicalizes a tenant-managed webhook
// URL. Domain names are re-resolved and checked again immediately before every
// connection; this function also rejects unsafe literal destinations early.
func NormalizeWebhookURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if len(raw) > 2048 {
		return "", errors.New("url is too long")
	}
	if strings.ContainsFunc(raw, unicode.IsControl) {
		return "", errors.New("url contains invalid control characters")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("webhook url must use https")
	}
	if parsed.User != nil {
		return "", errors.New("webhook url must not contain credentials")
	}
	if parsed.Fragment != "" {
		return "", errors.New("webhook url must not contain a fragment")
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", errors.New("webhook url host is required")
	}
	if strings.Contains(host, "%") {
		return "", errors.New("webhook url host is not allowed")
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", errors.New("webhook url host is not allowed")
	}
	if addr, err := netip.ParseAddr(host); err == nil && IsBlockedWebhookAddress(addr) {
		return "", errors.New("webhook url host is not allowed")
	}
	if ipLikeHostname(host) {
		return "", errors.New("webhook url host is not allowed")
	}

	port := parsed.Port()
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	parsed.Scheme = "https"
	return parsed.String(), nil
}

// IsBlockedWebhookAddress rejects destinations that are private, local,
// special-purpose, documentation-only, benchmark, multicast, or otherwise not
// suitable as an Internet webhook target.
func IsBlockedWebhookAddress(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range blockedWebhookPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ResolvePublicWebhookAddresses resolves a host once and fails closed when any
// answer is unsafe. Rejecting a mixed public/private answer prevents an
// attacker-controlled DNS server from steering retries into an internal
// address after a benign answer has been observed.
func ResolvePublicWebhookAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if direct, err := netip.ParseAddr(host); err == nil {
		direct = direct.Unmap()
		if IsBlockedWebhookAddress(direct) {
			return nil, errors.New("webhook destination resolves to a non-public address")
		}
		return []netip.Addr{direct}, nil
	}

	resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(resolved) == 0 {
		if err == nil {
			err = errors.New("no addresses returned")
		}
		return nil, fmt.Errorf("resolve webhook host: %w", err)
	}
	out := make([]netip.Addr, 0, len(resolved))
	for _, addr := range resolved {
		addr = addr.Unmap()
		if IsBlockedWebhookAddress(addr) {
			return nil, errors.New("webhook destination resolves to a non-public address")
		}
		out = append(out, addr)
	}
	return out, nil
}

// NewPinnedWebhookClient resolves and validates a tenant-managed endpoint, then
// dials only the validated address set. Redirects are disabled and TLS still
// verifies the original hostname. This closes DNS-rebinding and redirect-based
// SSRF gaps between endpoint creation and delivery.
func NewPinnedWebhookClient(ctx context.Context, rawURL string, timeout time.Duration) (*http.Client, error) {
	normalized, err := NormalizeWebhookURL(rawURL)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, errors.New("invalid webhook url")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addresses, err := ResolvePublicWebhookAddresses(resolveCtx, host)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var lastErr error
		for _, addr := range addresses {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = errors.New("no validated webhook destination")
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func ipLikeHostname(host string) bool {
	return strings.Count(host, ".") == 3 && strings.IndexFunc(host, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}) == -1
}
