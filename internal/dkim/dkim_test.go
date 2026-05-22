package dkim

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/emersion/go-msgauth/dkim"
)

const testMessage = "From: test@example.com\r\nTo: user@example.com\r\nSubject: Test\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <test@example.com>\r\n\r\nHello"

func TestGenerateKeyPair(t *testing.T) {
	privPEM, pubB64, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}
	if privPEM == "" {
		t.Fatal("private key PEM is empty")
	}
	if pubB64 == "" {
		t.Fatal("public key base64 is empty")
	}

	// Verify PEM is parseable
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}
	_, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
}

func TestPublicKeyFromPEM(t *testing.T) {
	privPEM, pubB64, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}

	got, err := PublicKeyFromPEM(privPEM)
	if err != nil {
		t.Fatalf("PublicKeyFromPEM error: %v", err)
	}
	if got != pubB64 {
		t.Fatalf("public key mismatch:\ngot:  %s\nwant: %s", got, pubB64)
	}
}

func TestSignMessage(t *testing.T) {
	privPEM, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}

	signed, err := SignMessage([]byte(testMessage), "example.com", DefaultSelector, privPEM)
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}

	if !strings.Contains(string(signed), "DKIM-Signature") {
		t.Fatal("signed message does not contain DKIM-Signature header")
	}
}

func TestSignMessageVerify(t *testing.T) {
	privPEM, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}

	signed, err := SignMessage([]byte(testMessage), "example.com", DefaultSelector, privPEM)
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}

	verifications, err := dkim.Verify(bytes.NewReader(signed))
	if err != nil {
		t.Fatalf("dkim.Verify error: %v", err)
	}

	if len(verifications) == 0 {
		t.Fatal("no verifications returned")
	}

	// The verification will have an error because there's no DNS to look up the
	// public key, but the signature itself should be parseable (err will be about
	// DNS lookup failure, not a malformed signature).
	for _, v := range verifications {
		if v.Domain != "example.com" {
			t.Errorf("unexpected domain: %s", v.Domain)
		}
	}
}

func TestDNSTXTValue(t *testing.T) {
	val := DNSTXTValue("AAAA")
	expected := "v=DKIM1; k=rsa; p=AAAA"
	if val != expected {
		t.Fatalf("DNSTXTValue = %q, want %q", val, expected)
	}
}

func TestDNSRecordName(t *testing.T) {
	name := DNSRecordName("default", "example.com")
	expected := "default._domainkey.example.com"
	if name != expected {
		t.Fatalf("DNSRecordName = %q, want %q", name, expected)
	}
}
