package smtp

import (
	"context"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"testing"
)

func TestSMTPConfiguredTLSDoesNotFallBackToPlaintext(t *testing.T) {
	for _, cfg := range []config.SMTP{{Addr: "127.0.0.1:0", TLSEnabled: true, TLSCert: "missing-cert.pem", TLSKey: "missing-key.pem"}, {Addr: "127.0.0.1:0", ForceTLS: true}} {
		s := NewServer(cfg, nil, nil, zerolog.Nop())
		if e := s.Start(context.Background()); e == nil {
			t.Fatal("SMTP started with unsatisfied TLS policy")
		}
	}
}
