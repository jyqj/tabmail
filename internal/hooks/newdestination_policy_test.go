package hooks

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

// These tests use synthetic DNS and only loopback receivers. They do not
// certify PG claim fencing, production DNS/routes, a proxy, or external
// idempotency. Never fall back to real DNS for a fixture hostname.
func destinationTestPolicy(t *testing.T, cidrs string) *destinationPolicy {
	t.Helper()
	p, err := newDestinationPolicy(cidrs, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		t.Error("unexpected resolver call")
		return nil, errors.New("fixture resolver only")
	}
	return p
}

func TestWebhookDestinationAddressClasses(t *testing.T) {
	p := destinationTestPolicy(t, "")
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		t.Run("public/"+ip, func(t *testing.T) {
			if !p.permits(netip.MustParseAddr(ip)) {
				t.Fatal("ordinary public address rejected")
			}
		})
	}
	for _, block := range webhookSpecialPrefixes {
		t.Run("special/"+block.String(), func(t *testing.T) {
			if p.permits(block.Addr()) {
				t.Fatal("special-purpose address allowed implicitly")
			}
		})
	}
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "224.0.0.1", "ff02::1", "255.255.255.255", "4000::1", "fe80::1%lo0"} {
		t.Run("denied/"+ip, func(t *testing.T) {
			if p.permits(netip.MustParseAddr(ip)) {
				t.Fatal("non-public address allowed implicitly")
			}
		})
	}
	if p.permits(netip.Addr{}) {
		t.Fatal("invalid address allowed")
	}
	controlled := destinationTestPolicy(t, "127.0.0.1/32,::1/128,10.0.0.0/24,fc00::/64,169.254.1.1/32,64:ff9b::1/128")
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.9", "fc00::9", "169.254.1.1", "64:ff9b::1"} {
		if !controlled.permits(netip.MustParseAddr(ip)) {
			t.Fatalf("explicitly controlled address rejected: %s", ip)
		}
	}
	if controlled.permits(netip.MustParseAddr("127.0.0.2")) || controlled.permits(netip.MustParseAddr("10.0.1.1")) {
		t.Fatal("CIDR scope widened")
	}
	wide := destinationTestPolicy(t, "0.0.0.0/0,::/0")
	for _, ip := range []string{"0.0.0.0", "::", "224.0.0.1", "ff02::1", "255.255.255.255", "fe80::1%lo0"} {
		if wide.permits(netip.MustParseAddr(ip)) {
			t.Fatalf("hard-denied address authorized by CIDR: %s", ip)
		}
	}
}

func TestWebhookDestinationCIDRFailClosed(t *testing.T) {
	for _, cidrs := range []string{"not-a-cidr", "127.0.0.1/32,bad", "127.0.0.1/32,", ",127.0.0.1/32", "10.0.0.1/24", "::ffff:127.0.0.1/120", "::ffff:0.0.0.0/80", "fe80::1%lo0/128"} {
		t.Run(cidrs, func(t *testing.T) {
			p, err := newDestinationPolicy(cidrs, time.Second)
			if p != nil || err == nil || err.Error() != "webhook destination policy: invalid_cidr" {
				t.Fatalf("invalid CIDR must fail whole policy closed: policy=%v error=%v", p != nil, err)
			}
		})
	}
	p := destinationTestPolicy(t, "::ffff:127.0.0.1/128,::ffff:10.0.0.0/104")
	if len(p.allowed) != 2 || p.allowed[0].String() != "127.0.0.1/32" || p.allowed[1].String() != "10.0.0.0/8" {
		t.Fatal("mapped CIDR did not normalize to the exact IPv4 prefix")
	}
	if !p.permits(netip.MustParseAddr("127.0.0.1")) || p.permits(netip.MustParseAddr("127.0.0.2")) {
		t.Fatal("mapped /128 changed receiver scope")
	}
}

