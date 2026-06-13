package outbound

import (
	"testing"

	"github.com/rs/zerolog"

	"tabmail/internal/config"
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
