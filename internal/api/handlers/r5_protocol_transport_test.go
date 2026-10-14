//go:build r5protocol

package handlers_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func r5TransportStream(t *testing.T, f *r5UIFixture, token string) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", f.server.URL+"/api/v1/company/events", nil)
	r5UIMust(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := f.server.Client().Do(req)
	r5UIMust(t, err)
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("actual SSE headers missing")
	}
	return response, bufio.NewReader(response.Body), cancel
}
func r5TransportFrame(t *testing.T, reader *bufio.Reader) (string, json.RawMessage) {
	t.Helper()
	var event string
	var data json.RawMessage
	for {
		line, err := reader.ReadString('\n')
		r5UIMust(t, err)
		if line == "\n" {
			return event, data
		}
		if len(line) > 7 && line[:7] == "event: " {
			event = line[7 : len(line)-1]
		}
		if len(line) > 6 && line[:6] == "data: " {
			data = json.RawMessage(line[6 : len(line)-1])
		}
	}
}

// Focused transport probes only. No formal protocol collector or qualification.
func TestR5TransportRealRouter(t *testing.T) {
	var database, address string
	t.Run("ready_permission_event_eof_reconnect_json_cleanup", func(t *testing.T) {
		f := r5UISeed(t)
		database = f.pool.Config().ConnConfig.Database
		address = f.server.Listener.Addr().String()
		token := r5UIToken(t, f.admin)
		response, reader, cancel := r5TransportStream(t, f, token)
		name, data := r5TransportFrame(t, reader)
		var ready struct {
			TenantID string `json:"tenant_id"`
		}
		r5UIMust(t, json.Unmarshal(data, &ready))
		if name != "ready" || ready.TenantID != f.tenant.ID.String() {
			t.Fatal("actual ready frame missing")
		}
		path := "/api/v1/admin/users/" + f.employee.ID.String() + "/permission-editor"
		before := r5UIReadPermissionEditor(t, f, token)
		raw := r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": before.Revision, "patch": map[string]any{"can_send": false}}, 200)
		sum := sha256.Sum256(raw)
		f.mu.Lock()
		trace := f.trace[len(f.trace)-1]
		f.mu.Unlock()
		if trace.Status != 200 || trace.Path != path || trace.ResponseSHA256 != hex.EncodeToString(sum[:]) || trace.ErrorCode != "" {
			t.Fatal("real JSON packet evidence changed")
		}
		conflict := r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": before.Revision, "patch": map[string]any{"can_send": true}}, 409)
		sum = sha256.Sum256(conflict)
		f.mu.Lock()
		trace = f.trace[len(f.trace)-1]
		f.mu.Unlock()
		if trace.Status != 409 || trace.ErrorCode != "CONFLICT" || trace.ResponseSHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("real JSON conflict evidence changed")
		}
		found := false
		for !found {
			name, data = r5TransportFrame(t, reader)
			if name != "company.admin.changed" {
				continue
			}
			// Explicit wire tags, never infer an event from UI invalidation.
			var wire struct {
				Metadata struct {
					Action     string `json:"action"`
					ResourceID string `json:"resource_id"`
				} `json:"metadata"`
			}
			r5UIMust(t, json.Unmarshal(data, &wire))
			found = wire.Metadata.Action == "permission.override.patch" && wire.Metadata.ResourceID == f.employee.ID.String()
		}
		cancel()
		response.Body.Close()
		// Shipping authority revalidation ends the stream itself with a clean EOF.
		response, reader, cancel = r5TransportStream(t, f, token)
		name, _ = r5TransportFrame(t, reader)
		if name != "ready" {
			t.Fatal("reconnect lacks actual ready")
		}
		_, err := f.pool.Exec(context.Background(), `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
		r5UIMust(t, err)
		for {
			_, err = reader.ReadString('\n')
			if err != nil {
				break
			}
		}
		if err != io.EOF {
			t.Fatalf("server authority closure did not yield clean EOF: %T", err)
		}
		response.Body.Close()
		cancel()
		_, err = f.pool.Exec(context.Background(), `UPDATE users SET is_active=true WHERE id=$1`, f.admin.ID)
		r5UIMust(t, err)
		_, reader, _ = r5TransportStream(t, f, token)
		name, _ = r5TransportFrame(t, reader)
		if name != "ready" {
			t.Fatal("post-EOF reconnect lacks ready")
		}
		// Leave the last request live: owned server cleanup must terminate it.
		f.closeTransport()
		for {
			_, err = reader.ReadString('\n')
			if err != nil {
				break
			}
		}
		t.Log("actual ready, permission.override.patch, clean EOF, reconnect and JSON digest verified")
	})
	if database == "" {
		t.Fatal("owned database identity missing")
	}
	r5TransportAssertReleased(t, database, address)
}

func TestR5TransportFetchObserver(t *testing.T) {
	var f *r5UIFixture
	t.Cleanup(func() {
		if f == nil {
			return
		}
		r5TransportAssertReleased(t, f.pool.Config().ConnConfig.Database, f.server.Listener.Addr().String())
	})
	f = r5UISeed(t)
	fixture := map[string]string{"api_url": f.server.URL, "token": r5UIToken(t, f.admin), "tenant_id": f.tenant.ID.String(), "employee_id": f.employee.ID.String()}
	raw, err := json.Marshal(fixture)
	r5UIMust(t, err)
	path := filepath.Join(t.TempDir(), "fixture.json")
	r5UIMust(t, os.WriteFile(path, raw, 0600))
	_, root, _, _ := runtime.Caller(0)
	web := filepath.Join(filepath.Dir(root), "../../../web")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "node_modules/vitest/vitest.mjs", "run", "--config", "vitest.r5transport.config.ts")
	cmd.Dir = web
	cmd.Env = append(os.Environ(), "TABMAIL_R5_TRANSPORT_FIXTURE="+path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dedicated real transport observer failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func r5TransportAssertReleased(t *testing.T, database, address string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, os.Getenv("TABMAIL_TEST_DB_DSN"))
	r5UIMust(t, err)
	defer admin.Close()
	var databases, backends int
	r5UIMust(t, admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname=$1`, database).Scan(&databases))
	r5UIMust(t, admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=$1`, database).Scan(&backends))
	if databases != 0 || backends != 0 {
		t.Fatal("owned PostgreSQL resources remain")
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	if response, err := client.Get((&url.URL{Scheme: "http", Host: address}).String()); err == nil {
		response.Body.Close()
		t.Fatal("owned HTTP listener remains")
	}
	t.Log("zero owned PostgreSQL databases/backends; HTTP listener closed")
}

func TestR5TransportLostSubmitResponse(t *testing.T) {
	f := r5UISeed(t)
	setup := r5UISetup(t, f, r5UICase{ID: "RC02", Input: json.RawMessage(`{}`)}, "submit_replay", "")
	var draft struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	r5UIMust(t, json.Unmarshal(setup["draft"].(json.RawMessage), &draft))
	path := "/api/v1/company/drafts/" + draft.ID + "/submit"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command, err := json.Marshal(map[string]any{"expected_revision": draft.Revision})
	r5UIMust(t, err)
	request := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", f.server.URL+path, bytes.NewReader(command))
		// This probe observes the fault before explicitly issuing its replay.
		req.GetBody = nil
		r5UIMust(t, err)
		req.Header.Set("Authorization", "Bearer "+r5UIToken(t, f.employee))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "r5-transport-owned-replay")
		return f.server.Client().Do(req)
	}
	response, err := request()
	if err == nil {
		response.Body.Close()
		t.Fatal("real committed submit response was not aborted")
	}
	response, err = request()
	r5UIMust(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	r5UIMust(t, err)
	if response.StatusCode != 200 || !json.Valid(raw) {
		t.Fatal("real submit replay response missing")
	}
	f.mu.Lock()
	var submits []r5UITrace
	for _, trace := range f.trace {
		if trace.Path == path {
			submits = append(submits, trace)
		}
	}
	f.mu.Unlock()
	if len(submits) != 2 || !submits[0].TransportAborted || submits[0].Status != 201 || submits[1].TransportAborted || submits[1].Status != 200 || submits[0].IdempotencySHA != submits[1].IdempotencySHA {
		t.Fatal("lost-response trace fidelity changed")
	}
	var jobs int
	r5UIMust(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, draft.ID).Scan(&jobs))
	if jobs != 1 {
		t.Fatal("replay duplicated committed job")
	}
}
