package api_test

import (
	"context"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

const r5ReceivedCanary = "RECEIVED-PRIVATE-CONTENT-CANARY"

func r5ReceivedHTTPMessage(t *testing.T, f *testpg.R5HTTPFixture) (*models.Message, string) {
	t.Helper()
	ctx := context.Background()
	c := f.Companies[0]
	_, err := f.Pool.Exec(ctx, `UPDATE mailboxes SET retention_hours_override=24 WHERE id=$1`, c.Shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: sender@fixture.test\r\nTo: shared@fixture-a.test\r\nSubject: " + r5ReceivedCanary + "\r\nMessage-ID: <received-fixture@fixture.test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\n" + r5ReceivedCanary + "\r\n--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=private.txt\r\n\r\n" + r5ReceivedCanary + "\r\n--b--\r\n"
	m := &models.Message{TenantID: c.Tenant.ID, MailboxID: c.Shared.ID, ZoneID: c.Zone.ID, Sender: "sender@fixture.test", Recipients: []string{c.Shared.FullAddress}, Subject: r5ReceivedCanary, RawObjectKey: "received-http-" + uuid.NewString(), Size: int64(len(raw))}
	if err = f.Store.CreateMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err = f.Objects.Put(ctx, m.RawObjectKey, strings.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	doc, err := mailcontent.New(f.Objects).Document(ctx, m.ID, m.RawObjectKey)
	if err != nil || len(doc.Parts) != 1 {
		t.Fatalf("MIME fixture: %v", err)
	}
	if err = f.Store.SaveParsedMessage(ctx, c.UserActor("reader"), c.Shared.ID, *doc); err != nil {
		t.Fatal(err)
	}
	return m, doc.Parts[0].ID
}
func r5ReceivedHTTPGet(t *testing.T, f *testpg.R5HTTPFixture, token, path string, want int) []byte {
	t.Helper()
	body := r5PermissionAuthorityHTTPCall(t, f, token, http.MethodGet, path, "", uuid.Nil, want)
	if want >= 400 {
		var v map[string]json.RawMessage
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		if d, ok := v["data"]; ok && string(d) != "null" {
			t.Fatal("denial returned content payload")
		}
		if strings.Contains(string(body), r5ReceivedCanary) || strings.Contains(string(body), "private.txt") {
			t.Fatal("denial leaked content")
		}
	}
	return body
}
func r5ReceivedHTTPPaths(m *models.Message, part string) []string {
	root := "/api/v1/company/mailboxes/" + m.MailboxID.String() + "/messages/" + m.ID.String()
	return []string{root, root + "/source", root + "/attachments", root + "/attachments/0", root + "/parts/" + part, root + "/conversation"}
}
func TestR5ReceivedContentHTTPExpiryAndWarmCache(t *testing.T) {
	for _, cutoff := range []string{"expiry", "purge", "archive-expiry", "trash-expiry"} {
		t.Run(cutoff, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			m, part := r5ReceivedHTTPMessage(t, f)
			token := f.JWT(0, "reader")
			for _, path := range r5ReceivedHTTPPaths(m, part) {
				r5ReceivedHTTPGet(t, f, token, path, 200)
			}
			sql := `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`
			switch cutoff {
			case "purge":
				sql = `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp(),expires_at=NULL WHERE id=$1`
			case "archive-expiry":
				sql = `UPDATE messages SET archived_at=clock_timestamp(),expires_at=clock_timestamp() WHERE id=$1`
			case "trash-expiry":
				sql = `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '1 day',expires_at=clock_timestamp() WHERE id=$1`
			}
			if _, err := f.Pool.Exec(context.Background(), sql, m.ID); err != nil {
				t.Fatal(err)
			}
			before := len(f.Objects.Calls())
			for _, path := range r5ReceivedHTTPPaths(m, part) {
				r5ReceivedHTTPGet(t, f, token, path, 404)
			}
			root := r5ReceivedHTTPPaths(m, part)[0]
			bodyCompose := r5PermissionAuthorityHTTPCall(t, f, f.JWT(0, "sender"), http.MethodPost, root+"/compose", `{"mode":"reply","from_mailbox_id":"`+m.MailboxID.String()+`"}`, uuid.Nil, 404)
			if strings.Contains(string(bodyCompose), r5ReceivedCanary) {
				t.Fatal("expired compose returned source content")
			}

			base := "/api/v1/company/mailboxes/" + m.MailboxID.String()
			for _, folder := range []string{"inbox", "archive", "trash", "all"} {
				body := r5ReceivedHTTPGet(t, f, token, base+"/messages?folder="+folder+"&q="+r5ReceivedCanary, 200)
				var v struct {
					Data []json.RawMessage `json:"data"`
					Meta struct {
						Total int `json:"total"`
					} `json:"meta"`
				}
				if err := json.Unmarshal(body, &v); err != nil {
					t.Fatal(err)
				}
				if len(v.Data) != 0 || v.Meta.Total != 0 || strings.Contains(string(body), r5ReceivedCanary) {
					t.Fatal("list/search/count exposed expired summary")
				}
			}
			body := r5ReceivedHTTPGet(t, f, token, base+"/index-status", 200)
			var v struct {
				Data company.ContentIndexStatus `json:"data"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			if v.Data.Total != 0 || v.Data.Indexed != 0 {
				t.Fatal("index summary exposed expired content")
			}
			if len(f.Objects.Calls()) != before {
				t.Fatal("eligibility denial performed object I/O")
			}
		})
	}
}
func TestR5ReceivedContentHTTPCurrentAuthorityAndResourceBinding(t *testing.T) {
	for _, axis := range []string{"frozen", "epoch", "grant", "none", "domain", "foreign", "wrong-mailbox", "id-reuse", "db-error"} {
		t.Run(axis, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			m, part := r5ReceivedHTTPMessage(t, f)
			c := f.Companies[0]
			token := f.JWT(0, "reader")
			want := 403
			ctx := context.Background()
			r5ReceivedHTTPGet(t, f, token, r5ReceivedHTTPPaths(m, part)[0], 200)
			var err error
			switch axis {
			case "frozen":
				_, err = f.Pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, c.Users["reader"].ID)
				want = 401
			case "epoch":
				_, err = f.Pool.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, c.Users["reader"].ID)
				want = 401
			case "grant":
				_, err = f.Pool.Exec(ctx, `UPDATE mailbox_grants SET can_read=false WHERE mailbox_id=$1 AND user_id=$2`, m.MailboxID, c.Users["reader"].ID)
			case "none", "domain":
				mode := "none"
				ids := []uuid.UUID{}
				if axis == "domain" {
					mode = "list"
					ids = []uuid.UUID{f.Companies[1].Zone.ID}
				}
				_, err = f.Pool.Exec(ctx, `INSERT INTO user_permission_overrides(user_id,domain_access_mode,allowed_zone_ids) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET domain_access_mode=EXCLUDED.domain_access_mode,allowed_zone_ids=EXCLUDED.allowed_zone_ids`, c.Users["reader"].ID, mode, ids)
			case "foreign":
				token = f.JWT(1, "reader")
				want = 404
			case "wrong-mailbox":
				m.MailboxID = c.Personal["reader"].ID
				want = 404
			case "id-reuse":
				_, err = f.Pool.Exec(ctx, `DELETE FROM messages WHERE id=$1`, m.ID)
				if err == nil {
					replacement := *m
					replacement.MailboxID = c.Personal["reader"].ID
					err = f.Store.CreateMessage(ctx, &replacement)
				}
				want = 404
			case "db-error":
				_, err = f.Pool.Exec(ctx, `ALTER TABLE messages RENAME TO injected_received_failure`)
				want = 500
			}
			if err != nil {
				t.Fatal(err)
			}
			before := len(f.Objects.Calls())
			for _, path := range r5ReceivedHTTPPaths(m, part) {
				r5ReceivedHTTPGet(t, f, token, path, want)
			}
			listWant := want
			if axis == "id-reuse" || axis == "wrong-mailbox" {
				listWant = 200
			}
			base := "/api/v1/company/mailboxes/" + m.MailboxID.String()
			for _, suffix := range []string{"/messages?q=" + r5ReceivedCanary, "/index-status"} {
				body := r5ReceivedHTTPGet(t, f, token, base+suffix, listWant)
				if strings.Contains(string(body), r5ReceivedCanary) {
					t.Fatal("authority denied content exposed list/index summary")
				}
			}
			if len(f.Objects.Calls()) != before {
				t.Fatal("authority denial performed object I/O")
			}
		})
	}
}

func TestR5ReceivedContentHTTPDeadlineAfterMailboxWait(t *testing.T) {
	for _, endpoint := range []string{"detail", "source", "list", "conversation", "summary"} {
		t.Run(endpoint, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			m, part := r5ReceivedHTTPMessage(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var deadline time.Time
			if err := f.Pool.QueryRow(ctx, `UPDATE messages SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, m.ID).Scan(&deadline); err != nil {
				t.Fatal(err)
			}
			hold, err := f.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback(context.Background())
			if _, err = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, m.MailboxID); err != nil {
				t.Fatal(err)
			}
			base := "/api/v1/company/mailboxes/" + m.MailboxID.String()
			path := r5ReceivedHTTPPaths(m, part)[0]
			want := 404
			switch endpoint {
			case "source":
				path += "/source"
			case "list":
				path = base + "/messages?q=" + r5ReceivedCanary
				want = 200
			case "conversation":
				path += "/conversation"
			case "summary":
				path = base + "/index-status"
				want = 200
			}
			done := make(chan r5PermissionAuthorityHTTPResult, 1)
			go func() {
				done <- r5PermissionAuthorityHTTPRequest(ctx, f, f.JWT(0, "reader"), http.MethodGet, path, "", uuid.Nil)
			}()
			r5InspectionHTTPWait(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
			var beganBefore bool
			if err = f.Pool.QueryRow(ctx, `SELECT bool_and(xact_start<$2) FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid))`, int32(hold.Conn().PgConn().PID()), deadline).Scan(&beganBefore); err != nil {
				t.Fatal(err)
			}
			if !beganBefore {
				t.Fatal("HTTP operation began after deadline")
			}
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				var expired bool
				if err = f.Pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired); err != nil {
					t.Fatal(err)
				}
				if expired {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-tick.C:
				}
			}
			if err = hold.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				body := r5PermissionAuthorityHTTPStatus(t, result, want)
				if strings.Contains(string(body), r5ReceivedCanary) {
					t.Fatal("HTTP lock-wait released expired content")
				}
				if want == 200 {
					var v struct {
						Data json.RawMessage `json:"data"`
						Meta struct {
							Total int `json:"total"`
						} `json:"meta"`
					}
					if err = json.Unmarshal(body, &v); err != nil {
						t.Fatal(err)
					}
					if endpoint == "summary" {
						var s company.ContentIndexStatus
						if err = json.Unmarshal(v.Data, &s); err != nil {
							t.Fatal(err)
						}
						if s.Total != 0 || s.Indexed != 0 {
							t.Fatal("HTTP lock-wait leaked expired summary")
						}
					} else if string(v.Data) != "[]" || v.Meta.Total != 0 {
						t.Fatal("HTTP lock-wait leaked expired count")
					}
				}
			case <-ctx.Done():
				t.Fatal("HTTP lock-wait did not finish")
			}
		})
	}
}