func TestWebhookDestinationURLValidation(t *testing.T) {
	p := destinationTestPolicy(t, "127.0.0.1/32,::1/128")
	for _, raw := range []string{"http://8.8.8.8/a?keep=query", "https://receiver.test:8443/a", "http://127.0.0.1:1234", "https://[::1]:8443", "http://[::ffff:127.0.0.1]:1234"} {
		if err := p.validateURL(raw); err != nil {
			t.Fatalf("valid destination rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"/relative", "file:///a", "ftp://8.8.8.8", "http:opaque", "https://", "http://user:canary@receiver.test/a?secret=canary", "http://receiver.test/#canary", "http://[fe80::1%25lo0]/", "http://receiver.test:0/", "http://receiver.test:65536/", "http://receiver.test:/", "http://receiver.test:no/", "http://127.0.0.2/", "http://[::]/", "http://receiver.test/%zz?canary"} {
		err := p.validateURL(raw)
		if err == nil || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), raw) {
			t.Fatalf("invalid destination accepted or leaked: %v", err)
		}
	}
}

type destinationSyntheticConn struct {
	peer   net.Addr
	closed bool
}

func (c *destinationSyntheticConn) Read([]byte) (int, error)         { return 0, errors.New("synthetic conn") }
func (c *destinationSyntheticConn) Write([]byte) (int, error)        { return 0, errors.New("synthetic conn") }
func (c *destinationSyntheticConn) Close() error                     { c.closed = true; return nil }
func (c *destinationSyntheticConn) LocalAddr() net.Addr              { return c.peer }
func (c *destinationSyntheticConn) RemoteAddr() net.Addr             { return c.peer }
func (c *destinationSyntheticConn) SetDeadline(time.Time) error      { return nil }
func (c *destinationSyntheticConn) SetReadDeadline(time.Time) error  { return nil }
func (c *destinationSyntheticConn) SetWriteDeadline(time.Time) error { return nil }

func TestWebhookDestinationAllAnswersBeforeNumericDial(t *testing.T) {
	for _, network := range []string{"tcp", "tcp4", "tcp6"} {
		for _, answer := range [][]netip.Addr{
			{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
			{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("fc00::1")},
			{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("::ffff:127.0.0.1")},
			{netip.Addr{}}, {},
		} {
			p := destinationTestPolicy(t, "")
			p.lookup = func(_ context.Context, family, host string) ([]netip.Addr, error) {
				if family != "ip" || host != "synthetic.test" {
					t.Fatal("resolver did not request full IP answer")
				}
				return answer, nil
			}
			calls := 0
			p.dial = func(context.Context, string, string) (net.Conn, error) {
				calls++
				return nil, errors.New("must not dial")
			}
			if conn, err := p.dialContext(context.Background(), network, "synthetic.test:443"); conn != nil || err == nil || calls != 0 {
				t.Fatalf("mixed/empty answer must cause zero sockets: network=%s calls=%d error=%v", network, calls, err)
			}
		}
	}
	p := destinationTestPolicy(t, "127.0.0.1/32,::1/128")
	p.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::ffff:127.0.0.1"), netip.MustParseAddr("::1")}, nil
	}
	var calls []string
	var deadlines []time.Time
	p.dial = func(ctx context.Context, _, addr string) (net.Conn, error) {
		calls = append(calls, addr)
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("dial missing total deadline")
		}
		deadlines = append(deadlines, deadline)
		if len(calls) == 1 {
			return nil, errors.New("synthetic first address failure")
		}
		return &destinationSyntheticConn{peer: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 443}}, nil
	}
	conn, err := p.dialContext(context.Background(), "tcp", "synthetic.test:443")
	if err != nil || conn == nil || !reflect.DeepEqual(calls, []string{"127.0.0.1:443", "[::1]:443"}) || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("numeric fallback/dedup/deadline contract failed: calls=%v error=%v", calls, err)
	}
	_ = conn.Close()
}

