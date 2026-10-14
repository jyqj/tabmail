package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/workqueue"
)

// These tests exercise the HTTP boundary only. They do not prove a deployment
// allowlist, DNS-rebinding prevention, or durable two-consumer claim fencing.
type r5WebhookRequest struct {
	method string
	header http.Header
	body   []byte
}

func r5CaptureWebhook(t *testing.T, r *http.Request) r5WebhookRequest {
	t.Helper()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		t.Errorf("read controlled webhook payload: %v", err)
	}
	return r5WebhookRequest{method: r.Method, header: r.Header.Clone(), body: body}
}

func r5AssertWebhookRequest(t *testing.T, got r5WebhookRequest, delivery *models.WebhookDelivery, secret string) {
	t.Helper()
	if got.method != http.MethodPost || !bytes.Equal(got.body, delivery.Payload) {
		t.Fatalf("webhook method/body changed: method=%s body=%q", got.method, got.body)
	}
	for key, expected := range map[string]string{
		"Content-Type":      "application/json",
		"X-TabMail-Event":   delivery.EventType,
		"X-TabMail-Attempt": strconv.Itoa(delivery.Attempts),
	} {
		if got.header.Get(key) != expected {
			t.Fatalf("header %s=%q, want %q", key, got.header.Get(key), expected)
		}
	}
	expectedSignature := ""
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(got.body)
		expectedSignature = hex.EncodeToString(mac.Sum(nil))
	}
	if got.header.Get("X-TabMail-Signature") != expectedSignature {
		t.Fatal("signature does not authenticate the actual received bytes")
	}
	if got.header.Get("Authorization") != "" || got.header.Get("Cookie") != "" {
		t.Fatal("unexpected ambient credentials on webhook request")
	}
}

func r5WebhookDelivery(target string, attempts int) *models.WebhookDelivery {
	return &models.WebhookDelivery{
		ID: uuid.New(), URL: target, EventType: "message.received", Attempts: attempts,
		Payload:   []byte(`{"type":"message.received","metadata":{"controlled":"payload"}}`),
		CreatedAt: time.Now().UTC(),
	}
}

func r5WebhookServer(t *testing.T, useTLS bool, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	if useTLS {
		s.StartTLS()
	} else {
		s.Start()
	}
	t.Cleanup(s.Close)
	return s
}

// Only the test transport trusts these local test certificates. Clone the
// dispatcher's controlled transport so its destination dial policy survives.
func r5TrustWebhookServers(t *testing.T, d *Dispatcher, servers ...*httptest.Server) {
	t.Helper()
	roots := x509.NewCertPool()
	for _, server := range servers {
		roots.AddCert(server.Certificate())
	}
	transport := d.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	d.client.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
}

func TestR5WebhookRedirectNeverForwards(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		for _, sameOrigin := range []bool{false, true} {
			for _, status := range []int{301, 302, 303, 307, 308} {
				name := "http/"
				if useTLS {
					name = "https/"
				}
				if sameOrigin {
					name += "same-origin/"
				} else {
					name += "cross-origin/"
				}
				t.Run(name+strconv.Itoa(status), func(t *testing.T) {
					var secondRequests atomic.Int32
					second := r5WebhookServer(t, useTLS, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						secondRequests.Add(1)
						w.WriteHeader(http.StatusOK)
					}))
					requests := make(chan r5WebhookRequest, 1)
					first := r5WebhookServer(t, useTLS, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/second" {
							secondRequests.Add(1)
							w.WriteHeader(http.StatusOK)
							return
						}
						requests <- r5CaptureWebhook(t, r)
						target := second.URL + "/second"
						if sameOrigin {
							target = "/second"
						}
						w.Header().Set("Location", target)
						w.WriteHeader(status)
					}))
					d := New(Config{URLs: first.URL, AllowedCIDRs: "127.0.0.1/32,::1/128", Secret: "controlled-secret", Timeout: 2 * time.Second}, zerolog.Nop())
					if useTLS {
						r5TrustWebhookServers(t, d, first, second)
					}
					delivery := r5WebhookDelivery(first.URL+"/first", 1)
					err := d.dispatch(context.Background(), delivery)
					if err == nil || err.Error() != "status "+strconv.Itoa(status) {
						t.Fatalf("redirect must use original non-2xx path, got %v", err)
					}
					if secondRequests.Load() != 0 {
						t.Fatalf("redirect destination received %d requests", secondRequests.Load())
					}
					select {
					case got := <-requests:
						r5AssertWebhookRequest(t, got, delivery, "controlled-secret")
					default:
						t.Fatal("configured receiver did not receive the original POST")
					}
				})
			}
		}
	}
}

