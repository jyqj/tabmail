package credentials

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestPasswordPolicyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", false}, {"old-registration-minimum", "12345678", false},
		{"eleven", strings.Repeat("a", 11), false}, {"twelve", strings.Repeat("a", 12), true},
		{"maximum", strings.Repeat("a", 72), true}, {"too-long", strings.Repeat("a", 73), false},
		{"unicode-twelve-bytes", strings.Repeat("中", 4), true},
		{"unicode-seventy-two-bytes", strings.Repeat("中", 24), true},
		{"unicode-over-limit", strings.Repeat("中", 25), false},
		{"password-whitespace-is-not-trimmed", " 1234567890 ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if err != nil && !errors.Is(err, ErrPassword) {
				t.Fatal(err)
			}
		})
	}
}

func TestAuditReasonPolicyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
		valid             bool
	}{
		{"whitespace", " \n\t", "", false}, {"seven", "1234567", "", false},
		{"trim-before-minimum", " 1234567 ", "", false},
		{"eight", " 12345678\n", "12345678", true},
		{"maximum", strings.Repeat("a", 1000), strings.Repeat("a", 1000), true},
		{"trim-before-maximum", " \t" + strings.Repeat("a", 1000) + "\n", strings.Repeat("a", 1000), true},
		{"too-long", strings.Repeat("a", 1001), "", false},
		{"unicode-bytes", " 审计理由 ", "审计理由", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AuditReason(tc.value)
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestInvitationIssuer(t *testing.T) {
	a, digest, err := IssueInvitation()
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := IssueInvitation()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(a)
	if err != nil || len(decoded) != InvitationBytes || a == b {
		t.Fatal("invalid or repeated invitation")
	}
	sum := sha256.Sum256([]byte(a))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("digest does not match issued secret")
	}
}
