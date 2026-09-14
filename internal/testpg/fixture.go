package testpg

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/config"
	"tabmail/internal/store/postgres"
)

// NewPostgres creates an isolated disposable database. Never clears the supplied
// DSN's database. CI sets TABMAIL_TEST_DB_DSN; local tests explicitly skip otherwise.
func NewPostgres(t *testing.T) (*postgres.PgStore, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TABMAIL_TEST_DB_DSN is required for real PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "tm_test_" + uuid.New().String()[:8]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	testDSN := u.String()
	st, err := postgres.New(ctx, config.DB{DSN: testDSN, MaxOpenConns: 10, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		st.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
	})
	return st, pool, testDSN
}