func TestWebhookDestinationPeerAndLiteralChecks(t *testing.T) {
	p := destinationTestPolicy(t, "127.0.0.1/32,::1/128")
	peer := &destinationSyntheticConn{peer: &net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 1234}}
	p.dial = func(context.Context, string, string) (net.Conn, error) { return peer, nil }
	if conn, err := p.dialContext(context.Background(), "tcp", "127.0.0.1:1234"); conn != nil || err == nil || !peer.closed {
		t.Fatal("mismatched peer must close before transport sees it")
	}
	var dialed string
	p.dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = address
		return &destinationSyntheticConn{peer: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 1234}}, nil
	}
	if conn, err := p.dialContext(context.Background(), "tcp", "[::1]:1234"); err != nil || conn == nil || dialed != "[::1]:1234" {
		t.Fatal("literal IPv6 must use canonical numeric dialing without resolver")
	}
	for _, addr := range []string{"[fe80::1%lo0]:1234", "127.0.0.2:1234", "127.0.0.1:0", "bad-address"} {
		dialed = ""
		if conn, err := p.dialContext(context.Background(), "tcp", addr); conn != nil || err == nil || dialed != "" {
			t.Fatal("invalid/forbidden literal reached dial")
		}
	}
}

func TestWebhookDestinationDNSFailureAndCancel(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		p := destinationTestPolicy(t, "")
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		p.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
			if canceled {
				return nil, ctx.Err()
			}
			return nil, errors.New("DNS_QUERY_CANARY")
		}
		p.dial = func(context.Context, string, string) (net.Conn, error) {
			t.Error("DNS failure/cancel reached numeric dial")
			return nil, errors.New("unexpected dial")
		}
		conn, err := p.dialContext(ctx, "tcp", "synthetic.test:443")
		cancel()
		if conn != nil || err == nil || strings.Contains(err.Error(), "CANARY") || (canceled && !errors.Is(err, context.Canceled)) {
			t.Fatal("DNS failure/cancel leaked or lost its bounded identity")
		}
	}
}

func TestWebhookDestinationMixedAnswerNeverPosts(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	d := New(Config{AllowedCIDRs: "127.0.0.1/32", Timeout: time.Second}, zerolog.Nop())
	t.Cleanup(d.client.CloseIdleConnections)
	d.destinationPolicy.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("fc00::1")}, nil
	}
	var dials atomic.Int32
	d.destinationPolicy.dial = func(context.Context, string, string) (net.Conn, error) {
		// Never connect to any candidate if the policy regresses in this test.
		dials.Add(1)
		return nil, errors.New("synthetic dial must not run")
	}
	err = d.dispatch(context.Background(), r5WebhookDelivery("http://synthetic.test:"+port+"/hook", 1))
	if err == nil || posts.Load() != 0 || dials.Load() != 0 {
		t.Fatal("mixed A/AAAA answer opened a socket or sent a POST")
	}
}

