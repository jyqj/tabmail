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
// expected RSA public key. The public-key comparison is exact after removing
// DNS/display whitespace from the p= tag; other tag values are parsed
// case-insensitively where DKIM allows it.
func TXTValueMatchesPublicKey(txtValue, publicKeyBase64 string) bool {
	tags := parseDKIMTags(txtValue)
	if !strings.EqualFold(tags["v"], "DKIM1") {
		return false
	}
	if k := strings.TrimSpace(tags["k"]); k != "" && !strings.EqualFold(k, "rsa") {
		return false
	}
	return stripWhitespace(tags["p"]) == stripWhitespace(publicKeyBase64)
}

func parseDKIMTags(txtValue string) map[string]string {
	tags := make(map[string]string)
	for _, part := range strings.Split(txtValue, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		tags[key] = strings.TrimSpace(value)
	}
	return tags
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
