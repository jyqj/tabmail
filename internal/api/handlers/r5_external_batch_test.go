//go:build r5protocol

package handlers_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"tabmail/internal/testpg"
)

// The bridge never launches a helper or accepts an executable. Only a batch
// supervisor holding the pinned exclusive lease can serve this private channel.
func r5BatchRPC(ctx context.Context, request map[string]any) (map[string]json.RawMessage, error) {
	channel, nonce := os.Getenv("TABMAIL_R5_BATCH_SOCKET"), os.Getenv("TABMAIL_R5_BATCH_NONCE")
	if !filepath.IsAbs(channel) || nonce == "" {
		return nil, errors.New("explicit private batch capability required")
	}
	pin := os.Getenv("TABMAIL_R5_BATCH_CONTRACT_SHA256")
	if len(pin) != 64 {
		return nil, errors.New("independent batch contract identity required")
	}
	request["nonce"] = nonce
	request["contract_sha256"] = pin
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", channel)
	if err != nil {
		return nil, fmt.Errorf("batch channel connection: %T", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err = json.NewEncoder(connection).Encode(request); err != nil {
		return nil, fmt.Errorf("batch request: %T", err)
	}
	var response map[string]json.RawMessage
	err = json.NewDecoder(io.LimitReader(connection, 65536)).Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("batch response: %T", err)
	}
	var ok bool
	if json.Unmarshal(response["ok"], &ok) != nil || !ok {
		return nil, errors.New("owned batch request rejected")
	}
	return response, nil
}

type r5BatchChildExit struct{ code int }

func (e *r5BatchChildExit) Error() string { return "supervised Vitest child failed" }
func (e *r5BatchChildExit) ExitCode() int { return e.code }

func r5BatchExecute(ctx context.Context, key, fixture, report string) ([]byte, error) {
	done := make(chan struct{})
	cancelDone := make(chan struct{})
	go func() {
		defer close(cancelDone)
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = r5BatchRPC(cancelCtx, map[string]any{"operation": "cancel", "key": key})
		case <-done:
		}
	}()
	// Read the physical child-join response even after the 75-second case context
	// cancels. Batch180 bounds the supervisor; no direct helper can be orphaned.
	joinCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	response, err := r5BatchRPC(joinCtx, map[string]any{"operation": "case", "key": key, "fixture": fixture})
	close(done)
	<-cancelDone
	if err != nil {
		return nil, err
	}
	var result struct {
		Exit          int `json:"exit_code"`
		Timeout, Tail bool
		Report        string
	}
	raw, _ := json.Marshal(response)
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Timeout || result.Tail || ctx.Err() != nil {
		return nil, errors.New("batch child lifecycle rejected")
	}
	if err = os.MkdirAll(filepath.Dir(report), 0700); err != nil {
		return nil, err
	}
	// Fresh report paths are fixed by the Go fixture; both copies remain private.
	if _, err = os.Lstat(report); !os.IsNotExist(err) {
		return nil, errors.New("fresh case report required")
	}
	bytes, err := os.ReadFile(result.Report)
	if err != nil {
		return nil, fmt.Errorf("batch staged report: %T", err)
	}
	if err = os.WriteFile(report, bytes, 0600); err != nil {
		return nil, err
	}
	logs, err := os.ReadFile(filepath.Join(filepath.Dir(result.Report), "stdout"))
	if err != nil {
		return nil, fmt.Errorf("batch child log: %T", err)
	}
	stderr, err := os.ReadFile(filepath.Join(filepath.Dir(result.Report), "stderr"))
	if err != nil {
		return nil, fmt.Errorf("batch child log: %T", err)
	}
	logs = append(logs, stderr...)
	if result.Exit != 0 {
		return logs, &r5BatchChildExit{code: result.Exit}
	}
	return logs, nil
}

func r5BatchSource(t *testing.T) (string, []byte) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	raw, err := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, err)
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != os.Getenv("TABMAIL_R5_BATCH_CATALOG_SHA256") {
		t.Fatal("independent batch catalog pin required")
	}
	return root, raw
}

