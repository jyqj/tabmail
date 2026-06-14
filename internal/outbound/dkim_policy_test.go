package outbound

import (
	"testing"

	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/models"
)

func TestDKIMFailClosedDefaultsSecure(t *testing.T) {
	if !NewService(config.Outbound{}, nil, zerolog.Nop()).dkimFailClosed() {
		t.Fatal("empty DKIM fail policy should default to fail-closed")
	}
	if !NewService(config.Outbound{DKIMFailPolicy: " FAIL_CLOSED "}, nil, zerolog.Nop()).dkimFailClosed() {
		t.Fatal("fail_closed should be fail-closed case-insensitively")
	}
	if NewService(config.Outbound{DKIMFailPolicy: config.DKIMFailOpen}, nil, zerolog.Nop()).dkimFailClosed() {
		t.Fatal("explicit fail_open should allow unsigned delivery on signing failure")
	}
	if !NewService(config.Outbound{DKIMFailPolicy: "unexpected"}, nil, zerolog.Nop()).dkimFailClosed() {
		t.Fatal("invalid DKIM fail policy should fail closed before config validation catches it")
	}
}

func TestDKIMSendBlockReason(t *testing.T) {
	key := "-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----"
	signing := NewService(config.Outbound{DKIMSign: true}, nil, zerolog.Nop())
	notSigning := NewService(config.Outbound{DKIMSign: false}, nil, zerolog.Nop())

	t.Run("zone does not require dkim is always allowed", func(t *testing.T) {
		if r := notSigning.DKIMSendBlockReason(&models.DomainZone{DKIMRequiredForSend: false}); r != "" {
			t.Fatalf("expected no block, got %q", r)
		}
	})
	t.Run("nil zone is allowed", func(t *testing.T) {
		if r := signing.DKIMSendBlockReason(nil); r != "" {
			t.Fatalf("expected no block for nil zone, got %q", r)
		}
	})
	t.Run("required but global signing disabled is blocked", func(t *testing.T) {
		zone := &models.DomainZone{DKIMRequiredForSend: true, DKIMEnabled: true, DKIMPrivateKeyPEM: &key}
		if r := notSigning.DKIMSendBlockReason(zone); r == "" {
			t.Fatal("expected block when signing disabled but zone requires DKIM")
		}
	})
	t.Run("required but zone has no key is blocked", func(t *testing.T) {
		zone := &models.DomainZone{DKIMRequiredForSend: true, DKIMEnabled: false, DKIMPrivateKeyPEM: nil}
		if r := signing.DKIMSendBlockReason(zone); r == "" {
			t.Fatal("expected block when zone requires DKIM but has no key configured")
		}
	})
	t.Run("required and fully configured is allowed", func(t *testing.T) {
		zone := &models.DomainZone{DKIMRequiredForSend: true, DKIMEnabled: true, DKIMPrivateKeyPEM: &key}
		if r := signing.DKIMSendBlockReason(zone); r != "" {
			t.Fatalf("expected no block when DKIM fully configured, got %q", r)
		}
	})
}