// Keep the shipping api.NewRouter/service/handler and PostgreSQL. Only the
// object port has a deterministic Read callback, so invalidation happens after
// Source's object-open guard but before terminal empty EOF/success headers.
type r5ReceivedEmptyEOFObjects struct {
	*testutil.R5ObjectFault
	onRead func()
}

func (o *r5ReceivedEmptyEOFObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, e := o.R5ObjectFault.Get(ctx, key)
	if e != nil {
		return nil, e
	}
	return &r5ReceivedEmptyEOFReader{ReadCloser: r, onRead: o.onRead}, nil
}

type r5ReceivedEmptyEOFReader struct {
	io.ReadCloser
	onRead func()
	once   sync.Once
}

func (r *r5ReceivedEmptyEOFReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		if r.onRead != nil {
			r.onRead()
		}
	})
	return r.ReadCloser.Read(p)
}
func TestR5ReceivedContentHTTPEmptyEOFRechecksAuthority(t *testing.T) {
	for _, axis := range []string{"active", "message-expiry", "mailbox-expiry", "grant", "db-error"} {
		t.Run(axis, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			m, _ := r5ReceivedHTTPMessage(t, f)
			c := f.Companies[0]
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if err := f.Objects.Put(ctx, m.RawObjectKey, strings.NewReader(""), 0); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Pool.Exec(ctx, `UPDATE messages SET size=0 WHERE id=$1`, m.ID); err != nil {
				t.Fatal(err)
			}
			objects := &r5ReceivedEmptyEOFObjects{R5ObjectFault: f.Objects}
			fired := atomic.Bool{}
			mutation := make(chan error, 1)
			objects.onRead = func() {
				fired.Store(true)
				var err error
				switch axis {
				case "message-expiry":
					_, err = f.Pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1`, m.ID)
				case "mailbox-expiry":
					_, err = f.Pool.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, m.MailboxID)
				case "grant":
					_, err = f.Pool.Exec(ctx, `UPDATE mailbox_grants SET can_read=false WHERE mailbox_id=$1 AND user_id=$2`, m.MailboxID, c.Users["reader"].ID)
				case "db-error":
					_, err = f.Pool.Exec(ctx, `ALTER TABLE messages RENAME TO injected_empty_source_failure`)
				}
				mutation <- err
			}
			mr := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { redisClient.Close() })
			secret := uuid.NewString()
			router := api.NewRouter(api.RouterConfig{Store: f.Store, CompanyRepository: f.Store, ObjectStore: objects, JWTSecret: secret, MailboxTokenSecret: uuid.NewString(), PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(redisClient, f.Store, 10000, nil), Logger: zerolog.Nop()})
			server := httptest.NewServer(router)
			defer server.Close()
			token, err := authn.IssueAccessToken(secret, c.Users["reader"])
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+r5ReceivedHTTPPaths(m, "")[0]+"/source", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !fired.Load() {
				t.Fatal("shipping source did not reach deferred EOF boundary")
			}
			select {
			case err := <-mutation:
				if err != nil {
					t.Fatalf("EOF invalidation fixture failed: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("EOF invalidation did not complete")
			}

			want := 404
			switch axis {
			case "active":
				want = 200
			case "grant":
				want = 403
			case "db-error":
				want = 500
			}
			if res.StatusCode != want {
				t.Fatalf("empty EOF status=%d want=%d", res.StatusCode, want)
			}
			if axis == "active" {
				if len(body) != 0 {
					t.Fatal("active empty object returned bytes")
				}
				return
			}
			if strings.Contains(res.Header.Get("Content-Type"), "message/rfc822") || res.Header.Get("Content-Disposition") != "" {
				t.Fatal("empty denied source committed success download headers")
			}
			var envelope map[string]json.RawMessage
			if err = json.Unmarshal(body, &envelope); err != nil {
				t.Fatal(err)
			}
			if data, ok := envelope["data"]; ok && string(data) != "null" {
				t.Fatal("empty EOF denial returned content payload")
			}
		})
	}
}
