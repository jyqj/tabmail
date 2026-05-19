package postgres

import (
	"strings"
	"testing"
)

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
	}
	for _, want := range expected {
		if !strings.Contains(schemaSQL, want) {
			t.Fatalf("schemaSQL missing %q", want)
		}
	}
}
