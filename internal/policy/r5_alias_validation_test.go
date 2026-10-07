package policy

import "testing"

func TestR5PlusTagCannotHideInvalidLocalPart(t *testing.T) {
	for _, local := range []string{
		"alice+tag..bad",
		"alice+tag.",
		"alice+tag bad",
		"alice+tag\tbad",
		"alice+tag\r\nbad",
		"alice+tag@other",
		"alice+tag<bad",
		"alice+tag\x00bad",
		"alice+标签",
	} {
		t.Run(local, func(t *testing.T) {
			for _, stripPlus := range []bool{false, true} {
				gotLocal, gotDomain, err := NormalizeAddressParts(local+"@example.test", stripPlus)
				if err == nil || gotLocal != "" || gotDomain != "" {
					t.Errorf("stripPlus=%t normalized malformed local %q to %q@%q, error %v", stripPlus, local, gotLocal, gotDomain, err)
				}
			}
		})
	}
}

func TestR5PlusTagPreservesValidAddressSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, address, want string
		stripPlus           bool
	}{
		{"ordinary_tag", "Alice+News@example.test", "alice", true},
		{"dotted_tag", "alice+news.daily@example.test", "alice", true},
		{"repeated_plus", "alice++daily@example.test", "alice", true},
		{"empty_tag", "alice+@example.test", "alice", true},
		{"leading_plus_is_local", "+tag@example.test", "+tag", true},
		{"disabled", "alice+news.daily@example.test", "alice+news.daily", false},
		{"source_route", "@mx.example.test:alice+news@example.test", "alice", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, domain, err := NormalizeAddressParts(tc.address, tc.stripPlus)
			if err != nil || local != tc.want || domain != "example.test" {
				t.Fatalf("got %q@%q, %v; want %q@example.test", local, domain, err, tc.want)
			}
		})
	}
	// A valid original dot-string may still produce an invalid mailbox base.
	// The original and derived local parts both have to satisfy naming policy.
	if _, _, err := NormalizeAddressParts("alice.+tag@example.test", true); err == nil {
		t.Fatal("stripping a tag produced an invalid trailing-dot mailbox")
	}
	if local, _, err := NormalizeAddressParts("alice.+tag@example.test", false); err != nil || local != "alice.+tag" {
		t.Fatalf("disabled stripping changed a valid dot-string: %q, %v", local, err)
	}
}
