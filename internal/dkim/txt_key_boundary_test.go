package dkim

import "testing"

func TestTXTKeyMatchRejectsRevokedAndAmbiguousRecords(t *testing.T) {
	const key = "c3ludGhldGljLXB1YmxpYy1rZXk="
	for _, tc := range []struct {
		name, record, expected string
	}{
		{"empty key", "v=DKIM1; p=", ""},
		{"missing key", "v=DKIM1; k=rsa", ""},
		{"whitespace key", "v=DKIM1; p= \t", " \t"},
		{"revocation before key", "v=DKIM1; p=; p=" + key, key},
		{"different key before current", "v=DKIM1; p=b2xk; p=" + key, key},
		{"identical repeated key", "v=DKIM1; p=" + key + "; p=" + key, key},
		{"repeated version", "v=DKIM0; v=DKIM1; p=" + key, key},
		{"repeated key type", "v=DKIM1; k=ed25519; k=rsa; p=" + key, key},
		{"repeated extension", "v=DKIM1; n=first; n=second; p=" + key, key},
		{"empty key type", "v=DKIM1; k=; p=" + key, key},
		{"missing assignment", "v=DKIM1; broken; p=" + key, key},
		{"empty tag", "v=DKIM1; =broken; p=" + key, key},
		{"empty middle field", "v=DKIM1;; p=" + key, key},
		{"invalid tag name", "v=DKIM1; 1invalid=x; p=" + key, key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if TXTValueMatchesPublicKey(tc.record, tc.expected) {
				t.Fatal("revoked or ambiguous key record matched the configured key")
			}
		})
	}
}

func TestTXTKeyMatchPreservesUnambiguousCompatibility(t *testing.T) {
	const key = "c3ludGhldGljLXB1YmxpYy1rZXk="
	for _, tc := range []struct {
		name, record string
		want         bool
	}{
		{"generated format", DNSTXTValue(key), true},
		{"default key type", "v=DKIM1; p=" + key, true},
		{"trailing separator", "v=DKIM1; p=" + key + "; \t", true},
		{"known extension", "v=DKIM1; n=one=two; p=" + key, true},
		{"unknown extension", "v=DKIM1; x_custom_1=metadata; p=" + key, true},
		{"existing reordered display", " k = rsa ; p = c3ludGhl \tdGljLXB1YmxpYy1rZXk= ; v = DKIM1 ", true},
		{"different key", DNSTXTValue("b2xk"), false},
		{"revoked configured key", "v=DKIM1; p=", false},
		{"missing version", "p=" + key, false},
		{"other algorithm", "v=DKIM1; k=ed25519; p=" + key, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TXTValueMatchesPublicKey(tc.record, key); got != tc.want {
				t.Fatalf("key match = %v, want %v", got, tc.want)
			}
		})
	}
}
