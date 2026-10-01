package testpg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/testutil"
)

type R5HTTPObservation struct {
	Method, Path, ResponseSHA256, ErrorCode string
	Status                                  int
}
type R5HTTPFixture struct {
	*R5Fixture
	Server                      *httptest.Server
	Objects                     *testutil.R5ObjectFault
	Relay                       *testutil.R5SMTPServer
	Outbound                    *outbound.Service
	SchemaVersion               int64
	APIKeyScopeConstraintSHA256 string
	mu                          sync.Mutex
	trace                       []R5HTTPObservation
	tokens                      map[string]string
	secret                      string
}

// NewR5HTTPFixture supplies signed current JWTs but never response-mocks the
// router, middleware, Redis cache, PostgreSQL, object port, or relay transport.
func NewR5HTTPFixture(t *testing.T) *R5HTTPFixture {
	t.Helper()
	f := &R5HTTPFixture{R5Fixture: NewR5Fixture(t), tokens: map[string]string{}, secret: uuid.NewString()}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	var definition string
	if err := f.Pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&f.SchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := f.Pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='tenant_api_keys_scopes_check' AND conrelid='tenant_api_keys'::regclass`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(definition))
	f.APIKeyScopeConstraintSHA256 = hex.EncodeToString(sum[:])
	f.Objects = testutil.NewR5ObjectFault(testutil.NewMemoryObjectStore())
	relay, err := testutil.StartR5SMTP(ctx, testutil.R5SMTPBehavior{})
	if err != nil {
		t.Fatal(err)
	}
	f.Relay = relay
	t.Cleanup(func() { _ = relay.Close() })
	cfg := relay.Config()
	cfg.PollInterval = 20 * time.Millisecond
	cfg.BatchSize = 1
	f.Outbound = outbound.NewService(cfg, f.Store, f.Store, zerolog.Nop())
	f.Outbound.SetObjectStore(f.Objects)
	t.Cleanup(f.Outbound.Stop)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := api.NewRouter(api.RouterConfig{Store: f.Store, CompanyRepository: f.Store, ObjectStore: f.Objects, RawObjects: rawobject.NewStore(f.Objects, f.Store), JWTSecret: f.secret, MailboxTokenSecret: uuid.NewString(), PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, f.Store, 10000, nil), OutboundService: f.Outbound, Logger: zerolog.Nop(), Readiness: f.Store.Readiness})
	f.Server = httptest.NewServer(router)
	t.Cleanup(f.Server.Close)
	for i, c := range f.Companies {
		token, e := authn.IssueAccessToken(f.secret, c.Admin)
		if e != nil {
			t.Fatal(e)
		}
		f.tokens[r5Identity(i, "admin")] = token
		for role, user := range c.Users {
			token, e = authn.IssueAccessToken(f.secret, user)
			if e != nil {
				t.Fatal(e)
			}
			f.tokens[r5Identity(i, role)] = token
		}
	}
	t.Cleanup(func() { clear(f.tokens); f.secret = "" })
	return f
}
func (f *R5HTTPFixture) RefreshJWT(t *testing.T, company int, role string) {
	t.Helper()
	var id uuid.UUID
	if role == "admin" {
		id = f.Companies[company].Admin.ID
	} else {
		user := f.Companies[company].Users[role]
		if user == nil {
			t.Fatal("unknown fixture JWT role")
		}
		id = user.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	user, err := f.Store.GetUser(ctx, id)
	if err != nil || user == nil {
		t.Fatal("actual current JWT principal unavailable")
	}
	token, err := authn.IssueAccessToken(f.secret, user)
	if err != nil {
		t.Fatal(err)
	}
	f.tokens[r5Identity(company, role)] = token
}

func r5Identity(company int, role string) string { return string(rune('A'+company)) + ":" + role }
func (f *R5HTTPFixture) JWT(company int, role string) string {
	return f.tokens[r5Identity(company, role)]
}
func (f *R5HTTPFixture) Observations() []R5HTTPObservation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]R5HTTPObservation(nil), f.trace...)
}

// Request invokes actual HTTP and returns private response bytes only to the
// caller test. Trace stores no header/token/payload; test diagnostics must not
// print response bytes from credential creation.
func (f *R5HTTPFixture) Request(t *testing.T, ctx context.Context, token, method, path string, body any, key string) (int, []byte) {
	t.Helper()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("R5 HTTP request requires a hard deadline")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, method, f.Server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		// Production auth.go has separate transport layers: API keys are not JWT Bearer tokens.
		if strings.HasPrefix(token, "tb_") {
			request.Header.Set("X-API-Key", token)
		} else {
			request.Header.Set("Authorization", "Bearer "+token)
		}
	}
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := f.Server.Client().Do(request)
	if err != nil {
		t.Fatal("actual fixture HTTP transport failed", err)
	}
	defer response.Body.Close()
	value, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(value)
	var envelope struct{ Error struct{ Code string } }
	_ = json.Unmarshal(value, &envelope)
	f.mu.Lock()
	f.trace = append(f.trace, R5HTTPObservation{Method: method, Path: request.URL.Path, Status: response.StatusCode, ResponseSHA256: hex.EncodeToString(digest[:]), ErrorCode: envelope.Error.Code})
	f.mu.Unlock()
	return response.StatusCode, value
}
