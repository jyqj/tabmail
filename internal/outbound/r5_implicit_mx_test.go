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

// This fixture serves real DNS packets only on an owned loopback UDP socket.
// Resolver.Dial sends every query here, regardless of machine DNS settings.
// The final SMTP session is captured per call; no message leaves the test.
type r5ImplicitDNS struct {
	mx      string
	address string
	mu      sync.Mutex
	queries []dnsmessage.Type
}

func (d *r5ImplicitDNS) resolver(t *testing.T) *net.Resolver {
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
					t.Errorf("DNS read: %v", err)
				}
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buf[:n]); err != nil || len(query.Questions) != 1 {
				t.Errorf("invalid DNS query: %v", err)
				continue
			}
			q := query.Questions[0]
			d.mu.Lock()
			d.queries = append(d.queries, q.Type)
			d.mu.Unlock()
			reply := dnsmessage.Message{
				Header: dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true,
					RecursionDesired: query.RecursionDesired, RecursionAvailable: true},
				Questions: query.Questions,
			}
			header := dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 60}
			switch {
			case d.mx == "nxdomain":
				reply.RCode = dnsmessage.RCodeNameError
			case q.Type == dnsmessage.TypeMX:
				switch d.mx {
				case "servfail":
					reply.RCode = dnsmessage.RCodeServerFailure
				case "null", "ordinary":
					host := "."
					if d.mx == "ordinary" {
						host = "mx.explicit.test."
					}
					reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.MXResource{MX: dnsmessage.MustNewName(host)}}}
				}
			case d.address == "servfail":
				reply.RCode = dnsmessage.RCodeServerFailure
			case q.Type == dnsmessage.TypeA && d.address == "a":
				reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}}}
			case q.Type == dnsmessage.TypeAAAA && d.address == "aaaa":
				reply.Answers = []dnsmessage.Resource{{Header: header, Body: &dnsmessage.AAAAResource{AAAA: [16]byte{15: 1}}}}
			}
			packet, err := reply.Pack()
			if err != nil {
				t.Errorf("DNS reply: %v", err)
				continue
			}
			if _, err := conn.WriteTo(packet, peer); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("DNS write: %v", err)
			}
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		<-done
	})
	return &net.Resolver{PreferGo: true, StrictErrors: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", conn.LocalAddr().String())
		},
	}
}

func (d *r5ImplicitDNS) addressQueries() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, typ := range d.queries {
		if typ == dnsmessage.TypeA || typ == dnsmessage.TypeAAAA {
			n++
		}
	}
	return n
}

func TestR5ImplicitMXRealDNS(t *testing.T) {
	for _, tc := range []struct {
		name, mx, address, host string
		wantError, temporary    bool
		code                    int
		noAddressLookup         bool
	}{
		{name: "nodata_with_ipv4", address: "a", host: "implicit.test"},
		{name: "nodata_with_ipv6", address: "aaaa", host: "implicit.test"},
		{name: "nxdomain", mx: "nxdomain", wantError: true},
		{name: "nodata_without_address", wantError: true},
		{name: "mx_servfail", mx: "servfail", wantError: true, temporary: true, noAddressLookup: true},
		{name: "address_servfail", address: "servfail", wantError: true, temporary: true},
		{name: "null_mx_with_address", mx: "null", address: "a", wantError: true, code: 556, noAddressLookup: true},
		{name: "explicit_mx_with_address", mx: "ordinary", address: "a", host: "mx.explicit.test", noAddressLookup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &r5ImplicitDNS{mx: tc.mx, address: tc.address}
			resolver := fixture.resolver(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			from, to, mime := "sender@example.test", []string{"one@implicit.test.", "two@implicit.test."}, []byte("synthetic message")
			var hosts []string
			err := deliverDirectWith(ctx, from, to, mime, true, smtpMXLookup(resolver),
				func(gotCtx context.Context, host, addr, gotFrom string, gotTo []string, gotMIME []byte, tls bool) error {
					hosts = append(hosts, host)
					if gotCtx != ctx || addr != host+":25" || gotFrom != from || !reflect.DeepEqual(gotTo, to) || !reflect.DeepEqual(gotMIME, mime) || !tls {
						t.Errorf("session input changed: host=%q addr=%q from=%q to=%v mime=%q TLS=%v", host, addr, gotFrom, gotTo, gotMIME, tls)
					}
					return nil
				})
			var wantHosts []string
			if tc.host != "" {
				wantHosts = []string{tc.host}
			}
			if (err != nil) != tc.wantError || !reflect.DeepEqual(hosts, wantHosts) {
				t.Errorf("routing: hosts=%v want=%v error=%v wantError=%v", hosts, wantHosts, err, tc.wantError)
			}
			var dnsErr *net.DNSError
			if tc.temporary && (!errors.As(err, &dnsErr) || !dnsErr.Temporary()) {
				t.Errorf("lost DNS retry classification: %v", err)
			}
			var reply *textproto.Error
			code := 0
			if errors.As(err, &reply) {
				code = reply.Code
			}
			if code != tc.code {
				t.Errorf("SMTP classification: code=%d want=%d error=%v", code, tc.code, err)
			}
			if tc.noAddressLookup && fixture.addressQueries() != 0 {
				t.Errorf("MX result incorrectly triggered %d address queries", fixture.addressQueries())
			}
			if tc.host == "implicit.test" && fixture.addressQueries() == 0 {
				t.Error("implicit route lacks positive address lookup")
			}
		})
	}
}

