package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"reflect"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// This fixture records actual DNS question names. Only the fixed expected
// A-label has an answer; a resolver that receives the original Unicode name
// cannot pass by using a permissive injected LookupMX implementation.
type next1010IDNADNS struct {
	name, mx, address string
	mu                sync.Mutex
	questions         []dnsmessage.Question
}

func (d *next1010IDNADNS) resolver(t *testing.T) *net.Resolver {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, peer, err := conn.ReadFrom(buf)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("IDNA DNS read: %v", err)
				}
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buf[:n]); err != nil || len(query.Questions) != 1 {
				t.Errorf("invalid IDNA DNS question: %v", err)
				continue
			}
			q := query.Questions[0]
			d.mu.Lock()
			d.questions = append(d.questions, q)
			d.mu.Unlock()
			reply := dnsmessage.Message{
				Header: dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true,
					RecursionDesired: query.RecursionDesired, RecursionAvailable: true},
				Questions: query.Questions,
			}
			header := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}
			switch {
			case q.Name.String() != d.name || d.mx == "nxdomain":
				reply.RCode = dnsmessage.RCodeNameError
			case q.Type == dnsmessage.TypeMX && d.mx == "servfail":
				reply.RCode = dnsmessage.RCodeServerFailure
			case q.Type == dnsmessage.TypeMX && d.mx != "":
				reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.MXResource{MX: dnsmessage.MustNewName(d.mx)}}}
			case q.Type == dnsmessage.TypeA && d.address == "a":
				reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}}}
			case q.Type == dnsmessage.TypeAAAA && d.address == "aaaa":
				reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AAAAResource{AAAA: [16]byte{15: 1}}}}
			}
			packet, err := reply.Pack()
			if err != nil {
				t.Errorf("IDNA DNS reply: %v", err)
				continue
			}
			if _, err := conn.WriteTo(packet, peer); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("IDNA DNS write: %v", err)
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close(); <-done })
	return &net.Resolver{PreferGo: true, StrictErrors: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", conn.LocalAddr().String())
		},
	}
}

func (d *next1010IDNADNS) verify(t *testing.T, addressLookup bool) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	mx, ip := 0, 0
	for _, q := range d.questions {
		if q.Name.String() != d.name {
			t.Errorf("DNS question = %q, want fixed A-label %q", q.Name.String(), d.name)
		}
		if q.Type == dnsmessage.TypeMX {
			mx++
		} else if q.Type == dnsmessage.TypeA || q.Type == dnsmessage.TypeAAAA {
			ip++
		}
	}
	if mx == 0 || (ip > 0) != addressLookup {
		t.Errorf("DNS effects: MX=%d address=%d, want MX and addressLookup=%t", mx, ip, addressLookup)
	}
}

func TestNext1010IDNARealDNSRouting(t *testing.T) {
	for _, tc := range []struct {
		name, domain, ascii, mx, address, host string
		wantError, temporary, addressLookup    bool
		code                                   int
	}{
		{name: "unicode-explicit-mx", domain: "例子.test.", ascii: "xn--fsqu00a.test.", mx: "mx.xn--fsqu00a.test.", host: "mx.xn--fsqu00a.test"},
		{name: "unicode-implicit-ipv4", domain: "例子.test.", ascii: "xn--fsqu00a.test.", address: "a", host: "xn--fsqu00a.test", addressLookup: true},
		{name: "unicode-implicit-ipv6", domain: "例子.test.", ascii: "xn--fsqu00a.test.", address: "aaaa", host: "xn--fsqu00a.test", addressLookup: true},
		{name: "unicode-without-root-dot", domain: "例子.test", ascii: "xn--fsqu00a.test.", address: "a", host: "xn--fsqu00a.test", addressLookup: true},
		{name: "nontransitional-sharp-s", domain: "faß.test.", ascii: "xn--fa-hia.test.", address: "a", host: "xn--fa-hia.test", addressLookup: true},
		{name: "ascii-preserves-case-and-underscore", domain: "MiXeD_Name.test.", ascii: "MiXeD_Name.test.", address: "a", host: "MiXeD_Name.test", addressLookup: true},
		{name: "existing-alabel", domain: "xn--fsqu00a.test.", ascii: "xn--fsqu00a.test.", address: "a", host: "xn--fsqu00a.test", addressLookup: true},
		{name: "unicode-null-mx", domain: "例子.test.", ascii: "xn--fsqu00a.test.", mx: ".", address: "a", wantError: true, code: 556},
		{name: "unicode-mx-servfail", domain: "例子.test.", ascii: "xn--fsqu00a.test.", mx: "servfail", wantError: true, temporary: true},
		{name: "unicode-no-address", domain: "例子.test.", ascii: "xn--fsqu00a.test.", wantError: true, addressLookup: true},
		{name: "unicode-nxdomain", domain: "例子.test.", ascii: "xn--fsqu00a.test.", mx: "nxdomain", wantError: true, addressLookup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dns := &next1010IDNADNS{name: tc.ascii, mx: tc.mx, address: tc.address}
			rv := dns.resolver(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			from := "sender@example.test"
			to := []string{"one@" + tc.domain, `"team @ help"@` + tc.domain}
			mime := []byte("Subject: unchanged envelope\r\n\r\nraw body\r\n")
			var hosts []string
			err := deliverDirectWith(ctx, from, to, mime, true, smtpMXLookup(rv),
				func(gotCtx context.Context, host, addr, gotFrom string, gotTo []string, gotMIME []byte, required bool) error {
					hosts = append(hosts, host)
					if gotCtx != ctx || addr != net.JoinHostPort(host, "25") || gotFrom != from || !reflect.DeepEqual(gotTo, to) || !reflect.DeepEqual(gotMIME, mime) || !required {
						t.Errorf("IDNA changed envelope, bytes or TLS policy: host=%q addr=%q from=%q to=%q TLS=%t", host, addr, gotFrom, gotTo, required)
					}
					return nil
				})
			var wantHosts []string
			if tc.host != "" {
				wantHosts = []string{tc.host}
			}
			if (err != nil) != tc.wantError || !reflect.DeepEqual(hosts, wantHosts) {
				t.Errorf("IDNA routing: err=%v hosts=%q, want error=%t hosts=%q", err, hosts, tc.wantError, wantHosts)
			}
			var dnsErr *net.DNSError
			if tc.temporary && (!errors.As(err, &dnsErr) || !dnsErr.Temporary()) {
				t.Errorf("IDNA lost temporary DNS failure: %v", err)
			}
			var smtpError *textproto.Error
			code := 0
			if errors.As(err, &smtpError) {
				code = smtpError.Code
			}
			if code != tc.code {
				t.Errorf("IDNA SMTP code=%d, want %d: %v", code, tc.code, err)
			}
			dns.verify(t, tc.addressLookup)
		})
	}
}

