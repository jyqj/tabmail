package policy

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

type NamingMode int

const (
	NamingFull NamingMode = iota
	NamingLocal
	NamingDomain
)

var localPartAllowed = regexp.MustCompile(`^[a-z0-9!#$%&'*+\-=/\?^_\.{|}~]+$`)

func ParseNamingMode(s string) (NamingMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "full":
		return NamingFull, nil
	case "local":
		return NamingLocal, nil
	case "domain":
		return NamingDomain, nil
	default:
		return NamingFull, fmt.Errorf("invalid mailbox naming mode %q", s)
	}
}

func NormalizeAddressParts(address string, stripPlus bool) (string, string, error) {
	address = strings.ToLower(strings.TrimSpace(strings.Trim(address, "<>")))
	if address == "" {
		return "", "", errors.New("empty address")
	}
	if address[0] == '@' {
		if idx := strings.IndexRune(address, ':'); idx >= 0 && idx < len(address)-1 {
			address = address[idx+1:]
		}
	}
	at := strings.LastIndex(address, "@")
	if at < 1 || at >= len(address)-1 {
		return "", "", fmt.Errorf("invalid address %q", address)
	}
	local := address[:at]
	domain := address[at+1:]
	if stripPlus {
		if idx := strings.Index(local, "+"); idx > 0 {
			local = local[:idx]
		}
	}
	if err := ValidateLocalPart(local); err != nil {
		return "", "", err
	}
	if !ValidateDomainPart(domain) {
		return "", "", fmt.Errorf("invalid domain part")
	}
	return local, domain, nil
}

func ExtractMailbox(address string, mode NamingMode, stripPlus bool) (string, error) {
	local, domain, err := NormalizeAddressParts(address, stripPlus)
	if err != nil {
		return "", err
	}
	switch mode {
	case NamingFull:
		return local + "@" + domain, nil
	case NamingLocal:
		return local, nil
	case NamingDomain:
		return domain, nil
	default:
		return "", fmt.Errorf("unsupported naming mode")
	}
}

func ValidateLocalPart(local string) error {
	if local == "" {
		return fmt.Errorf("empty local part")
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return fmt.Errorf("invalid dot placement in local part")
	}
	if !localPartAllowed.MatchString(local) {
		return fmt.Errorf("invalid local part")
	}
	return nil
}

func ValidateDomainPart(domain string) bool {
	if domain == "" || len(domain) > 255 {
		return false
	}
	if len(domain) >= 4 && domain[0] == '[' && domain[len(domain)-1] == ']' {
		s := 1
		if strings.HasPrefix(domain[1:], "ipv6:") {
			s = 6
		}
		return net.ParseIP(domain[s:len(domain)-1]) != nil
	}
	if domain[len(domain)-1] != '.' {
		domain += "."
	}
	prev := '.'
	labelLen := 0
	hasAlphaNum := false
	for _, c := range domain {
		switch {
		case ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') || c == '_':
			hasAlphaNum = true
			labelLen++
		case c == '-':
			if prev == '.' || prev == '-' {
				return false
			}
			labelLen++
		case c == '.':
			if prev == '.' || prev == '-' || labelLen > 63 || !hasAlphaNum {
				return false
			}
			labelLen = 0
			hasAlphaNum = false
		default:
			return false
		}
		prev = c
	}
	return true
}
