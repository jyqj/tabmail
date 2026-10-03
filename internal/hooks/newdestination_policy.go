package hooks

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The policy, resolver and dialer belong to one dispatcher/transport lifetime.
// Dependencies are private seams for synthetic tests, not runtime bypass flags.
type destinationPolicy struct {
	allowed []netip.Prefix
	timeout time.Duration
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
}

type destinationPolicyError struct {
	reason string
	cause  error // Only context cancellation/deadline sentinels, never raw input.
}

func (e *destinationPolicyError) Error() string { return "webhook destination policy: " + e.reason }
func (e *destinationPolicyError) Unwrap() error { return e.cause }

func destinationDenied(reason string) error { return &destinationPolicyError{reason: reason} }

// This conservative public-address definition excludes all special-purpose
// allocations, including globally reachable protocol anycast. Operators can
// authorize a controlled unicast receiver explicitly. Snapshot: 2026-10-03.
// Sources (not fetched during delivery):
// https://www.iana.org/assignments/iana-ipv4-special-registry
// https://www.iana.org/assignments/iana-ipv6-special-registry
// IPv4-mapped IPv6 is unmapped before checking; translation/tunnel ranges are
// not treated as ordinary public addresses or proof of the final IPv4 peer.
var webhookSpecialPrefixes = func() []netip.Prefix {
	blocks := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24",
		"192.52.193.0/24", "192.88.99.0/24", "192.168.0.0/16", "192.175.48.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64",
		"2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48",
		"3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10",
	}
	result := make([]netip.Prefix, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, netip.MustParsePrefix(block))
	}
	return result
}()

func newDestinationPolicy(cidrs string, timeout time.Duration) (*destinationPolicy, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	p := &destinationPolicy{
		timeout: timeout,
		lookup:  net.DefaultResolver.LookupNetIP,
		dial:    (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
	}
	if strings.TrimSpace(cidrs) == "" {
		return p, nil
	}
	for _, item := range strings.Split(cidrs, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err != nil || prefix.Addr().Zone() != "" || prefix != prefix.Masked() {
			return nil, destinationDenied("invalid_cidr")
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, destinationDenied("invalid_cidr")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		p.allowed = append(p.allowed, prefix)
	}
	return p, nil
}

func newDestinationTransport(p *destinationPolicy, policyErr error) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Deliberate deployment contract: webhook egress is direct. Validating a
	// proxy socket cannot validate the destination resolved by that proxy.
	transport.Proxy = nil
	transport.Dial = nil
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if policyErr != nil {
			return nil, policyErr
		}
		if p == nil {
			return nil, destinationDenied("invalid_policy")
		}
		return p.dialContext(ctx, network, address)
	}
	return transport
}

func (p *destinationPolicy) permits(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() || ip == netip.MustParseAddr("255.255.255.255") {
		return false
	}
	for _, prefix := range p.allowed {
		if prefix.Contains(ip) {
			return true
		}
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	// IsGlobalUnicast also accepts unallocated/reserved IPv6 space.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range webhookSpecialPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func (p *destinationPolicy) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return destinationDenied("invalid_url")
	}
	host := u.Hostname()
	if strings.Contains(host, "%") {
		return destinationDenied("invalid_host")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return destinationDenied("invalid_port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return destinationDenied("invalid_port")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !p.permits(ip) {
		return destinationDenied("address_not_allowed")
	}
	return nil
}

func (p *destinationPolicy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, destinationDenied("invalid_network")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || strings.Contains(host, "%") {
		return nil, destinationDenied("invalid_host")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, destinationDenied("invalid_port")
	}
	port = strconv.Itoa(n)
	// Transport can keep a speculative dial alive independently of the request;
	// bound lookup plus all candidate dials ourselves, not once per candidate.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	var addresses []netip.Addr
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = p.lookup(ctx, "ip", host)
		if err != nil {
			denied := &destinationPolicyError{reason: "dns_failed"}
			if errors.Is(err, context.Canceled) {
				denied.cause = context.Canceled
			} else if errors.Is(err, context.DeadlineExceeded) {
				denied.cause = context.DeadlineExceeded
			}
			return nil, denied
		}
	}
	if len(addresses) == 0 {
		return nil, destinationDenied("dns_empty")
	}
	// Check the complete A/AAAA answer before opening ANY socket, including
	// answers incompatible with a requested tcp4/tcp6 dial.
	seen := make(map[netip.Addr]bool, len(addresses))
	approved := make([]netip.Addr, 0, len(addresses))
	for _, ip := range addresses {
		if !p.permits(ip) {
			return nil, destinationDenied("address_not_allowed")
		}
		ip = ip.Unmap()
		if !seen[ip] {
			seen[ip] = true
			approved = append(approved, ip)
		}
	}
	var lastErr error
	for _, ip := range approved {
		if (network == "tcp4" && !ip.Is4()) || (network == "tcp6" && !ip.Is6()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := p.dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			lastErr = err
			continue
		}
		if conn == nil {
			return nil, destinationDenied("invalid_peer")
		}
		if err := ctx.Err(); err != nil {
			_ = conn.Close()
			return nil, err
		}
		remote := conn.RemoteAddr()
		if remote == nil {
			_ = conn.Close()
			return nil, destinationDenied("invalid_peer")
		}
		peerHost, peerPort, err := net.SplitHostPort(remote.String())
		peer, parseErr := netip.ParseAddr(peerHost)
		if err != nil || parseErr != nil || peerPort != port || !p.permits(peer) || peer.Unmap() != ip {
			_ = conn.Close()
			return nil, destinationDenied("peer_not_allowed")
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, destinationDenied("dns_no_compatible_address")
}

func redactWebhookURLError(err error) error {
	var wrapped *url.Error
	if errors.As(err, &wrapped) {
		// Preserve context/TLS error identity and timeout methods, but do not
		// persist a raw request URL through retry/dead-letter LastError.
		return &url.Error{Op: wrapped.Op, URL: "[redacted]", Err: wrapped.Err}
	}
	return err
}
