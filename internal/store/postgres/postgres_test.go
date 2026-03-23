package postgres

import "testing"

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
