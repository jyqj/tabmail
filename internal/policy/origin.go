package policy

import "strings"

func ShouldRejectOrigin(from string, rejectDomains []string) bool {
	if from == "" || from == "<>" || len(rejectDomains) == 0 {
		return false
	}
	from = strings.TrimSpace(strings.Trim(from, "<>"))
	at := strings.LastIndex(from, "@")
	if at < 0 || at >= len(from)-1 {
		return false
	}
	return matchDomainList(canonicalPolicyDomain(from[at+1:]), rejectDomains)
}
