package policy

import (
	"path"
	"strings"
)

func ShouldAcceptDomain(domain string, defaultAccept bool, acceptDomains, rejectDomains []string) bool {
	domain = canonicalPolicyDomain(domain)
	if domain == "" {
		return false
	}
	if defaultAccept {
		return !matchDomainList(domain, rejectDomains)
	}
	return matchDomainList(domain, acceptDomains)
}

func ShouldStoreDomain(domain string, defaultStore bool, storeDomains, discardDomains []string) bool {
	domain = canonicalPolicyDomain(domain)
	if domain == "" {
		return false
	}
	if defaultStore {
		return !matchDomainList(domain, discardDomains)
	}
	return matchDomainList(domain, storeDomains)
}

func matchDomainList(domain string, patterns []string) bool {
	// Compare both spellings of one terminal root dot without rewriting the
	// glob. Trimming a pattern can turn an escaped dot into an invalid escape,
	// and a character class or wildcard may match the dot itself.
	// Literals and names with empty labels do not get a rooted spelling. Keep
	// the parser's existing accepted label characters, including UTF-8.
	rootedDomain := ""
	if domain != "" && !strings.ContainsAny(domain, "[]") &&
		!strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".") && !strings.Contains(domain, "..") {
		rootedDomain = domain + "."
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if ok, _ := path.Match(pattern, domain); ok {
			return true
		}
		if rootedDomain != "" {
			if ok, _ := path.Match(pattern, rootedDomain); ok {
				return true
			}
		}
	}
	return false
}

// Normalize a domain value, never a glob pattern. DNS names may carry one
// terminal root dot; resolver identities are not changed by policy matching.
// Do not turn a root-only name into an empty origin or repair empty labels.
func canonicalPolicyDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if len(domain) > 1 && strings.HasSuffix(domain, ".") && !strings.HasSuffix(domain, "..") {
		domain = strings.TrimSuffix(domain, ".")
	}
	return domain
}
