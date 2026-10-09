package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

type r5AdminStreamReader struct {
	events          []models.OutboxEvent
	reads, accesses int
	fenced          bool
	afterRead       func()
}

func (s *r5AdminStreamReader) ListCompanyAdminEvents(_ context.Context, _ authz.Actor, limit int) ([]models.OutboxEvent, error) {
	s.reads++
	if limit != 200 {
		panic("unbounded event read")
	}
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.events, nil
}

func (s *r5AdminStreamReader) WithCompanyAdminEventAccess(ctx context.Context, _ authz.Actor, emit func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.accesses++
	s.fenced = true
	defer func() { s.fenced = false }()
	return emit()
}

// The pure writer exposes the same ResponseController ports as net/http's
// production writer. It checks that actual flush (not just buffered Write)
// remains inside the one-frame authority fence.
type r5AdminStreamWriter struct {
	*httptest.ResponseRecorder
	reader      *r5AdminStreamReader
	deadline    time.Time
	flushes     int
	cancelAfter int
	cancel      context.CancelFunc
	slow        bool
}

func (w *r5AdminStreamWriter) SetWriteDeadline(deadline time.Time) error {
	if !w.reader.fenced || time.Until(deadline) > 2100*time.Millisecond {
		panic("frame deadline outside bounded authority fence")
	}
	w.deadline = deadline
	return nil
}

func (w *r5AdminStreamWriter) Write(data []byte) (int, error) {
	if !w.reader.fenced {
		// HTTP pre-stream errors are not privileged event releases.
		if strings.Contains(string(data), "event:") {
			panic("event write escaped authority fence")
		}
	}
	return w.ResponseRecorder.Write(data)
}

func (w *r5AdminStreamWriter) FlushError() error {
	if !w.reader.fenced {
		panic("flush escaped authority fence")
	}
	w.flushes++
	if w.slow {
		time.Sleep(time.Until(w.deadline))
		return errors.New("controlled writer deadline exceeded")
	}
	w.ResponseRecorder.Flush()
	if w.flushes == w.cancelAfter {
		w.cancel()
	}
	return nil
}

func r5AdminStreamPayload(tenant uuid.UUID) string {
	return `{"type":"company.admin.changed","tenant_id":"` + tenant.String() + `","occurred_at":"2026-10-02T00:00:00Z","metadata":{"action":"permission.profile.update","resource_type":"permission_profile","resource_id":"` + uuid.NewString() + `","activation_token":"PRIVATE_SENTINEL"},"bcc":["PRIVATE_SENTINEL"],"object_key":"PRIVATE_SENTINEL","subject":"PRIVATE_SENTINEL"}`
}