type r5ImplicitResolverFuncs struct {
	mx func(context.Context, string) ([]*net.MX, error)
	ip func(context.Context, string) ([]net.IPAddr, error)
}

func (r r5ImplicitResolverFuncs) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return r.mx(ctx, domain)
}

func (r r5ImplicitResolverFuncs) LookupIPAddr(ctx context.Context, domain string) ([]net.IPAddr, error) {
	return r.ip(ctx, domain)
}

// In-memory controls cover cancellation at the exact address-proof boundary
// and malformed/empty successful resolver responses, which a DNS packet cannot
// represent as a successful net.Resolver.LookupIPAddr result.
func TestR5ImplicitMXAddressProofBoundary(t *testing.T) {
	for _, mode := range []string{"empty", "nil_ip", "cancel_before_proof", "cancel_during_proof", "address_deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ipCalls, sessions := 0, 0
			mxErr := &net.DNSError{Err: "no such host", Name: "proof.test", IsNotFound: true}
			resolver := r5ImplicitResolverFuncs{
				mx: func(context.Context, string) ([]*net.MX, error) {
					if mode == "cancel_before_proof" {
						cancel()
					}
					return nil, mxErr
				},
				ip: func(gotCtx context.Context, domain string) ([]net.IPAddr, error) {
					ipCalls++
					if gotCtx != ctx || domain != "proof.test" {
						t.Errorf("address lookup context/domain changed: %q", domain)
					}
					switch mode {
					case "cancel_during_proof":
						cancel()
						return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, nil
					case "address_deadline":
						return nil, fmt.Errorf("address lookup: %w", context.DeadlineExceeded)
					case "nil_ip":
						return []net.IPAddr{{}}, nil
					default:
						return nil, nil
					}
				},
			}
			err := deliverDirectWith(ctx, "sender@example.test", []string{"one@proof.test"}, nil, false, smtpMXLookup(resolver),
				func(context.Context, string, string, string, []string, []byte, bool) error { sessions++; return nil })
			wantIPCalls := 1
			if mode == "cancel_before_proof" {
				wantIPCalls = 0
			}
			if err == nil || sessions != 0 || ipCalls != wantIPCalls {
				t.Errorf("unproven route: IP lookups=%d want=%d sessions=%d error=%v", ipCalls, wantIPCalls, sessions, err)
			}
			if (mode == "cancel_before_proof" || mode == "cancel_during_proof") && !errors.Is(err, context.Canceled) {
				t.Errorf("lost cancellation: %v", err)
			}
			if mode == "address_deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("lost address deadline: %v", err)
			}
		})
	}
}
