package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func TestR5CompanyAdminEventsOriginalOutboxCurrentScope(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("company admin event runtime requires owned TABMAIL_TEST_DB_DSN; do not count a skip as evidence")
	}
	f := seedCompany(t)
	ctx := context.Background()
	ids := map[uuid.UUID]bool{}
	for _, state := range []string{"pending", "processing", "retry", "done"} {
		id := uuid.New()
		ids[id] = true
		payload := `{"type":"company.admin.changed","tenant_id":"` + f.tenant.ID.String() + `","occurred_at":"2026-10-02T00:00:00Z","metadata":{"action":"permission.profile.update","resource_type":"permission_profile","resource_id":"` + uuid.NewString() + `","token":"PRIVATE_STREAM_SENTINEL"},"bcc":["PRIVATE_STREAM_SENTINEL"],"object_key":"PRIVATE_STREAM_SENTINEL"}`
		_, err := f.pool.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload,state) VALUES($1,'company.admin.changed',$2,$3)`, id, payload, state)
		must(t, err)
	}
	foreign := uuid.New()
	_, err := f.pool.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload) VALUES($1,'company.admin.changed',$2),($3,'message.received',$4)`, foreign, `{"tenant_id":"`+uuid.NewString()+`","metadata":{"secret":"PRIVATE_STREAM_SENTINEL"}}`, uuid.New(), `{"tenant_id":"`+f.tenant.ID.String()+`"}`)
	must(t, err)
	before := ""
	must(t, f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM outbox_events o`).Scan(&before))
	for i := 0; i < 2; i++ {
		events, err := f.st.ListCompanyAdminEvents(ctx, f.a, 200)
		must(t, err)
		for _, event := range events {
			if event.ID == foreign || event.EventType != "company.admin.changed" || strings.Contains(string(event.Payload), "PRIVATE_STREAM_SENTINEL") {
				t.Fatal("foreign/private/non-admin outbox projection", event.ID)
			}
			delete(ids, event.ID)
			var wire map[string]json.RawMessage
			must(t, json.Unmarshal(event.Payload, &wire))
			if len(wire) != 4 {
				t.Fatal("outbox projection expanded wire envelope")
			}
		}
	}
	if len(ids) != 0 {
		t.Fatal("dispatcher state consumed SSE durable ID", ids)
	}
	limited, err := f.st.ListCompanyAdminEvents(ctx, f.a, 1)
	must(t, err)
	if len(limited) != 1 {
		t.Fatal("recent window bound missing")
	}
	for _, actor := range []authz.Actor{f.u, {Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: f.tenant.ID, IsAdmin: true}, {Type: authz.PrincipalUser, ID: f.admin.ID, TenantID: uuid.New(), IsAdmin: true}} {
		if _, err := f.st.ListCompanyAdminEvents(ctx, actor, 200); err == nil {
			t.Fatal("noninteractive/foreign/reader actor read management events")
		}
	}
	after := ""
	must(t, f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM outbox_events o`).Scan(&after))
	if before != after {
		t.Fatal("SSE poll changed dispatcher durable ownership")
	}
	// A cached admin actor must fail after the current stored role changes.
	_, err = f.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, f.admin.ID, models.RoleUser)
	must(t, err)
	if _, err := f.st.ListCompanyAdminEvents(ctx, f.a, 200); err == nil {
		t.Fatal("poll inherited handshake admin authority")
	}
}

// A PostgreSQL lock barrier proves the original user guard spans the release
// callback, and that a committed role/session change rejects the next frame.
// Callback errors and cancellation must release the same fence, too.
func TestR5CompanyAdminEventsReleaseFence(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("company admin event fence requires owned TABMAIL_TEST_DB_DSN")
	}
	for _, mode := range []string{"role", "session", "write-error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			actor := f.a
			version := f.admin.SessionVersion
			actor.SessionVersion = &version
			frameCtx, cancelFrame := context.WithCancel(ctx)
			defer cancelFrame()
			entered, release := make(chan struct{}), make(chan struct{})
			frameDone := make(chan error, 1)
			writeError := errors.New("controlled bounded frame write failure")
			go func() {
				frameDone <- f.st.WithCompanyAdminEventAccess(frameCtx, actor, func() error {
					close(entered)
					select {
					case <-release:
						if mode == "write-error" {
							return writeError
						}
						return nil
					case <-frameCtx.Done():
						return frameCtx.Err()
					}
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("frame did not acquire current actor fence")
			}
			writer, err := f.pool.Acquire(ctx)
			must(t, err)
			defer writer.Release()
			writerDone := make(chan error, 1)
			go func() {
				query := `UPDATE users SET role='user' WHERE id=$1`
				if mode == "session" {
					query = `UPDATE users SET session_version=session_version+1 WHERE id=$1`
				}
				_, err := writer.Exec(ctx, query, actor.ID)
				writerDone <- err
			}()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				var blocked bool
				must(t, f.pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1::int))>0`, int32(writer.Conn().PgConn().PID())).Scan(&blocked))
				if blocked {
					break
				}
				select {
				case err := <-writerDone:
					t.Fatalf("revoke committed before frame flush completed: %v", err)
				case <-ctx.Done():
					t.Fatal("actual PostgreSQL user fence lock wait missing")
				case <-tick.C:
				}
			}
			if mode == "cancel" {
				cancelFrame()
			} else {
				close(release)
			}
			select {
			case err := <-frameDone:
				if mode == "write-error" {
					if !errors.Is(err, writeError) {
						t.Fatal("write failure swallowed", err)
					}
				} else if mode == "cancel" {
					if err == nil {
						t.Fatal("cancelled frame reported success")
					}
				} else {
					must(t, err)
				}
			case <-ctx.Done():
				t.Fatal("frame did not release actor fence")
			}
			select {
			case err := <-writerDone:
				must(t, err)
			case <-ctx.Done():
				t.Fatal("revoke remained blocked after frame completion")
			}
			released := false
			if err := f.st.WithCompanyAdminEventAccess(ctx, actor, func() error { released = true; return nil }); err == nil || released {
				t.Fatal("post-commit revoked role/session released another frame")
			}
		})
	}
}