func TestWebhookDestinationDNSAndFallbackTotalDeadline(t *testing.T) {
	for _, stage := range []string{"dns", "fallback"} {
		t.Run(stage, func(t *testing.T) {
			p := destinationTestPolicy(t, "127.0.0.1/32,::1/128")
			p.timeout = 30 * time.Millisecond
			calls := 0
			p.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
				if stage == "dns" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}, nil
			}
			p.dial = func(ctx context.Context, _, _ string) (net.Conn, error) { calls++; <-ctx.Done(); return nil, ctx.Err() }
			start := time.Now()
			conn, err := p.dialContext(context.Background(), "tcp", "synthetic.test:443")
			if conn != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second || (stage == "dns" && calls != 0) || (stage == "fallback" && calls != 1) {
				t.Fatalf("total DNS/fallback deadline violated: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestWebhookDestinationRebindingAndApprovedPoolReuse(t *testing.T) {
	var proxyPosts atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyPosts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	d := New(Config{AllowedCIDRs: "127.0.0.1/32", Timeout: time.Second}, zerolog.Nop())
	t.Cleanup(d.client.CloseIdleConnections)
	var lookups atomic.Int32
	d.destinationPolicy.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.2")}, nil
	}
	delivery := r5WebhookDelivery("http://synthetic.test:"+port+"/hook", 1)
	for i := 0; i < 2; i++ {
		if err := d.dispatch(context.Background(), delivery); err != nil {
			t.Fatal(err)
		}
	}
	if posts.Load() != 2 || lookups.Load() != 1 {
		t.Fatal("204 connection was not safely reused at its original peer")
	}
	d.client.CloseIdleConnections()
	if err := d.dispatch(context.Background(), delivery); err == nil || posts.Load() != 2 || lookups.Load() != 2 {
		t.Fatal("new connection did not recheck rebinding answer before POST")
	}
	if proxyPosts.Load() != 0 {
		t.Fatal("environment proxy received webhook traffic")
	}
}

func TestWebhookDestinationTLSOriginalHostname(t *testing.T) {
	type requestIdentity struct{ host, sni string }
	identities := make(chan requestIdentity, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identities <- requestIdentity{r.Host, r.TLS.ServerName}
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.StartTLS()
	t.Cleanup(srv.Close)
	cert := srv.Certificate()
	if len(cert.DNSNames) == 0 {
		t.Fatal("controlled TLS certificate lacks a DNS SAN")
	}
	hostname := cert.DNSNames[0]
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	d := New(Config{AllowedCIDRs: "127.0.0.1/32", Timeout: time.Second}, zerolog.Nop())
	d.destinationPolicy.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	transport := d.client.Transport.(*http.Transport).Clone()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	d.client.Transport = transport
	t.Cleanup(d.client.CloseIdleConnections)
	if err := d.dispatch(context.Background(), r5WebhookDelivery("https://"+net.JoinHostPort(hostname, port)+"/hook", 1)); err != nil {
		t.Fatal(err)
	}
	identity := <-identities
	if identity.host != net.JoinHostPort(hostname, port) || identity.sni != hostname {
		t.Fatal("numeric TCP dialing changed HTTP Host or TLS SNI")
	}
	err = d.dispatch(context.Background(), r5WebhookDelivery("https://"+net.JoinHostPort("wrong.synthetic.test", port)+"/hook", 1))
	var mismatch x509.HostnameError
	if !errors.As(err, &mismatch) {
		t.Fatalf("wrong hostname did not retain certificate validation: %v", err)
	}
	select {
	case <-identities:
		t.Fatal("hostname-mismatched TLS peer received a POST")
	default:
	}
}

func TestWebhookDestinationPolicyErrorRedactionAndDirectProxy(t *testing.T) {
	var logs bytes.Buffer
	d := New(Config{AllowedCIDRs: "127.0.0.1/32", MaxRetries: 2, RetryDelay: time.Millisecond}, zerolog.New(&logs))
	d.destinationPolicy.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("resolver QUERY_CANARY PAYLOAD_CANARY")
	}
	transport := d.client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.DialTLSContext != nil || transport.DialTLS != nil || transport.DialContext == nil || transport == http.DefaultTransport {
		t.Fatal("dedicated direct transport has a bypass or shares default pool")
	}
	delivery := &models.WebhookDelivery{ID: uuid.New(), URL: "http://synthetic.test/hook?secret=QUERY_CANARY", Payload: []byte("PAYLOAD_CANARY"), Attempts: 1}
	err := d.dispatch(context.Background(), delivery)
	var wrapped *url.Error
	if err == nil || errors.As(err, &wrapped) || strings.Contains(err.Error(), "CANARY") {
		t.Fatal("policy error retained raw resolver/url wrapper")
	}
	d.dispatchDirect(delivery)
	letters := d.DeadLetters(1)
	if len(letters) != 1 || strings.Contains(letters[0].LastError, "CANARY") || strings.Contains(logs.String(), "CANARY") || letters[0].URL != delivery.URL || !bytes.Equal(letters[0].Payload, delivery.Payload) {
		t.Fatal("policy log/LastError leaked or original dead-letter DTO changed")
	}
	wrappedErr := &url.Error{Op: "Post", URL: delivery.URL, Err: context.DeadlineExceeded}
	redacted := redactWebhookURLError(wrappedErr)
	if !errors.As(redacted, &wrapped) || !errors.Is(redacted, context.DeadlineExceeded) || !wrapped.Timeout() || strings.Contains(redacted.Error(), "CANARY") {
		t.Fatal("nonpolicy url redaction lost error identity or leaked query")
	}
	invalid := New(Config{AllowedCIDRs: "127.0.0.1/32,bad"}, zerolog.Nop())
	if err := invalid.dispatch(context.Background(), delivery); err == nil || err.Error() != "webhook destination policy: invalid_cidr" {
		t.Fatal("invalid config did not reject every dispatch")
	}
}

func TestWebhookDestinationRetryRoutingRedaction(t *testing.T) {
	for _, test := range []struct {
		name     string
		attempts int
		state    string
	}{{"retry", 1, "retry"}, {"dead", 3, "dead"}} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			d := New(Config{Timeout: time.Second, MaxRetries: 3, RetryDelay: time.Millisecond}, zerolog.New(&logs))
			t.Cleanup(d.client.CloseIdleConnections)
			d.destinationPolicy.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
				return nil, errors.New("DNS_CANARY")
			}
			delivery := r5WebhookDelivery("http://synthetic.test/hook?secret=QUERY_CANARY", test.attempts)
			delivery.Payload = []byte("PAYLOAD_CANARY")
			store := &r5WebhookBoundaryStore{delivery: delivery, marks: make(chan r5WebhookMark, 1)}
			worker := workqueue.NewWorker[*deliveryPayload](newDeliveryStore(store), d.processDelivery,
				workqueue.LinearBackoff[*deliveryPayload]{Base: d.retryDelay, Max: d.maxRetries},
				&deliveryHooks{dispatcher: d}, 5*time.Minute, time.Hour, 1, zerolog.Nop())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan struct{})
			before := time.Now()
			go func() { defer close(stopped); worker.Run(ctx) }()
			select {
			case mark := <-store.marks:
				if mark.id != delivery.ID || mark.state != test.state || mark.err != "webhook destination policy: dns_failed" || strings.Contains(mark.err, "CANARY") {
					t.Fatalf("policy changed worker identity/route or leaked LastError: %+v", mark)
				}
				if test.state == "retry" && mark.next.Before(before.Add(d.retryDelay*time.Duration(test.attempts))) {
					t.Fatal("policy changed linear retry cadence")
				}
			case <-time.After(time.Second):
				t.Fatal("policy rejection did not reach the original delivery adapter")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("synthetic worker failed to stop")
			}
			if strings.Contains(logs.String(), "CANARY") {
				t.Fatal("policy rejection leaked input in logs")
			}
			letters := d.DeadLetters(1)
			if test.state == "dead" && (len(letters) != 1 || letters[0].LastError != "webhook destination policy: dns_failed" || letters[0].ID != delivery.ID.String() || letters[0].Attempts != test.attempts || !bytes.Equal(letters[0].Payload, delivery.Payload)) {
				t.Fatal("policy changed dead-letter snapshot or leaked LastError")
			}
			if test.state == "retry" && len(letters) != 0 {
				t.Fatal("non-terminal policy rejection gained a dead letter")
			}
		})
	}
}

