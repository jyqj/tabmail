package testpg

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A template belongs to one explicit TestMain lifetime, never to production or
// a process-global server cache. Each fixture remains a separate owned database.
var templateScope struct {
	sync.RWMutex
	owner *databaseTemplate
}

type databaseTemplate struct {
	dsn      string
	gate     chan struct{}
	admin    *pgxpool.Pool
	name     string
	ready    bool
	err      error
	closed   bool
	closeErr error
}

func newDatabaseTemplate(dsn string) *databaseTemplate {
	return &databaseTemplate{dsn: dsn, gate: make(chan struct{}, 1)}
}

// RunWithDatabaseTemplate opts one test binary into a single immutable migrated
// template. The supplied DSN is only the admin connection: it is never migrated,
// emptied or used as the template. Missing DSN keeps the original local behavior.
// Call only from TestMain, before m.Run; ordinary fatal tests still run cleanup.
func RunWithDatabaseTemplate(run func() int) (code int) {
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		return run()
	}
	owner := newDatabaseTemplate(dsn)
	templateScope.Lock()
	if templateScope.owner != nil {
		templateScope.Unlock()
		fmt.Fprintln(os.Stderr, "nested PostgreSQL template scope rejected")
		return 1
	}
	templateScope.owner = owner
	templateScope.Unlock()
	defer func() {
		templateScope.Lock()
		templateScope.owner = nil
		templateScope.Unlock()
		if err := owner.close(); err != nil {
			fmt.Fprintf(os.Stderr, "owned PostgreSQL template cleanup failed: %T\n", err)
			code = 1
		}
	}()
	return run()
}

func fixtureTemplate(ctx context.Context, dsn string, initialize func(string) error) (string, error) {
	templateScope.RLock()
	owner := templateScope.owner
	templateScope.RUnlock()
	if owner == nil || owner.dsn != dsn {
		return "", nil
	}
	return owner.prepare(ctx, initialize)
}

func (d *databaseTemplate) prepare(ctx context.Context, initialize func(string) error) (string, error) {
	select {
	case d.gate <- struct{}{}:
		defer func() { <-d.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.closed {
		return "", fmt.Errorf("PostgreSQL template owner is closed")
	}
	if d.err != nil {
		return "", d.err // A partial initialization never becomes a clone source.
	}
	if d.ready {
		return d.name, nil
	}
	var err error
	d.admin, err = pgxpool.New(ctx, d.dsn)
	if err != nil {
		d.err = err
		return "", err
	}
	name := "tm_template_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = d.admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		d.err = err
		return "", err
	}
	d.name = name // Ownership is granted only after successful creation.
	u, err := url.Parse(d.dsn)
	if err == nil {
		u.Path = "/" + name
		err = initialize(u.String())
	}
	if err == nil {
		// initialize must close every connection before sealing. Even the owner
		// cannot connect accidentally and poison later fixture copies.
		_, err = d.admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS false")
	}
	if err != nil {
		d.err = err
		return "", err
	}
	d.ready = true
	return name, nil
}

func (d *databaseTemplate) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), postgresFixtureDropTimeout)
	defer cancel()
	select {
	case d.gate <- struct{}{}:
		defer func() { <-d.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if d.closed {
		return d.closeErr
	}
	d.closed = true
	if d.admin == nil {
		return nil
	}
	defer d.admin.Close()
	if d.name == "" {
		return nil
	}
	_, d.closeErr = d.admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{d.name}.Sanitize()+" WITH (FORCE)")
	return d.closeErr
}
