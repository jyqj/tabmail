package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewR5LoopbackProbe writes the private PG/HTTP probe input owned by t.
// The caller supplies its isolated NewPostgres pool and keeps it alive until
// the consuming process joins. No fixture values are copied into evidence.
func NewR5LoopbackProbe(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key string) string {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("owned loopback probe setup failed: %T", err)
		}
	}
	privateBytes := make([]byte, 24)
	_, err := rand.Read(privateBytes)
	must(err)
	token := hex.EncodeToString(privateBytes)
	_, err = pool.Exec(ctx, "CREATE TABLE r5_batch_probe (identity text NOT NULL)")
	must(err)
	_, err = pool.Exec(ctx, "INSERT INTO r5_batch_probe VALUES ($1)", key)
	must(err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var identity string
		if err := pool.QueryRow(r.Context(), "SELECT identity FROM r5_batch_probe").Scan(&identity); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"identity": identity})
	}))
	t.Cleanup(server.Close)
	fixture := filepath.Join(t.TempDir(), "fixture.json")
	bytes, err := json.Marshal(map[string]string{"case_id": key, "api_url": server.URL, "token": token})
	must(err)
	must(os.WriteFile(fixture, bytes, 0600))
	return fixture
}