type destinationFanoutStore struct {
	*testutil.FakeStore
	endpoints []*models.WebhookEndpoint
	urls      []string
}

func (s *destinationFanoutStore) ListWebhookEndpoints(context.Context, uuid.UUID) ([]*models.WebhookEndpoint, error) {
	return s.endpoints, nil
}
func (s *destinationFanoutStore) CreateWebhookDeliveries(_ context.Context, _ *models.OutboxEvent, urls []string) error {
	s.urls = append([]string(nil), urls...)
	return nil
}

func TestWebhookDestinationGlobalTenantAndClaimedDelivery(t *testing.T) {
	tenant := uuid.New()
	st := &destinationFanoutStore{FakeStore: testutil.NewFakeStore(), endpoints: []*models.WebhookEndpoint{{URL: "http://127.0.0.2/tenant", IsActive: true}}}
	d := New(Config{URLs: "http://127.0.0.2/global"}, zerolog.Nop()).BindStore(st)
	event := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{"tenant_id":"` + tenant.String() + `"}`)}
	if err := d.processOutbox(context.Background(), &workqueue.Job[*outboxPayload]{Payload: &outboxPayload{OutboxEvent: event}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.urls, []string{"http://127.0.0.2/global", "http://127.0.0.2/tenant"}) {
		t.Fatal("policy silently filtered fanout or changed URL identity")
	}
	for _, target := range append(st.urls, "http://127.0.0.2/old-delivery") {
		delivery := &models.WebhookDelivery{ID: uuid.New(), EventID: event.ID, URL: target, Payload: append([]byte(nil), event.Payload...), Attempts: 2}
		before := *delivery
		err := d.processDelivery(context.Background(), &workqueue.Job[*deliveryPayload]{Payload: &deliveryPayload{WebhookDelivery: delivery}})
		if err == nil || err.Error() != "webhook destination policy: address_not_allowed" || !reflect.DeepEqual(before, *delivery) {
			t.Fatal("claimed global/tenant/old delivery bypassed policy or changed retry identity")
		}
	}
}

func TestWebhookDestinationConfigEnvironmentKey(t *testing.T) {
	// Reflect the real config types without reading environment credentials or
	// calling config.Load (which validates unrelated production resources).
	rootField, ok := reflect.TypeOf(config.Root{}).FieldByName("Webhook")
	if !ok || rootField.Type != reflect.TypeOf(config.Webhook{}) {
		t.Fatal("formal config owner changed")
	}
	field, ok := rootField.Type.FieldByName("AllowedCIDRs")
	if !ok {
		t.Fatal("formal webhook CIDR field missing")
	}
	inner := reflect.StructOf([]reflect.StructField{field})
	outer := reflect.StructOf([]reflect.StructField{{Name: rootField.Name, Type: inner, Tag: rootField.Tag}})
	fixture := reflect.New(outer).Interface()
	var keys bytes.Buffer
	if err := envconfig.Usagef("TABMAIL", fixture, &keys, "{{range .}}{{.Key}}\n{{end}}"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(keys.String()) != "TABMAIL_WEBHOOK_ALLOWED_CIDRS" {
		t.Fatalf("wrong canonical environment key: %q", keys.String())
	}
	// All lookups in this exact-field fixture are controlled test variables.
	t.Setenv("ALLOWED_CIDRS", "")
	t.Setenv("TABMAIL_WEBHOOK_ALLOWED_CID_RS", "127.0.0.2/32")
	t.Setenv("TABMAIL_WEBHOOK_ALLOWED_CIDRS", "127.0.0.1/32,::1/128")
	if err := envconfig.Process("TABMAIL", fixture); err != nil {
		t.Fatal(err)
	}
	loaded := reflect.ValueOf(fixture).Elem().FieldByName("Webhook").FieldByName("AllowedCIDRs").String()
	if loaded != "127.0.0.1/32,::1/128" {
		t.Fatal("canonical key did not reach the formal field")
	}
	p := destinationTestPolicy(t, loaded)
	if !p.permits(netip.MustParseAddr("127.0.0.1")) || p.permits(netip.MustParseAddr("127.0.0.2")) {
		t.Fatal("environment policy scope widened")
	}
}
