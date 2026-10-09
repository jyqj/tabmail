package settings_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/settings"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

// Both versions pause immediately before the first seed's real database write.
// The old manager has already observed a missing row before calling Upsert;
// the atomic manager calls SeedSetting without a separate existence decision.
// The narrow assertion keeps this same fixture compilable on the old source.
type r5SettingsSeedPGGate struct {
	*postgres.PgStore
	entered, resume chan struct{}
	enterOnce       sync.Once
	releaseOnce     sync.Once
	writes          int
	writeErr        error
}

func (s *r5SettingsSeedPGGate) release() { s.releaseOnce.Do(func() { close(s.resume) }) }

func (s *r5SettingsSeedPGGate) beforeWrite(ctx context.Context) error {
	s.enterOnce.Do(func() { close(s.entered) })
	select {
	case <-s.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *r5SettingsSeedPGGate) UpsertSetting(ctx context.Context, key, value, description string) (err error) {
	defer func() { s.writes++; s.writeErr = err }()
	if err = s.beforeWrite(ctx); err != nil {
		return err
	}
	return s.PgStore.UpsertSetting(ctx, key, value, description)
}

func (s *r5SettingsSeedPGGate) SeedSetting(ctx context.Context, key, value, description string) (inserted bool, err error) {
	defer func() { s.writes++; s.writeErr = err }()
	if err = s.beforeWrite(ctx); err != nil {
		return false, err
	}
	port, ok := any(s.PgStore).(interface {
		SeedSetting(context.Context, string, string, string) (bool, error)
	})
	if !ok {
		return false, errors.New("production PostgreSQL atomic seed command is unavailable")
	}
	return port.SeedSetting(ctx, key, value, description)
}

func r5SettingsSeedPGRead(t *testing.T, pool *pgxpool.Pool, key string) models.SystemSetting {
	t.Helper()
	var row models.SystemSetting
	if err := pool.QueryRow(t.Context(), `SELECT key, value, description, updated_at FROM system_settings WHERE key=$1`, key).
		Scan(&row.Key, &row.Value, &row.Description, &row.UpdatedAt); err != nil {
		t.Fatalf("read persisted setting %q: %v", key, err)
	}
	return row
}

func r5SettingsSeedPGEqual(a, b models.SystemSetting) bool {
	return a.Key == b.Key && a.Value == b.Value && a.Description == b.Description && a.UpdatedAt.Equal(b.UpdatedAt)
}

func TestR5SettingsSeedPGConcurrentWrites(t *testing.T) {
	for _, writer := range []string{"concurrent_admin_write", "concurrent_startup_seed"} {
		t.Run(writer, func(t *testing.T) {
			st, observer, dsn := testpg.NewPostgres(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			// A second production PgStore owns a separate connection pool. Its
			// committed row is observed through the fixture's third pool.
			other, err := postgres.New(ctx, config.DB{DSN: dsn, MaxOpenConns: 1, ConnMaxLifetime: time.Minute})
			if err != nil {
				t.Fatalf("open independent settings writer: %T", err)
			}
			t.Cleanup(func() {
				if err := other.Close(); err != nil {
					t.Errorf("close independent settings writer: %T", err)
				}
			})
			key := models.SettingOpenRegistration
			var existing int
			if err := observer.QueryRow(ctx, `SELECT count(*) FROM system_settings WHERE key=$1`, key).Scan(&existing); err != nil || existing != 0 {
				t.Fatalf("fixture requires initially absent setting: rows=%d err=%v", existing, err)
			}
			gate := &r5SettingsSeedPGGate{PgStore: st, entered: make(chan struct{}), resume: make(chan struct{})}
			done := make(chan struct{})
			t.Cleanup(func() {
				gate.release()
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("owned settings seed did not stop before pool cleanup")
				}
			})
			stale := settings.SeedValue{Value: "true", Description: "first startup environment"}
			go func() {
				defer close(done)
				settings.NewManager(gate, zerolog.Nop()).Seed(ctx, map[string]settings.SeedValue{key: stale})
			}()
			select {
			case <-gate.entered:
			case <-ctx.Done():
				t.Fatal("first seed did not reach its database write boundary")
			}
			winner := settings.SeedValue{Value: "false", Description: writer + " committed choice"}
			second := settings.NewManager(other, zerolog.Nop())
			if writer == "concurrent_admin_write" {
				if err := second.Set(ctx, key, winner.Value, winner.Description); err != nil {
					t.Fatalf("commit administrator setting: %v", err)
				}
			} else {
				second.Seed(ctx, map[string]settings.SeedValue{key: winner})
			}
			before := r5SettingsSeedPGRead(t, observer, key)
			if before.Value != winner.Value || before.Description != winner.Description || before.UpdatedAt.IsZero() {
				t.Fatalf("competing writer did not commit its intended value: %+v", before)
			}
			gate.release()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("first seed did not finish after its write boundary was released")
			}
			if gate.writes != 1 || gate.writeErr != nil {
				t.Fatalf("first seed must execute one successful database command: writes=%d err=%v", gate.writes, gate.writeErr)
			}
			after := r5SettingsSeedPGRead(t, observer, key)
			if after.Value == stale.Value && after.Description == stale.Description {
				t.Errorf("EXEC07_STALE_SEED_OVERWROTE_COMMITTED_SETTING writer=%s before_value=%q after_value=%q before_description=%q after_description=%q before_updated_at=%s after_updated_at=%s", writer, before.Value, after.Value, before.Description, after.Description, before.UpdatedAt.Format(time.RFC3339Nano), after.UpdatedAt.Format(time.RFC3339Nano))
			} else if !r5SettingsSeedPGEqual(before, after) {
				t.Errorf("seed changed committed setting metadata: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestR5SettingsSeedPGPreservesExistingValues(t *testing.T) {
	st, observer, _ := testpg.NewPostgres(t)
	for _, tc := range []struct{ name, value string }{
		{"empty", ""}, {"false", "false"}, {"zero", "0"}, {"text", "administrator choice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := "exec07_existing_" + tc.name
			if err := st.UpsertSetting(t.Context(), key, tc.value, "existing description"); err != nil {
				t.Fatal(err)
			}
			before := r5SettingsSeedPGRead(t, observer, key)
			settings.NewManager(st, zerolog.Nop()).Seed(t.Context(), map[string]settings.SeedValue{
				key: {Value: "environment default", Description: "environment description"},
			})
			if after := r5SettingsSeedPGRead(t, observer, key); !r5SettingsSeedPGEqual(before, after) {
				t.Errorf("existing %s setting or metadata changed: before=%+v after=%+v", tc.name, before, after)
			}
		})
	}
}

func TestR5SettingsSeedPGCreatesAbsentDefaults(t *testing.T) {
	st, observer, _ := testpg.NewPostgres(t)
	defaults := map[string]settings.SeedValue{
		models.SettingOpenRegistration: {Value: "false", Description: "registration disabled"},
		models.SettingPublicIPRPM:      {Value: "0", Description: "zero remains zero"},
		"exec07_empty_default":         {Value: "", Description: "empty remains present"},
	}
	settings.NewManager(st, zerolog.Nop()).Seed(t.Context(), defaults)
	for key, want := range defaults {
		got := r5SettingsSeedPGRead(t, observer, key)
		if got.Value != want.Value || got.Description != want.Description || got.UpdatedAt.IsZero() {
			t.Errorf("new default not stored exactly: key=%s got=%+v want=%+v", key, got, want)
		}
	}
}

func TestR5SettingsSeedPGCancelledContext(t *testing.T) {
	st, observer, _ := testpg.NewPostgres(t)
	const existingKey, absentKey = "exec07_cancel_existing", "exec07_cancel_absent"
	if err := st.UpsertSetting(t.Context(), existingKey, "retained", "retained description"); err != nil {
		t.Fatal(err)
	}
	before := r5SettingsSeedPGRead(t, observer, existingKey)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	settings.NewManager(st, zerolog.Nop()).Seed(ctx, map[string]settings.SeedValue{
		existingKey: {Value: "unwanted", Description: "unwanted description"},
		absentKey:   {Value: "unwanted", Description: "unwanted description"},
	})
	var created int
	if err := observer.QueryRow(t.Context(), `SELECT count(*) FROM system_settings WHERE key=$1`, absentKey).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if after := r5SettingsSeedPGRead(t, observer, existingKey); created != 0 || !r5SettingsSeedPGEqual(before, after) {
		t.Errorf("cancelled seed changed stored settings: created=%d before=%+v after=%+v", created, before, after)
	}
}
