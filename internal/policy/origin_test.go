package policy

import "testing"

func TestShouldRejectOrigin(t *testing.T) {
	tests := []struct {
		from     string
		patterns []string
		want     bool
	}{
		{"sender@example.com", []string{"example.com"}, true},
		{"sender@example.com", []string{"*.com"}, true},
		{"sender@sub.example.com", []string{"*.example.com"}, true},
		{"sender@example.com", []string{"gmail.com"}, false},
		{"<>", []string{"*"}, false},
	}
	for _, tt := range tests {
		if got := ShouldRejectOrigin(tt.from, tt.patterns); got != tt.want {
			t.Fatalf("ShouldRejectOrigin(%q, %v) = %v, want %v", tt.from, tt.patterns, got, tt.want)
		}
	}
}
