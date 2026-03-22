package policy

import (
	"path"
	"strings"
)

func ShouldRejectOrigin(from string, rejectDomains []string) bool {
	if from == "" || from == "<>" || len(rejectDomains) == 0 {
		return false
	}
	from = strings.TrimSpace(strings.Trim(from, "<>"))
	at := strings.LastIndex(from, "@")
	if at < 0 || at >= len(from)-1 {
		return false
	}
	domain := strings.ToLower(from[at+1:])
	for _, pattern := range rejectDomains {
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
