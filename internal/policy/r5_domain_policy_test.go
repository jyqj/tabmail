package policy

import "testing"

func TestR5DomainPolicyCanonicalRootDot(t *testing.T) {
	for _, tc := range []struct {
		name, domain, pattern string
		match                 bool
	}{
		{"domain_root_dot", "blocked.example.", "blocked.example", true},
		{"pattern_root_dot", "blocked.example", "blocked.example.", true},
		{"both_root_dot", "blocked.example.", "blocked.example.", true},
		{"wildcard_domain_root_dot", "a.blocked.example.", "*.blocked.example", true},
		{"wildcard_pattern_root_dot", "a.blocked.example", "*.blocked.example.", true},
		{"case_and_whitespace", " BLOCKED.Example. ", " blocked.EXAMPLE ", true},
		{"unchanged_exact", "blocked.example", "blocked.example", true},
		{"exact_does_not_match_subdomain", "a.blocked.example.", "blocked.example", false},
		{"exact_does_not_match_suffix", "blocked.example.evil.", "blocked.example", false},
		{"wildcard_does_not_match_parent", "blocked.example.", "*.blocked.example", false},
		{"unrelated", "allowed.example.", "blocked.example", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patterns := []string{tc.pattern}
			if got := ShouldAcceptDomain(tc.domain, false, patterns, nil); got != tc.match {
				t.Errorf("explicit accept=%t, want %t", got, tc.match)
			}
			if got := ShouldAcceptDomain(tc.domain, true, nil, patterns); got != !tc.match {
				t.Errorf("default accept with rejection=%t, want %t", got, !tc.match)
			}
			if got := ShouldStoreDomain(tc.domain, false, patterns, nil); got != tc.match {
				t.Errorf("explicit store=%t, want %t", got, tc.match)
			}
			if got := ShouldStoreDomain(tc.domain, true, nil, patterns); got != !tc.match {
				t.Errorf("default store with discard=%t, want %t", got, !tc.match)
			}
			if got := ShouldRejectOrigin("sender@"+tc.domain, patterns); got != tc.match {
				t.Errorf("origin rejection=%t, want %t", got, tc.match)
			}
		})
	}
}

func TestR5DomainPolicyEmptyAndDefaultControls(t *testing.T) {
	for _, domain := range []string{"", " "} {
		if ShouldAcceptDomain(domain, true, nil, nil) || ShouldAcceptDomain(domain, false, []string{"*"}, nil) {
			t.Errorf("empty/root domain %q accepted", domain)
		}
		if ShouldStoreDomain(domain, true, nil, nil) || ShouldStoreDomain(domain, false, []string{"*"}, nil) {
			t.Errorf("empty/root domain %q stored", domain)
		}
	}
	for _, from := range []string{"", "<>"} {
		if ShouldRejectOrigin(from, []string{"*"}) {
			t.Errorf("no origin domain %q matched a rejection pattern", from)
		}
	}
	// A malformed nonempty origin is not a null reverse path. Normalizing a
	// terminal dot must not turn it into an exemption from a catch-all block.
	if !ShouldRejectOrigin("sender@.", []string{"*"}) {
		t.Fatal("a root-only origin bypassed the existing catch-all block")
	}
	if ShouldAcceptDomain("blocked.example..", false, []string{"blocked.example"}, nil) {
		t.Fatal("normalization repaired multiple empty labels")
	}
	if !ShouldAcceptDomain("allowed.example.", true, nil, nil) || !ShouldStoreDomain("allowed.example.", true, nil, nil) {
		t.Fatal("empty deny list changed the existing default policy")
	}
	if ShouldAcceptDomain("allowed.example.", false, nil, nil) || ShouldStoreDomain("allowed.example.", false, nil, nil) {
		t.Fatal("empty allow list admitted a domain")
	}
}
