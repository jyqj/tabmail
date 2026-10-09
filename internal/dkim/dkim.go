package dkim

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"unicode"

	"github.com/emersion/go-msgauth/dkim"
)

const DefaultSelector = "default"

// GenerateKeyPair generates an RSA-2048 keypair.
// Returns PEM-encoded private key and base64 public key for DNS.
func GenerateKeyPair() (privateKeyPEM string, publicKeyBase64 string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate RSA key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal public key: %w", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pubDER)

	return string(privPEM), pubB64, nil
}

// PublicKeyFromPEM extracts the base64-encoded public key from a PEM private key.
func PublicKeyFromPEM(privatePEM string) (string, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(pubDER), nil
}

// DNSTXTValue returns the full DKIM DNS TXT record value.
func DNSTXTValue(publicKeyBase64 string) string {
	return "v=DKIM1; k=rsa; p=" + publicKeyBase64
}

// TXTValueMatchesPublicKey reports whether a DKIM DNS TXT value contains the
// expected RSA public key and permits this signer's SHA-256 email signatures.
// The public-key comparison is exact after removing DNS/display whitespace
// from the p= tag; existing version and key-type display handling is preserved.
func TXTValueMatchesPublicKey(txtValue, publicKeyBase64 string) bool {
	expected := stripWhitespace(publicKeyBase64)
	if expected == "" {
		return false
	}
	tags := parseDKIMTags(txtValue)
	if !strings.EqualFold(tags["v"], "DKIM1") {
		return false
	}
	if k, present := tags["k"]; present && !strings.EqualFold(k, "rsa") {
		return false
	}
	// RFC 6376 section 3.6.1: omitted h=/s= allow all supported hashes and
	// services; explicit lists must allow the hash and service we actually use.
	// Their identifiers are case-sensitive, and only s= defines a wildcard.
	if h, present := tags["h"]; present && !tagListContains(h, "sha256") {
		return false
	}
	if s, present := tags["s"]; present && !tagListContains(s, "email") && !tagListContains(s, "*") {
		return false
	}
	return stripWhitespace(tags["p"]) == expected
}

func tagListContains(value, expected string) bool {
	for _, item := range strings.Split(value, ":") {
		if strings.TrimSpace(item) == expected {
			return true
		}
	}
	return false
}

// A malformed or repeated tag invalidates the record. In particular, a later
// p= must not overwrite an earlier revocation or a different public key.
// Preserve this matching helper's existing display whitespace/case handling;
// it is not a replacement for the DKIM signature verifier.
func parseDKIMTags(txtValue string) map[string]string {
	tags := make(map[string]string)
	parts := strings.Split(txtValue, ";")
	for i, part := range parts {
		if strings.TrimSpace(part) == "" && i == len(parts)-1 {
			break // One trailing semicolon is allowed.
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if !validTagName(key) {
			return nil
		}
		if _, exists := tags[key]; exists {
			return nil
		}
		tags[key] = strings.TrimSpace(value)
	}
	return tags
}

func validTagName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		ch := name[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

func stripWhitespace(v string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, v)
}

// DNSRecordName returns the DNS record name for DKIM.
func DNSRecordName(selector, domain string) string {
	return selector + "._domainkey." + domain
}

// SignMessage signs a MIME message with DKIM.
// Returns the signed message (DKIM-Signature header prepended).
func SignMessage(rawMIME []byte, domain, selector, privateKeyPEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	opts := &dkim.SignOptions{
		Domain:   domain,
		Selector: selector,
		Signer:   key,
		Hash:     crypto.SHA256,
		HeaderKeys: []string{
			"From", "To", "Cc", "Subject", "Date",
			"Message-ID", "MIME-Version", "Content-Type",
		},
	}

	var signed bytes.Buffer
	if err := dkim.Sign(&signed, bytes.NewReader(rawMIME), opts); err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}

	return signed.Bytes(), nil
}
