package policy

import (
	"path"
	"strings"
)

func ShouldAcceptDomain(domain string, defaultAccept bool, acceptDomains, rejectDomains []string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if defaultAccept {
		return !matchDomainList(domain, rejectDomains)
	}
	return matchDomainList(domain, acceptDomains)
}

func ShouldStoreDomain(domain string, defaultStore bool, storeDomains, discardDomains []string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if defaultStore {
		return !matchDomainList(domain, discardDomains)
	}
	return matchDomainList(domain, storeDomains)
}

func matchDomainList(domain string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if ok, _ := path.Match(pattern, domain); ok {
			return true
		}
	}
	return false
}
