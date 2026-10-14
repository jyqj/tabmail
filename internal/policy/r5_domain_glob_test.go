package policy

import "testing"

// Patterns are path.Match expressions. A terminal root dot can be expressed
// with an escape or a character class, so it must not be stripped as text.
func TestR5DomainPolicyRootDotGlobSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, domain, pattern string
		match                 bool
	}{
		{"escaped_dot_rooted", "blocked.example.", `blocked.example\.`, true},
		{"escaped_dot_plain", "blocked.example", `blocked.example\.`, true},
		{"class_dot_rooted", "blocked.example.", `blocked.example[.]`, true},
		{"class_dot_plain", "blocked.example", `blocked.example[.]`, true},
		{"wildcard_escaped_dot", "sub.blocked.example.", `*.blocked.example\.`, true},
		{"question_matches_root_dot", "blocked.example.", `blocked.example?`, true},
		{"ordinary_wildcard", "sub.blocked.example.", `*.blocked.example`, true},
		{"ordinary_rooted_wildcard", "sub.blocked.example", `*.blocked.example.`, true},
		{"case_and_space_keep_escape", " BLOCKED.Example. ", ` BLOCKED.EXAMPLE\. `, true},
		{"exact_keeps_subdomain_boundary", "sub.blocked.example.", `blocked.example\.`, false},
		{"wildcard_keeps_parent_boundary", "blocked.example.", `*.blocked.example\.`, false},
		{"unrelated", "allowed.example.", `blocked.example[.]`, false},
		{"invalid_escape", "blocked.example.", `blocked.example\`, false},
		{"invalid_class", "blocked.example.", `blocked.example[`, false},
		{"literal_exact", "[192.0.2.7]", `\[192.0.2.7\]`, true},
		{"literal_has_no_root_dot_alias", "[192.0.2.7]", `\[192.0.2.7\]\.`, false},
		{"root_only_remains_nonempty", ".", `*`, true},
		{"multiple_dots_not_repaired", "blocked.example..", `blocked.example\.`, false},
		{"utf8_root_dot", "例子.测试.", `例子.测试\.`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patterns := []string{tc.pattern}
			if got := ShouldRejectOrigin("sender@"+tc.domain, patterns); got != tc.match {
				t.Errorf("origin rejection=%t, want %t", got, tc.match)
			}
			if got := ShouldAcceptDomain(tc.domain, true, nil, patterns); got != !tc.match {
				t.Errorf("recipient deny=%t, want %t", !got, tc.match)
			}
			if got := ShouldStoreDomain(tc.domain, true, nil, patterns); got != !tc.match {
				t.Errorf("discard=%t, want %t", !got, tc.match)
			}
			if got := ShouldAcceptDomain(tc.domain, false, patterns, nil); got != tc.match {
				t.Errorf("explicit accept=%t, want %t", got, tc.match)
			}
			if got := ShouldStoreDomain(tc.domain, false, patterns, nil); got != tc.match {
				t.Errorf("explicit store=%t, want %t", got, tc.match)
			}
		})
	}
}