func TestR5WebhookDirectControlledReceiver(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		for _, status := range []int{200, 204, 299, 400, 500} {
			for _, secret := range []string{"", "controlled-secret"} {
				t.Run(strconv.FormatBool(useTLS)+"/"+strconv.Itoa(status)+"/signed="+strconv.FormatBool(secret != ""), func(t *testing.T) {
					requests := make(chan r5WebhookRequest, 1)
					s := r5WebhookServer(t, useTLS, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests <- r5CaptureWebhook(t, r)
						w.WriteHeader(status)
					}))
					d := New(Config{URLs: s.URL, AllowedCIDRs: "127.0.0.1/32,::1/128", Secret: secret, Timeout: 2 * time.Second}, zerolog.Nop())
					if useTLS {
						r5TrustWebhookServers(t, d, s)
					}
					delivery := r5WebhookDelivery(s.URL, 2)
					err := d.dispatch(context.Background(), delivery)
					if status < 300 {
						if err != nil {
							t.Fatalf("direct controlled receiver rejected: %v", err)
						}
					} else if err == nil || err.Error() != "status "+strconv.Itoa(status) {
						t.Fatalf("non-2xx classification changed: %v", err)
					}
					select {
					case got := <-requests:
						r5AssertWebhookRequest(t, got, delivery, secret)
					default:
						t.Fatal("direct configured receiver did not receive request")
					}
				})
			}
		}
	}
}

func TestR5WebhookDefaultTLSRejectsUntrustedReceiver(t *testing.T) {
	var requests atomic.Int32
	s := r5WebhookServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	d := New(Config{URLs: s.URL, AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: 2 * time.Second}, zerolog.Nop())
	transport, ok := d.client.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil || transport.Proxy != nil || (transport.TLSClientConfig != nil && (transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.RootCAs != nil || transport.TLSClientConfig.ServerName != "")) {
		t.Fatal("production client must retain controlled direct dialing and system-trust TLS verification")
	}
	err := d.dispatch(context.Background(), r5WebhookDelivery(s.URL, 1))
	var certificateError x509.UnknownAuthorityError
	if !errors.As(err, &certificateError) {
		t.Fatalf("expected untrusted certificate error, got %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("untrusted receiver received an HTTP payload")
	}
}

func TestR5WebhookClosesSlowLargeResponseWithoutDrain(t *testing.T) {
	for _, status := range []int{200, 307, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			release := make(chan struct{})
			closed := make(chan struct{})
			s := r5WebhookServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r5CaptureWebhook(t, r)
				w.Header().Set("Content-Length", strconv.Itoa(64<<20))
				w.Header().Set("Location", "/never-follow")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("controlled prefix"))
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					close(closed)
				case <-release:
				}
			}))
			defer close(release)
			d := New(Config{URLs: s.URL, AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: 5 * time.Second}, zerolog.Nop())
			result := make(chan error, 1)
			go func() { result <- d.dispatch(context.Background(), r5WebhookDelivery(s.URL, 1)) }()
			select {
			case err := <-result:
				if status == 200 && err != nil {
					t.Fatal(err)
				}
				if status != 200 && (err == nil || err.Error() != "status "+strconv.Itoa(status)) {
					t.Fatalf("status error changed: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("dispatch waited for the unbounded response body")
			}
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("response body was not closed at the controlled receiver")
			}
		})
	}
}

func TestR5WebhookRequestContextBoundaries(t *testing.T) {
	for _, stage := range []string{"dial", "tls", "headers"} {
		for _, boundary := range []string{"cancel", "deadline", "client-timeout"} {
			t.Run(stage+"/"+boundary, func(t *testing.T) {
				entered := make(chan struct{})
				release := make(chan struct{})
				defer close(release)
				d := New(Config{AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: 5 * time.Second}, zerolog.Nop())
				target := "http://127.0.0.1:1/controlled"
				switch stage {
				case "dial":
					// A blocked test-only dialer never touches a real destination.
					// Transport may retain a dial for reuse after request cancellation;
					// release is therefore mandatory test cleanup, not a DNS assertion.
					d.destinationPolicy.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
						close(entered)
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-release:
							return nil, errors.New("controlled dial released")
						}
					}
					t.Cleanup(d.client.CloseIdleConnections)
				case "tls":
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = listener.Close() })
					target = "https://" + listener.Addr().String()
					go func() {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						defer conn.Close()
						// Read an actual TLS handshake prefix before opening the gate.
						var prefix [1]byte
						if _, err := io.ReadFull(conn, prefix[:]); err != nil {
							return
						}
						close(entered)
						<-release
					}()
				case "headers":
					s := r5WebhookServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_ = r5CaptureWebhook(t, r)
						close(entered)
						select {
						case <-r.Context().Done():
						case <-release:
						}
					}))
					target = s.URL
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if boundary == "deadline" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, time.Second)
					defer deadlineCancel()
				} else if boundary == "client-timeout" {
					d.client.Timeout = time.Second
				}
				result := make(chan error, 1)
				go func() { result <- d.dispatch(ctx, r5WebhookDelivery(target, 1)) }()
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("request never entered the controlled blocking stage")
				}
				expected := context.DeadlineExceeded
				if boundary == "cancel" {
					cancel()
					expected = context.Canceled
				}
				select {
				case err := <-result:
					var wrapped *url.Error
					if !errors.Is(err, expected) || !errors.As(err, &wrapped) {
						t.Fatalf("context boundary lost its wrapped error: got %v, want %v", err, expected)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("context/client timeout did not bound the blocked request")
				}
			})
		}
	}
}

