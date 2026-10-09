package outbound

import (
	"fmt"
	"net/mail"
	"net/netip"
	"strings"
)

// addressLiteralHost returns a network host without SMTP's square brackets or
// IPv6 tag. DNS names remain DNS names, including all-digit dotted domains.
// Preserve the untagged IPv6 form net/mail has historically accepted as well.
func addressLiteralHost(domain string) (string, bool, error) {
	if !strings.ContainsAny(domain, "[]") {
		return "", false, nil
	}
	if len(domain) < 2 || domain[0] != '[' || domain[len(domain)-1] != ']' {
		return "", true, fmt.Errorf("invalid SMTP address literal")
	}
	value := domain[1 : len(domain)-1]
	tagged := len(value) >= 5 && strings.EqualFold(value[:5], "IPv6:")
	if tagged {
		value = value[5:]
	}
	ip, err := netip.ParseAddr(value)
	if err != nil || ip.Zone() != "" || (tagged && !ip.Is6()) {
		return "", true, fmt.Errorf("invalid SMTP address literal")
	}
	return ip.String(), true, nil
}

// net/mail rejects RFC 5321's IPv6 tag even though it accepts the same address
// without that tag. Validate the IP first, let net/mail validate the complete
// mailbox with only the tag removed, and restore it only when the parsed
// domain is exactly the selected literal. Quoted local parts, display names,
// comments, and rejection of trailing/multiple addresses stay with net/mail.
func parseRecipientMailbox(value string) (*mail.Address, error) {
	parsed, originalErr := mail.ParseAddress(value)
	if originalErr == nil {
		return parsed, nil
	}
	// Select the actual domain once. Decoy tags in a display name, quoted
	// local part or comment must not cause a complete reparse for every tag.
	start := recipientDomainStart(value)
	if start < 0 || len(value)-start < 6 || !strings.EqualFold(value[start:start+6], "[IPv6:") {
		return nil, originalErr
	}
	end := strings.IndexByte(value[start+6:], ']')
	if end < 0 {
		return nil, originalErr
	}
	end += start + 6
	literal := value[start : end+1]
	if _, _, err := addressLiteralHost(literal); err != nil {
		return nil, originalErr
	}
	bare := "[" + value[start+6:end] + "]"
	candidate, err := mail.ParseAddress(value[:start] + bare + value[end+1:])
	if err == nil && strings.HasSuffix(candidate.Address, "@"+bare) {
		candidate.Address = strings.TrimSuffix(candidate.Address, bare) + literal
		return candidate, nil
	}
	return nil, originalErr
}

// This scan only locates a possible domain. net/mail remains responsible for
// accepting the complete syntax, including mismatched quotes and comments.
func recipientDomainStart(value string) int {
	at, comments := -1, 0
	quoted, escaped := false, false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if escaped {
			escaped = false
			continue
		}
		if (quoted || comments > 0) && c == '\\' {
			escaped = true
			continue
		}
		if comments > 0 {
			if c == '(' {
				comments++
			} else if c == ')' {
				comments--
			}
			continue
		}
		if c == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		if c == '(' {
			comments++
		} else if c == '@' {
			at = i
		}
	}
	if at < 0 {
		return -1
	}
	start := at + 1
	for start < len(value) && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	return start
}
