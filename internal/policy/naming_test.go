package policy

import "testing"

func TestExtractMailbox(t *testing.T) {
	tests := []struct {
		address   string
		mode      NamingMode
		stripPlus bool
		want      string
	}{
		{"alice@example.com", NamingFull, true, "alice@example.com"},
		{"alice+tag@example.com", NamingFull, true, "alice@example.com"},
		{"alice+tag@example.com", NamingLocal, true, "alice"},
		{"alice@example.com", NamingDomain, true, "example.com"},
	}
	for _, tt := range tests {
		got, err := ExtractMailbox(tt.address, tt.mode, tt.stripPlus)
		if err != nil {
			t.Fatalf("ExtractMailbox(%q) unexpected error: %v", tt.address, err)
		}
		if got != tt.want {
			t.Fatalf("ExtractMailbox(%q) = %q, want %q", tt.address, got, tt.want)
		}
	}
}

func TestValidateLocalPart(t *testing.T) {
	if err := ValidateLocalPart("alice+tag"); err != nil {
		t.Fatalf("expected local part valid, got %v", err)
	}
	if err := ValidateLocalPart("alice<>"); err == nil {
		t.Fatal("expected invalid local part")
	}
	if _, _, err := NormalizeAddressParts("@mx.example.com:alice+tag@example.com", true); err != nil {
		t.Fatalf("expected route form accepted, got %v", err)
	}
	if ValidateDomainPart("bad..example.com") {
		t.Fatal("expected invalid domain")
	}
}