// Low-load transport tests use the shipping Auth/RevalidateRequest and handler,
// but deliberately make no PostgreSQL/runtime coverage claim.
func TestR5CompanyAdminStreamUnitContract(t *testing.T) {
	for _, mode := range []string{"allowed", "duplicate", "cursor", "invalid-cursor", "foreign", "unknown-action", "mismatched-resource", "personal-state", "invalid-id", "nil-revalidation", "reader", "key", "frozen", "revoked-before-read", "read-to-write-revoked", "flush-timeout", "cancel-after-read"} {
		t.Run(mode, func(t *testing.T) {
			st := testutil.NewFakeStore()
			tenant := &models.Tenant{ID: uuid.New(), Name: "stream company"}
			st.SeedTenant(tenant)
			user := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "admin@stream.test", Role: models.RoleAdmin, IsActive: true}
			if mode == "reader" {
				user.Role = models.RoleUser
			}
			if mode == "frozen" {
				user.IsActive = false
			}
			if err := st.CreateUser(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			token, err := authn.IssueAccessToken("stream-test-secret", user)
			if err != nil {
				t.Fatal(err)
			}
			payload := r5AdminStreamPayload(tenant.ID)
			switch mode {
			case "foreign":
				payload = r5AdminStreamPayload(uuid.New())
			case "unknown-action":
				payload = strings.ReplaceAll(payload, "permission.profile.update", "PRIVATE_SENTINEL")
			case "mismatched-resource":
				payload = strings.ReplaceAll(payload, `"resource_type":"permission_profile"`, `"resource_type":"user"`)
			case "personal-state":
				payload = strings.ReplaceAll(payload, "permission.profile.update", "message.star")
			case "invalid-id":
				payload = strings.ReplaceAll(payload, `"resource_id":"`, `"resource_id":"invalid-`)
			}
			id := uuid.New()
			reader := &r5AdminStreamReader{events: []models.OutboxEvent{{ID: id, EventType: "company.admin.changed", Payload: json.RawMessage(payload)}}}
			if mode == "duplicate" {
				reader.events = append(reader.events, reader.events[0])
			}
			refresh := func(r *http.Request) (*http.Request, error) {
				if mode == "revoked-before-read" {
					current := *user
					current.Role = models.RoleUser
					if err := st.UpdateUser(r.Context(), &current); err != nil {
						return nil, err
					}
				}
				return middleware.RevalidateRequest(r, st, "stream-test-secret", tenant.ID.String())
			}
			if mode == "nil-revalidation" {
				refresh = nil
			}
			h := handlers.NewCompanyAdminEventHandler(reader, refresh, zerolog.Nop())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "read-to-write-revoked" {
				reader.afterRead = func() {
					current := *user
					current.Role = models.RoleUser
					if err := st.UpdateUser(context.Background(), &current); err != nil {
						t.Fatal(err)
					}
				}
			} else if mode == "cancel-after-read" {
				reader.afterRead = cancel
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/company/events", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+token)
			if mode == "key" {
				st.RegisterAPIKey("stream-key", tenant, []string{"domains:read"})
				req.Header.Del("Authorization")
				req.Header.Set("X-API-Key", "stream-key")
			}
			if mode == "cursor" {
				req.Header.Set("Last-Event-ID", id.String())
			} else if mode == "invalid-cursor" {
				req.Header.Set("Last-Event-ID", "18446744073709551616")
			}
			frames := 2
			if mode == "allowed" || mode == "duplicate" || mode == "cursor" || mode == "invalid-cursor" {
				frames = 3
			}
			w := &r5AdminStreamWriter{ResponseRecorder: httptest.NewRecorder(), reader: reader, cancelAfter: frames, cancel: cancel, slow: mode == "flush-timeout"}
			started := time.Now()
			middleware.Auth(st, "stream-test-secret", tenant.ID.String())(middleware.RequireAuth(middleware.RequireAdmin(http.HandlerFunc(h.Events)))).ServeHTTP(w, req)
			body := w.Body.String()
			if reader.fenced {
				t.Fatal("stream exit leaked frame authority fence")
			}
			if mode == "read-to-write-revoked" || mode == "cancel-after-read" {
				if reader.reads != 1 || reader.accesses != 0 || strings.Contains(body, "event:") {
					t.Fatal("revoked/cancelled read batch released a frame", body)
				}
				return
			}
			if mode == "flush-timeout" {
				if reader.reads != 1 || reader.accesses != 1 || w.flushes != 1 || time.Since(started) > 3*time.Second || strings.Contains(body, "company.admin.changed") {
					t.Fatal("slow flush continued old batch or retained fence")
				}
				return
			}
			denied := mode == "reader" || mode == "key" || mode == "frozen" || mode == "nil-revalidation" || mode == "revoked-before-read"
			if denied {
				if reader.reads != 0 || strings.Contains(body, "event: ready") || w.Code < 400 {
					t.Fatalf("stale/noninteractive authority read stream: status=%d reads=%d", w.Code, reader.reads)
				}
				return
			}
			if reader.reads != 1 || strings.Contains(body, "PRIVATE_SENTINEL") || !strings.Contains(body, "event: resync\ndata: {\"tenant_id\":\""+tenant.ID.String()+"\"}") {
				t.Fatal("projection/reconnect snapshot contract broken", body)
			}
			want := 0
			if mode == "allowed" || mode == "duplicate" || mode == "cursor" || mode == "invalid-cursor" {
				want = 1
			}
			if strings.Count(body, "event: company.admin.changed") != want || (want == 1 && !strings.Contains(body, "id: "+id.String())) {
				t.Fatal("durable ID or allow-list broken", body)
			}
			if w.Header().Get("Cache-Control") != "private, no-store, no-transform" {
				t.Fatal("stream is cacheable")
			}
		})
	}
}

type r5AdminSSEFrame struct{ id, name, data string }

func r5AdminReadFrame(t *testing.T, scanner *bufio.Scanner) r5AdminSSEFrame {
	t.Helper()
	frame, ok := r5AdminNextFrame(t, scanner)
	if !ok {
		t.Fatal("SSE frame missing before clean EOF")
	}
	return frame
}

// Parse whole frames; a buffered id line is not evidence of a newly released
// post-commit event. Partial frames, duplicate fields and timeout are failures.
func r5AdminNextFrame(t *testing.T, scanner *bufio.Scanner) (r5AdminSSEFrame, bool) {
	t.Helper()
	var frame r5AdminSSEFrame
	fields := map[string]bool{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if frame.name == "" || !fields["data"] {
				t.Fatal("empty/incomplete SSE frame")
			}
			return frame, true
		}
		name, value, found := strings.Cut(line, ": ")
		if !found || fields[name] {
			t.Fatal("malformed/duplicate SSE frame field", line)
		}
		fields[name] = true
		switch name {
		case "id":
			frame.id = value
		case "event":
			frame.name = value
		case "data":
			frame.data = value
		default:
			t.Fatal("unknown SSE frame field", name)
		}
	}
	if scanner.Err() != nil || len(fields) != 0 {
		t.Fatalf("SSE did not end with clean frame boundary EOF: %v partial=%v", scanner.Err(), fields)
	}
	return frame, false
}