func TestR5ProtocolBatchComponentObservations(t *testing.T) {
	root, raw := r5BatchSource(t)
	if os.Getenv("TABMAIL_R5_PROTOCOL_COMPONENT_EVIDENCE") == "" {
		t.Fatal("fresh private case evidence root required")
	}
	var catalog struct {
		Cases []struct {
			ID       string          `json:"id"`
			Input    json.RawMessage `json:"input"`
			Adapters []struct {
				Test            string   `json:"test"`
				ComponentSource string   `json:"component_source"`
				Paths           []string `json:"runtime_test_paths"`
			} `json:"shared_adapters"`
		} `json:"cases"`
	}
	r5UIMust(t, json.Unmarshal(raw, &catalog))
	slots := make(chan struct{}, 4)
	var mu sync.Mutex
	completed := []string{}
	seen := map[string]bool{}
	// t.Run waits for every parallel leaf and its cleanup before owner ack.
	t.Run("owners", func(group *testing.T) {
		// The supervisor maps this dedicated /owners group to the original
		// declared case paths; business assertions keep their exact identity.
		for _, row := range catalog.Cases {
			for _, adapter := range row.Adapters {
				if adapter.Test != "TestR5ProtocolComponentObservations" || adapter.ComponentSource == "" {
					continue
				}
				for _, path := range adapter.Paths {
					key := strings.TrimPrefix(path, "TestR5ProtocolComponentObservations/")
					variant := strings.TrimPrefix(key, row.ID+"/")
					if key == path || seen[key] {
						t.Fatal("duplicate/unbound catalog variant")
					}
					seen[key] = true
					c := r5UICase{ID: row.ID, Input: row.Input}
					group.Run(key, func(leaf *testing.T) {
						leaf.Parallel()
						slots <- struct{}{}
						// Registered first, so acknowledgement follows every fixture cleanup.
						leaf.Cleanup(func() { mu.Lock(); completed = append(completed, key); mu.Unlock(); <-slots })
						r5UIObserveCase(leaf, root, c, variant, os.Getenv("TABMAIL_R5_BATCH_CATALOG_SHA256"), func(ctx context.Context, private, report string) ([]byte, error) {
							return r5BatchExecute(ctx, key, private, report)
						})
					})
				}
			}
		}
	})
	sort.Strings(completed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r5BatchRPC(ctx, map[string]any{"operation": "ack", "completed": completed})
	r5UIMust(t, err)
}

func TestR5ExternalBatchIsolationProbe(t *testing.T) {
	_, _ = r5BatchSource(t)
	slots := make(chan struct{}, 4)
	var mu sync.Mutex
	completed := []string{}
	t.Run("owners", func(group *testing.T) {
		for i := 0; i < 8; i++ {
			key := fmt.Sprint("probe/", i)
			group.Run(key, func(leaf *testing.T) {
				leaf.Parallel()
				slots <- struct{}{}
				leaf.Cleanup(func() { mu.Lock(); completed = append(completed, key); mu.Unlock(); <-slots })
				_, pool, _ := testpg.NewPostgres(leaf)
				ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
				defer cancel()
				privateBytes := make([]byte, 24)
				_, err := rand.Read(privateBytes)
				r5UIMust(leaf, err)
				token := hex.EncodeToString(privateBytes)
				_, err = pool.Exec(ctx, "CREATE TABLE r5_batch_probe (identity text NOT NULL)")
				r5UIMust(leaf, err)
				_, err = pool.Exec(ctx, "INSERT INTO r5_batch_probe VALUES ($1)", key)
				r5UIMust(leaf, err)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer "+token {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					var identity string
					if err := pool.QueryRow(r.Context(), "SELECT identity FROM r5_batch_probe").Scan(&identity); err != nil {
						w.WriteHeader(500)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"identity": identity})
				}))
				leaf.Cleanup(server.Close)
				fixture := filepath.Join(leaf.TempDir(), "fixture.json")
				bytes, err := json.Marshal(map[string]string{"case_id": key, "api_url": server.URL, "token": token})
				r5UIMust(leaf, err)
				r5UIMust(leaf, os.WriteFile(fixture, bytes, 0600))
				report := filepath.Join(leaf.TempDir(), "report.json")
				_, err = r5BatchExecute(ctx, key, fixture, report)
				r5UIMust(leaf, err)
				var result struct {
					Success bool
					Total   int `json:"numTotalTests"`
					Passed  int `json:"numPassedTests"`
				}
				bytes, err = os.ReadFile(report)
				r5UIMust(leaf, err)
				r5UIMust(leaf, json.Unmarshal(bytes, &result))
				if !result.Success || result.Total != 1 || result.Passed != 1 {
					leaf.Fatal("independent realm/HTTP/PostgreSQL probe failed")
				}
			})
		}
	})
	sort.Strings(completed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r5BatchRPC(ctx, map[string]any{"operation": "ack", "completed": completed})
	r5UIMust(t, err)
}

func TestR5BatchBridgeExitAndJoin(t *testing.T) {
	for _, code := range []int{0, 1} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			private := t.TempDir()
			staged := filepath.Join(private, "vitest.json")
			r5UIMust(t, os.WriteFile(staged, []byte(`{"success":false}`), 0600))
			r5UIMust(t, os.WriteFile(filepath.Join(private, "stdout"), []byte("owned child output"), 0600))
			r5UIMust(t, os.WriteFile(filepath.Join(private, "stderr"), nil, 0600))
			channel := filepath.Join(private, "channel.sock")
			listener, err := net.Listen("unix", channel)
			r5UIMust(t, err)
			done := make(chan struct{})
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				var request map[string]any
				_ = json.NewDecoder(connection).Decode(&request)
				_ = json.NewEncoder(connection).Encode(map[string]any{"ok": true, "exit_code": code, "timeout": false, "tail": false, "report": staged})
			}()
			t.Setenv("TABMAIL_R5_BATCH_SOCKET", channel)
			t.Setenv("TABMAIL_R5_BATCH_NONCE", "unit-owned-capability")
			t.Setenv("TABMAIL_R5_BATCH_CONTRACT_SHA256", strings.Repeat("0", 64))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			logs, childErr := r5BatchExecute(ctx, "probe/0", "private-fixture", filepath.Join(t.TempDir(), "report.json"))
			_ = listener.Close()
			<-done
			if string(logs) != "owned child output" {
				t.Fatal("supervised real log not retained")
			}
			if code == 0 && childErr != nil {
				t.Fatal(childErr)
			}
			if code != 0 {
				var exit interface{ ExitCode() int }
				if !errors.As(childErr, &exit) || exit.ExitCode() != code {
					t.Fatal("numeric supervised child exit not retained")
				}
			}
		})
	}
}
