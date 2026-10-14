package policy

import (
	"strings"
	"testing"
)

func TestR5DomainInteriorHyphens(t *testing.T) {
	label63 := strings.Repeat("a", 30) + "--" + strings.Repeat("b", 31)
	for _, domain := range []string{
		"a--b.example",
		"a---b.example",
		"1--2.example",
		"xn--bcher-kva.example",
		"mail.xn--bcher-kva.example",
		"xn--bcher-kva.example.",
		label63 + ".example",
	} {
		t.Run(domain, func(t *testing.T) {
			if !ValidateDomainPart(domain) {
				t.Fatalf("valid interior-hyphen domain rejected: %q", domain)
			}
		})
	}
	local, domain, err := NormalizeAddressParts("User+Tag@XN--BCHER-KVA.EXAMPLE", true)
	if err != nil || local != "user" || domain != "xn--bcher-kva.example" {
		t.Fatalf("normalization did not preserve the valid ASCII domain: %q@%q, %v", local, domain, err)
	}
}

func TestR5DomainHyphenBoundaryRejections(t *testing.T) {
	for _, domain := range []string{
		"-a.example", "--a.example", "a-.example", "a--.example",
		"a.-b.example", "a.b-.example", "xn--.example",
		"a--b..example", "a--b.example..",
		strings.Repeat("a", 31) + "--" + strings.Repeat("b", 31) + ".example",
		"bücher.example",
	} {
		t.Run(domain, func(t *testing.T) {
			if ValidateDomainPart(domain) {
				t.Fatalf("invalid domain admitted: %q", domain)
			}
		})
	}
	for _, domain := range []string{"a-b.example", "1.example", "example.test.", "[127.0.0.1]", "[ipv6:::1]"} {
		if !ValidateDomainPart(domain) {
			t.Errorf("existing valid domain rejected: %q", domain)
		}
	}
}