type r5WebhookMark struct {
	id    uuid.UUID
	state string
	err   string
	next  time.Time
}

// This fake covers the existing delivery adapter/worker routing only, not PG
// claim tokens, lease stealing, concurrent consumers, or external idempotency.
type r5WebhookBoundaryStore struct {
	delivery *models.WebhookDelivery
	marks    chan r5WebhookMark
}

func (s *r5WebhookBoundaryStore) ClaimWebhookDeliveries(context.Context, time.Time, int) ([]*models.WebhookDelivery, error) {
	if s.delivery == nil {
		return nil, nil
	}
	delivery := s.delivery
	s.delivery = nil
	return []*models.WebhookDelivery{delivery}, nil
}

func (s *r5WebhookBoundaryStore) MarkWebhookDeliveryDoneClaim(_ context.Context, id uuid.UUID, _ int) error {
	s.marks <- r5WebhookMark{id: id, state: "delivered"}
	return nil
}

func (s *r5WebhookBoundaryStore) MarkWebhookDeliveryRetryClaim(_ context.Context, id uuid.UUID, _ int, lastError string, next time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	s.marks <- r5WebhookMark{id: id, state: state, err: lastError, next: next}
	return nil
}

func TestR5WebhookRedirectDeliveryRetryRouting(t *testing.T) {
	for _, test := range []struct {
		status   int
		attempts int
		state    string
	}{{200, 1, "delivered"}, {301, 1, "retry"}, {307, 1, "retry"}, {308, 3, "dead"}} {
		t.Run(strconv.Itoa(test.status)+"/"+test.state, func(t *testing.T) {
			var secondRequests atomic.Int32
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce atomic.Bool
			releaseReceiver := func() {
				if releaseOnce.CompareAndSwap(false, true) {
					close(release)
				}
			}
			defer releaseReceiver()
			s := r5WebhookServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/second" {
					secondRequests.Add(1)
					w.WriteHeader(http.StatusOK)
					return
				}
				_ = r5CaptureWebhook(t, r)
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Location", "/second")
				w.WriteHeader(test.status)
			}))
			d := New(Config{AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: 5 * time.Second, Secret: "controlled-secret", MaxRetries: 3, RetryDelay: 20 * time.Millisecond}, zerolog.Nop())
			delivery := r5WebhookDelivery(s.URL+"/first", test.attempts)
			store := &r5WebhookBoundaryStore{delivery: delivery, marks: make(chan r5WebhookMark, 1)}
			worker := workqueue.NewWorker[*deliveryPayload](newDeliveryStore(store), d.processDelivery,
				workqueue.LinearBackoff[*deliveryPayload]{Base: d.retryDelay, Max: d.maxRetries},
				&deliveryHooks{dispatcher: d}, 5*time.Minute, time.Hour, 1, zerolog.Nop())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan struct{})
			go func() { defer close(stopped); worker.Run(ctx) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("delivery worker never reached the controlled receiver")
			}
			select {
			case got := <-store.marks:
				t.Fatalf("worker marked %s before the response barrier released", got.state)
			default:
			}
			releasedAt := time.Now()
			releaseReceiver()
			select {
			case mark := <-store.marks:
				if mark.id != delivery.ID || mark.state != test.state {
					t.Fatalf("unexpected worker route: %+v", mark)
				}
				if test.status != 200 && mark.err != "status "+strconv.Itoa(test.status) {
					t.Fatalf("worker lost redirect error: %+v", mark)
				}
				if test.state == "retry" && mark.next.Before(releasedAt.Add(d.retryDelay*time.Duration(test.attempts))) {
					t.Fatal("redirect changed existing linear retry timing")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("delivery worker did not mark the HTTP outcome")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("worker failed to stop after outcome")
			}
			if secondRequests.Load() != 0 {
				t.Fatal("worker followed a redirect before selecting its retry route")
			}
			letters := d.DeadLetters(10)
			if test.state == "dead" {
				if len(letters) != 1 || letters[0].ID != delivery.ID.String() || letters[0].Attempts != test.attempts || !bytes.Equal(letters[0].Payload, delivery.Payload) {
					t.Fatalf("existing dead-letter delivery snapshot changed: %+v", letters)
				}
			} else if len(letters) != 0 {
				t.Fatal("non-terminal delivery gained a dead letter")
			}
		})
	}
}
