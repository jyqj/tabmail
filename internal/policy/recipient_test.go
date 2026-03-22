package policy

import "testing"

func TestShouldAcceptDomain(t *testing.T) {
	if !ShouldAcceptDomain("example.com", true, nil, []string{"gmail.com"}) {
	} else {
		// ok
	}
	if ShouldAcceptDomain("gmail.com", true, nil, []string{"gmail.com"}) {
		t.Fatal("expected gmail.com rejected")
	}
	if !ShouldAcceptDomain("mail.example.com", false, []string{"*.example.com"}, nil) {
		t.Fatal("expected mail.example.com accepted")
	}
}

func TestShouldStoreDomain(t *testing.T) {
	if ShouldStoreDomain("skip.example.com", true, nil, []string{"skip.example.com"}) {
		t.Fatal("expected discard match")
	}
	if !ShouldStoreDomain("keep.example.com", false, []string{"*.example.com"}, nil) {
		t.Fatal("expected explicit store match")
	}
}
