package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"tabmail/internal/models"
)

// capturingExecer records the bound args of an Exec call so tests can assert how
// values are coalesced before they reach Postgres.
type capturingExecer struct{ args []any }

func (c *capturingExecer) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	c.args = args
	return pgconn.CommandTag{}, nil
}

// TestInsertOutboundJobCoalescesNilRecipientArrays pins the NOT-NULL guard on the
// to_addrs/cc_addrs/bcc_addrs columns: a To-only send leaves CC/BCC nil, and pgx
// encodes a nil []string as SQL NULL, which violates the NOT NULL constraint.
// insertOutboundJob must wrap them with nonNil so an empty array reaches the DB.
func TestInsertOutboundJobCoalescesNilRecipientArrays(t *testing.T) {
	rec := &capturingExecer{}
	job := &models.OutboundJob{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		MailFrom: "sender@example.test",
		RcptTo:   []string{"to@example.test"}, // To-only: To/CC/BCC left nil
	}
	if err := insertOutboundJob(context.Background(), rec, job); err != nil {
		t.Fatalf("insertOutboundJob: %v", err)
	}
	n := len(rec.args)
	if n < 3 {
		t.Fatalf("expected bound args, got %d", n)
	}
	for i, name := range []string{"to_addrs", "cc_addrs", "bcc_addrs"} {
		arr, ok := rec.args[n-3+i].([]string)
		if !ok {
			t.Fatalf("%s arg is %T, want []string", name, rec.args[n-3+i])
		}
		if arr == nil {
			t.Fatalf("%s must be a non-nil slice to avoid a NOT NULL violation", name)
		}
	}
}

func TestHashKeyDeterministic(t *testing.T) {
	const raw = "tb_example_key"

	got1 := hashKey(raw)
	got2 := hashKey(raw)
	if got1 != got2 {
		t.Fatalf("expected deterministic hash, got %q and %q", got1, got2)
	}
	if len(got1) != 64 {
		t.Fatalf("expected sha256 hex length 64, got %d", len(got1))
	}
	if got1 == raw {
		t.Fatalf("expected hash to differ from raw input")
	}
}

func TestSchemaSnapshotContainsCurrentStateHardening(t *testing.T) {
	expected := []string{
		"ON DELETE SET NULL",
		"mailboxes_access_password_check",
		"outbox_events_state_check",
		"webhook_deliveries_state_check",
		"ingest_jobs_state_check",
		"claimed_at      TIMESTAMPTZ",
		"lease_until     TIMESTAMPTZ",
		"idx_messages_raw_object_key",
		"idx_ingest_jobs_raw_state",
		"idx_audit_created_at",
		"CREATE TYPE user_role AS ENUM ('super_admin', 'admin', 'user')",
		"WHEN 'platform_admin' THEN 'super_admin'",
		"WHEN 'tenant_admin' THEN 'admin'",
	}
	for _, want := range expected {
		if !strings.Contains(schemaSQL, want) {
			t.Fatalf("schemaSQL missing %q", want)
		}
	}
}
