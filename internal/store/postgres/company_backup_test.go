package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"testing"
	"time"
)

func TestR3CoordinatedSnapshotRestoresIntoEmptyTargets(t *testing.T) {
	if _, e := exec.LookPath("pg_dump"); e != nil {
		t.Skip("PostgreSQL client tools required for snapshot rehearsal")
	}
	f := seedCompany(t)
	ctx := context.Background()
	dir := t.TempDir()
	objects := filepath.Join(dir, "objects")
	must(t, os.Mkdir(objects, 0700))
	must(t, os.WriteFile(filepath.Join(objects, "original.eml"), []byte("Subject: Restored\r\n\r\nRestored bytes"), 0600))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "client@recipient.test", Recipients: []string{f.personal.FullAddress}, Subject: "Restored", RawObjectKey: "original.eml", Size: 35}
	must(t, f.st.CreateMessage(ctx, m))
	script, e := filepath.Abs("../../../scripts/company_snapshot.py")
	must(t, e)
	snapshot := filepath.Join(dir, "snapshot")
	invoke := func(mode, dsn, data string) error {
		cmd := exec.Command("python3", script, mode, snapshot)
		cmd.Env = append(os.Environ(), "TABMAIL_DB_DSN="+dsn, "TABMAIL_DATADIR="+data, "TABMAIL_WRITERS_STOPPED=yes", "TABMAIL_OBJECT_STORE=fs")
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Log(string(b))
		}
		return e
	}
	source := f.pool.Config().ConnString()
	must(t, invoke("backup", source, objects))
	target := "tm_restore_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, e = f.pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{target}.Sanitize())
	must(t, e)
	defer f.pool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{target}.Sanitize()+" WITH (FORCE)")
	u, e := url.Parse(source)
	must(t, e)
	u.Path = "/" + target
	restoreObjects := filepath.Join(dir, "restored")
	must(t, invoke("restore", u.String(), restoreObjects))
	restored, e := postgres.New(ctx, config.DB{DSN: u.String(), MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
	must(t, e)
	defer restored.Close()
	got, e := restored.GetMessage(ctx, m.ID)
	must(t, e)
	if got == nil || got.Subject != "Restored" {
		t.Fatal("message did not restore")
	}
	user, e := restored.GetUser(ctx, f.employee.ID)
	must(t, e)
	if user == nil || user.Role != models.RoleUser {
		t.Fatal("identity did not restore")
	}
	b, e := os.ReadFile(filepath.Join(restoreObjects, got.RawObjectKey))
	must(t, e)
	if !strings.Contains(string(b), "Restored bytes") {
		t.Fatal("original did not restore")
	}
	if invoke("restore", u.String(), restoreObjects) == nil {
		t.Fatal("destructive second restore allowed")
	}
}