func TestNext1010IDNAInvalidBeforeDNS(t *testing.T) {
	for _, domain := range []string{"\u200d.test", "\ufffd.test", "\xff.test"} {
		t.Run(fmt.Sprintf("%q", domain), func(t *testing.T) {
			lookups, sessions := 0, 0
			err := deliverDirectWith(t.Context(), "sender@example.test", []string{"reader@" + domain}, nil, true,
				func(context.Context, string) ([]*net.MX, error) { lookups++; return nil, nil },
				func(context.Context, string, string, string, []string, []byte, bool) error { sessions++; return nil })
			var smtpError *textproto.Error
			if !errors.As(err, &smtpError) || smtpError.Code != 553 || lookups != 0 || sessions != 0 {
				t.Errorf("invalid IDNA domain reached DNS/SMTP: DNS=%d sessions=%d err=%v, want 553 before effects", lookups, sessions, err)
			}
		})
	}
}

func TestNext1010IDNALiteralsBypassDNS(t *testing.T) {
	for _, tc := range []struct{ domain, host string }{{"[127.0.0.1]", "127.0.0.1"}, {"[IPv6:::1]", "::1"}} {
		t.Run(tc.domain, func(t *testing.T) {
			lookups, sessions := 0, 0
			to := []string{"reader@" + tc.domain}
			err := deliverDirectWith(t.Context(), "sender@example.test", to, nil, true,
				func(context.Context, string) ([]*net.MX, error) { lookups++; return nil, nil },
				func(_ context.Context, host, addr, _ string, gotTo []string, _ []byte, required bool) error {
					sessions++
					if host != tc.host || addr != net.JoinHostPort(tc.host, "25") || !reflect.DeepEqual(to, gotTo) || !required {
						t.Errorf("literal changed by IDNA routing: %q %q %q", host, addr, gotTo)
					}
					return nil
				})
			if err != nil || lookups != 0 || sessions != 1 {
				t.Errorf("literal routing: DNS=%d sessions=%d err=%v", lookups, sessions, err)
			}
		})
	}
}

func TestNext1010IDNACancellation(t *testing.T) {
	for _, stage := range []string{"before-dns", "during-mx", "during-address-proof", "during-smtp"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			mxCalls, ipCalls, sessions := 0, 0, 0
			if stage == "before-dns" {
				cancel()
			}
			rv := r5ImplicitResolverFuncs{
				mx: func(_ context.Context, domain string) ([]*net.MX, error) {
					mxCalls++
					if domain != "xn--fsqu00a.test." {
						t.Errorf("cancellable MX lookup received %q", domain)
					}
					if stage == "during-smtp" {
						return []*net.MX{{Host: "mx.first.test."}, {Host: "mx.second.test."}}, nil
					}
					if stage == "during-mx" {
						cancel()
					}
					return nil, &net.DNSError{Err: "no MX", Name: domain, IsNotFound: true}
				},
				ip: func(_ context.Context, domain string) ([]net.IPAddr, error) {
					ipCalls++
					if domain != "xn--fsqu00a.test." {
						t.Errorf("cancellable address lookup received %q", domain)
					}
					cancel()
					return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
				},
			}
			err := deliverDirectWith(ctx, "sender@example.test", []string{"reader@例子.test."}, nil, true, smtpMXLookup(rv),
				func(context.Context, string, string, string, []string, []byte, bool) error {
					sessions++
					cancel()
					return context.Canceled
				})
			wantMX, wantIP, wantSessions := 1, 0, 0
			switch stage {
			case "before-dns":
				wantMX = 0
			case "during-address-proof":
				wantIP = 1
			case "during-smtp":
				wantSessions = 1
			}
			if !errors.Is(err, context.Canceled) || mxCalls != wantMX || ipCalls != wantIP || sessions != wantSessions {
				t.Errorf("cancellation lost or retried: MX=%d IP=%d SMTP=%d err=%v, want %d/%d/%d", mxCalls, ipCalls, sessions, err, wantMX, wantIP, wantSessions)
			}
		})
	}
}
