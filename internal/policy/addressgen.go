package policy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	AddressSuggestionAlgorithm = "obfuscated_minute_bucket_v1"
	addressAlphabet            = "abcdefghijklmnopqrstuvwxyz0123456789"
	addressRandomBodyLength    = 12
	addressTimeCodeLength      = 6
	subdomainRandomBodyLength  = 10
	subdomainTimeCodeLength    = 5
)

func GenerateSuggestedAddress(now time.Time, domain, secret string) (string, string, error) {
	domain = normalizeDomain(domain)
	local, err := generateStructuredToken(now, "mailbox|"+domain, secret, addressRandomBodyLength, addressTimeCodeLength, rand.Reader)
	if err != nil {
		return "", "", err
	}
	return local, local + "@" + domain, nil
}

func GenerateSuggestedSubdomainAddress(now time.Time, parentDomain, secret string) (string, string, error) {
	parentDomain = normalizeDomain(parentDomain)
	label, err := generateStructuredToken(now, "subdomain|"+parentDomain, secret, subdomainRandomBodyLength, subdomainTimeCodeLength, rand.Reader)
	if err != nil {
		return "", "", err
	}
	return label, label + "." + parentDomain, nil
}

func generateSuggestedLocalPart(now time.Time, domain, secret string, r io.Reader) (string, error) {
	return generateStructuredToken(now, "mailbox|"+normalizeDomain(domain), secret, addressRandomBodyLength, addressTimeCodeLength, r)
}

func generateStructuredToken(now time.Time, scope, secret string, randomLen, timeCodeLen int, r io.Reader) (string, error) {
	domain := normalizeDomain(strings.TrimPrefix(scope, "mailbox|"))
	if strings.HasPrefix(scope, "subdomain|") {
		domain = normalizeDomain(strings.TrimPrefix(scope, "subdomain|"))
	}
	domain = normalizeDomain(domain)
	if !ValidateDomainPart(domain) {
		return "", fmt.Errorf("invalid domain part")
	}
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("empty address generation secret")
	}
	body, err := randomString(r, randomLen, addressAlphabet)
	if err != nil {
		return "", err
	}
	timeCode := obfuscatedMinuteCode(now.UTC().Unix()/60, scope, secret, timeCodeLen)
	pos := insertionIndex(body, scope, secret)
	local := body[:pos] + timeCode + body[pos:]
	if err := ValidateLocalPart(local); err != nil {
		return "", err
	}
	return local, nil
}

func obfuscatedMinuteCode(bucket int64, scope, secret string, width int) string {
	modulus := int64(powInt(len(addressAlphabet), width))
	a, b := affineParams(scope, secret, modulus)
	value := (a*(bucket%modulus) + b) % modulus
	return encodeFixedBase36(value, width)
}

func insertionIndex(body, scope, secret string) int {
	if len(body) <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(secret + "|" + scope + "|" + body))
	return 1 + int(sum[0])%(len(body)-1)
}

func affineParams(scope, secret string, modulus int64) (int64, int64) {
	sum := sha256.Sum256([]byte(secret + "|" + scope))
	a := int64(binary.BigEndian.Uint64(sum[:8]) % uint64(modulus))
	if a == 0 {
		a = 5
	}
	for a%2 == 0 || a%3 == 0 {
		a++
	}
	b := int64(binary.BigEndian.Uint64(sum[8:16]) % uint64(modulus))
	return a, b
}

func encodeFixedBase36(v int64, width int) string {
	if width <= 0 {
		return ""
	}
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = addressAlphabet[v%36]
		v /= 36
	}
	return string(buf)
}

func randomString(r io.Reader, n int, alphabet string) (string, error) {
	if n <= 0 {
		return "", nil
	}
	if len(alphabet) == 0 {
		return "", fmt.Errorf("empty alphabet")
	}
	buf := make([]byte, n)
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", err
	}
	for i := 0; i < n; i++ {
		buf[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(buf), nil
}

func normalizeDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func powInt(base, exp int) int {
	out := 1
	for i := 0; i < exp; i++ {
		out *= base
	}
	return out
}
