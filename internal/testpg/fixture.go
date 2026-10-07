package testpg

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
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
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test database URL: %T", err)
	}
	// Initialization owns its cancellation deadline. Keep the existing DROP
	// budget available before testing's hard process timeout, and inherit a
	// cancelled test context instead of opening a new database during teardown.
	setupDeadline := time.Now().Add(postgresFixtureSetupTimeout)
	if testDeadline, ok := t.Deadline(); ok {
		latestSetup := testDeadline.Add(-postgresFixtureDropTimeout)
		if latestSetup.Before(setupDeadline) {
			setupDeadline = latestSetup
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), setupDeadline)
	defer cancel()
	if ctx.Err() != nil {
		t.Fatal("PostgreSQL fixture setup has no remaining lifecycle budget")
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	owned := &postgresFixtureResources{closeAdmin: admin.Close}
	t.Cleanup(func() {
		if err := owned.close(); err != nil {
			// Driver errors can include connection inputs. Report the failure,
			// never the DSN or the error's potentially sensitive contents.
			t.Errorf("owned PostgreSQL database cleanup failed: %T", err)
		}
	})
	name := "tm_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	// A failed CREATE never grants ownership of an existing database name.
	owned.dropDatabase = func(cleanupCtx context.Context) error {
		_, err := admin.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		return err
	}
	u.Path = "/" + name
	testDSN := u.String()
	st, err := postgres.New(ctx, config.DB{DSN: testDSN, MaxOpenConns: 10, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	owned.closeStore = func() { _ = st.Close() }
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	owned.closeObserver = pool.Close
	return st, pool, testDSN
}

const (
	postgresFixtureSetupTimeout = 30 * time.Second
	postgresFixtureDropTimeout  = 10 * time.Second
)

// Registration is incremental, immediately after each resource is acquired.
// These closures are fixture-owned, not shared connections or a migrated DB
// template. Close remains synchronous: a leaked borrower is not hidden by a
// background cleanup goroutine.
type postgresFixtureResources struct {
	closeObserver func()
	closeStore    func()
	dropDatabase  func(context.Context) error
	closeAdmin    func()
	once          sync.Once
	dropErr       error
}

func (r *postgresFixtureResources) close() error {
	r.once.Do(func() {
		if r.closeAdmin != nil {
			defer r.closeAdmin()
		}
		if r.closeObserver != nil {
			r.closeObserver()
		}
		if r.closeStore != nil {
			r.closeStore()
		}
		if r.dropDatabase != nil {
			// testing cancels t.Context before cleanup. Use an independent
			// bounded context, created only after borrowers have been closed.
			ctx, cancel := context.WithTimeout(context.Background(), postgresFixtureDropTimeout)
			defer cancel()
			r.dropErr = r.dropDatabase(ctx)
		}
	})
	return r.dropErr
}