// Actual shipping router+JWT+selected tenant+PgStore, not a replacement route.
func TestR5CompanyAdminStreamHTTPDurableScopeReconnectAndRevocation(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("company admin SSE runtime requires owned TABMAIL_TEST_DB_DSN; do not count a skip as evidence")
	}
	f := testpg.NewR5HTTPFixture(t)
	super := r5PermissionAuthorityHTTPSuper(t, f)
	a, b := f.Companies[0], f.Companies[1]
	id := uuid.New()
	if _, err := f.Pool.Exec(context.Background(), `INSERT INTO outbox_events(id,event_type,payload,state) VALUES($1,'company.admin.changed',$2,'done'),($3,'company.admin.changed',$4,'pending')`, id, r5AdminStreamPayload(a.Tenant.ID), uuid.New(), r5AdminStreamPayload(b.Tenant.ID)); err != nil {
		t.Fatal(err)
	}
	open := func(token string, tenant uuid.UUID, last string) (*http.Response, *bufio.Scanner, context.CancelFunc) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Server.URL+"/api/v1/company/events", nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", tenant.String())
		req.Header.Set("Last-Event-ID", last)
		res, err := f.Server.Client().Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
			cancel()
			res.Body.Close()
			t.Fatalf("shipping stream status=%d", res.StatusCode)
		}
		return res, bufio.NewScanner(res.Body), cancel
	}
	for _, last := range []string{"", id.String(), "obsolete-or-invalid"} {
		res, scanner, cancel := open(f.JWT(0, "reader"), a.Tenant.ID, last)
		ready, resync, event := r5AdminReadFrame(t, scanner), r5AdminReadFrame(t, scanner), r5AdminReadFrame(t, scanner)
		if ready.name != "ready" || resync.name != "resync" || ready.data != `{"tenant_id":"`+a.Tenant.ID.String()+`"}` || event.name != "company.admin.changed" || event.id != id.String() || strings.Contains(event.data, "PRIVATE_SENTINEL") || strings.Contains(event.data, b.Tenant.ID.String()) {
			t.Fatal("shipping scope/reconnect projection failed", ready, resync, event)
		}
		cancel()
		res.Body.Close()
	}
	// The fixture's ConfigureCompany/invitation/activation/grant producers have
	// already populated the outbox. Add one explicitly synthetic oldest row as
	// the last frame of this <200-row initial window, then consume the ENTIRE
	// known window to its release boundary before committing revocation.
	barrier := uuid.New()
	if _, err := f.Pool.Exec(context.Background(), `INSERT INTO outbox_events(id,event_type,payload,created_at) VALUES($1,'company.admin.changed',$2,'1970-01-01T00:00:00Z')`, barrier, r5AdminStreamPayload(a.Tenant.ID)); err != nil {
		t.Fatal(err)
	}
	rows, err := f.Pool.Query(context.Background(), `SELECT id FROM outbox_events WHERE event_type='company.admin.changed' AND payload->>'tenant_id'=$1 ORDER BY created_at DESC,id DESC`, a.Tenant.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	initialIDs := map[string]bool{}
	lastInitial := ""
	for rows.Next() {
		var eventID uuid.UUID
		if err := rows.Scan(&eventID); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		lastInitial = eventID.String()
		initialIDs[lastInitial] = true
	}
	rows.Close()
	if rows.Err() != nil || len(initialIDs) >= 200 || len(initialIDs) < 2 || lastInitial != barrier.String() {
		t.Fatalf("initial release barrier is not the complete bounded window: rows=%d last=%s err=%v", len(initialIDs), lastInitial, rows.Err())
	}
	// Established management streams must not keep handshake authority.
	res, scanner, cancel := open(f.JWT(0, "admin"), a.Tenant.ID, "")
	defer cancel()
	defer res.Body.Close()
	ready, resync := r5AdminReadFrame(t, scanner), r5AdminReadFrame(t, scanner)
	if ready.name != "ready" || resync.name != "resync" || ready.id != "" || resync.id != "" || ready.data != `{"tenant_id":"`+a.Tenant.ID.String()+`"}` || resync.data != ready.data {
		t.Fatal("revocation stream control scope invalid", ready, resync)
	}
	consumed := map[string]bool{}
	for {
		frame := r5AdminReadFrame(t, scanner)
		if frame.name != "company.admin.changed" || !initialIDs[frame.id] || consumed[frame.id] || strings.Contains(frame.data, "PRIVATE_SENTINEL") {
			t.Fatal("initial window released unknown/duplicate/private frame", frame)
		}
		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		if err := json.Unmarshal([]byte(frame.data), &payload); err != nil || payload.TenantID != a.Tenant.ID.String() {
			t.Fatal("initial window released wrong tenant", frame)
		}
		consumed[frame.id] = true
		if frame.id == barrier.String() {
			break
		}
	}
	if len(consumed) != len(initialIDs) {
		t.Fatalf("last release barrier arrived before complete known initial window: got=%d want=%d", len(consumed), len(initialIDs))
	}
	t.Logf("completed pre-revocation initial release window: frames=%d synthetic_last_barrier=%s", len(consumed), barrier)
	role := models.RoleUser
	if _, err := f.Store.UpdateUserGuarded(context.Background(), super, a.Tenant.ID, a.Admin.ID, models.UserAdminPatch{Role: &role}); err != nil {
		t.Fatal(err)
	}
	// UpdateUserGuarded has now returned after its actual commit. A CURRENT
	// super administrator produces a NEW durable marker via the real company
	// mutation, not a test INSERT masquerading as producer-to-SSE evidence.
	settings, err := f.Store.GetCompanySettings(context.Background(), a.Tenant.ID)
	if err != nil || settings == nil {
		t.Fatal("post-commit authoritative settings missing", err)
	}
	settings.Name = "Post-revocation marker " + uuid.NewString()
	if _, err := f.Store.ConfigureCompany(context.Background(), super, *settings); err != nil {
		t.Fatal("post-commit real mutation failed", err)
	}
	rows, err = f.Pool.Query(context.Background(), `SELECT id,payload FROM outbox_events WHERE event_type='company.admin.changed' AND payload->>'tenant_id'=$1`, a.Tenant.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	marker := ""
	for rows.Next() {
		var eventID uuid.UUID
		var raw []byte
		if err := rows.Scan(&eventID, &raw); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if initialIDs[eventID.String()] {
			continue
		}
		var payload struct {
			TenantID string            `json:"tenant_id"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if marker != "" || payload.TenantID != a.Tenant.ID.String() || payload.Metadata["action"] != "company.configure" || payload.Metadata["resource_type"] != "company" || payload.Metadata["resource_id"] != a.Tenant.ID.String() || len(payload.Metadata) != 3 {
			rows.Close()
			t.Fatal("post-commit producer did not create exactly one original minimal marker", string(raw))
		}
		marker = eventID.String()
	}
	rows.Close()
	if rows.Err() != nil || marker == "" {
		t.Fatal("unique post-commit real producer marker missing", rows.Err())
	}
	t.Logf("revocation committed; current super ConfigureCompany produced original durable marker=%s", marker)
	for {
		frame, more := r5AdminNextFrame(t, scanner)
		if !more {
			break // Only a clean EOF, never a timeout or partial frame, succeeds.
		}
		if frame.id == marker || !initialIDs[frame.id] || consumed[frame.id] {
			t.Fatal("revoked stream released post-commit/unknown/duplicate frame", frame)
		}
		// Every initial ID was consumed through the explicit last release
		// barrier. Therefore ANY later frame is a failure, not a permissive
		// drain that silently reclassifies a newly released event as buffered.
		t.Fatal("revoked stream released a frame beyond its completed initial window", frame)
	}
	// A new CURRENT authorized stream must deliver that exact real producer ID;
	// only the old revoked stream is stopped. This is producer->shipping SSE,
	// not merely a passive SQL observation of a synthetic marker.
	freshRes, freshScanner, freshCancel := open(f.JWT(0, "reader"), a.Tenant.ID, "")
	freshReady, freshResync, freshEvent := r5AdminReadFrame(t, freshScanner), r5AdminReadFrame(t, freshScanner), r5AdminReadFrame(t, freshScanner)
	freshCancel()
	freshRes.Body.Close()
	if freshReady.name != "ready" || freshResync.name != "resync" || freshEvent.name != "company.admin.changed" || freshEvent.id != marker || strings.Contains(freshEvent.data, settings.Name) {
		t.Fatal("current authorized stream did not deliver original minimal real producer marker", freshEvent)
	}
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {f.JWT(0, "sender"), 403}, {f.JWT(0, "frozen"), 401}, {f.JWT(0, "admin"), 401}} {
		r5PermissionAuthorityHTTPCall(t, f, tc.token, http.MethodGet, "/api/v1/company/events", "", uuid.Nil, tc.want)
	}
	// The stream never changed dispatcher state/attempts or produced another row.
	var state string
	var attempts int
	if err := f.Pool.QueryRow(context.Background(), `SELECT state,attempts FROM outbox_events WHERE id=$1`, id).Scan(&state, &attempts); err != nil || state != "done" || attempts != 0 {
		t.Fatal("SSE changed original outbox ownership", state, attempts, err)
	}
}
