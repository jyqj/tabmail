package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

func TestIngressMigrationRetainsLegacyAndVerifiesVersions(t *testing.T) {
	st, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	// Reconstruct the previous registered baseline only in this disposable DB.
	_, err := pool.Exec(ctx, `DROP TABLE ingress_targets,ingress_daily_usage;
 ALTER TABLE ingest_jobs DROP COLUMN recovery_managed,DROP COLUMN claim_token,DROP COLUMN raw_sha256,DROP COLUMN raw_size;
 DELETE FROM tabmail_schema_migrations WHERE version=4`)
	if err != nil {
		t.Fatal(err)
	}
	old := &models.IngestJob{ID: uuid.New(), Source: "smtp", Recipients: []string{"legacy@example.test"}, RawObjectKey: "legacy.eml", State: "processing"}
	if err = st.CreateIngestJob(ctx, old); err != nil {
		t.Fatal(err)
	}
	cfg := config.DB{DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 1, ConnMaxLifetime: time.Minute}
	upgraded, err := postgres.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var state string
	if err = pool.QueryRow(ctx, `SELECT state FROM ingest_jobs WHERE id=$1`, old.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "dead" {
		t.Fatal("legacy attempted receipt was blindly replayed")
	}
	refs, err := upgraded.CountRawObjectReferences(ctx, old.RawObjectKey)
	if err != nil || refs != 1 {
		t.Fatalf("legacy raw not protected: %d %v", refs, err)
	}
	var versions []int
	if err = pool.QueryRow(ctx, `SELECT array_agg(version ORDER BY version) FROM tabmail_schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || versions[0] != 1 || versions[1] != 2 || versions[2] != 4 {
		t.Fatalf("versions %v", versions)
	}
	restarted, err := postgres.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Close()
	_, err = pool.Exec(ctx, `INSERT INTO tabmail_schema_migrations(version,checksum) VALUES(3,'unrecognized-outbound')`)
	if err != nil {
		t.Fatal(err)
	}
	unexpected, err := postgres.New(ctx, cfg)
	if err == nil {
		unexpected.Close()
		t.Fatal("unsupported schema version was silently adopted")
	}
	_, err = pool.Exec(ctx, `DELETE FROM tabmail_schema_migrations WHERE version=3; UPDATE tabmail_schema_migrations SET checksum='tampered' WHERE version=4`)
	if err != nil {
		t.Fatal(err)
	}
	unexpected, err = postgres.New(ctx, cfg)
	if err == nil {
		unexpected.Close()
		t.Fatal("altered migration checksum accepted")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("%v", err)
	}
}

func TestIngressMigrationDoesNotDeleteHistoricalBytes(t *testing.T) {
	b, err := os.ReadFile("migrations/0004_ingress_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"DELETE FROM messages", "DELETE FROM ingest_jobs", "DROP TABLE", "REFERENCES mailboxes"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("unexpected destructive migration: %s", forbidden)
		}
	}
}
