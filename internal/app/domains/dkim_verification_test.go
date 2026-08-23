package domainapp

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestTriggerVerifyDKIMRequiresCurrentPublicKey(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant-a"}
	st.SeedTenant(tenant)

	privPEM, pubB64, err := tabdkim.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair current: %v", err)
	}
	_, stalePubB64, err := tabdkim.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair stale: %v", err)
	}

	zone := &models.DomainZone{
		ID:                uuid.New(),
		TenantID:          tenant.ID,
		Domain:            "example.test",
		TXTRecord:         "tabmail-verify=ok",
		DKIMPrivateKeyPEM: &privPEM,
		DKIMSelector:      "mail",
		DKIMEnabled:       true,
	}
	st.SeedZone(zone)

	dkimHost := tabdkim.DNSRecordName("mail", zone.Domain)
	dkimTXT := tabdkim.DNSTXTValue(stalePubB64)
	svc := NewService(st, nil, "mx.example.test", policy.NamingFull, "secret", nil, zerolog.Nop())
	svc.SetResolvers(
		func(name string) ([]string, error) {
			switch name {
			case zone.Domain:
				return []string{zone.TXTRecord, "v=spf1 include:example.test"}, nil
			case dkimHost:
				return []string{dkimTXT}, nil
			default:
				return nil, nil
			}
		},
		func(name string) ([]*net.MX, error) {
			if name == zone.Domain {
				return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
			}
			return nil, nil
		},
	)

	got, checks, err := svc.TriggerVerify(ctx, adminActor(tenant.ID), zone.ID)
	if err != nil {
		t.Fatalf("TriggerVerify stale DKIM: %v", err)
	}
	if checks.DKIM.Status == "pass" || got.DKIMEnabled {
		t.Fatalf("stale DKIM public key must fail and disable DKIM: status=%s enabled=%v", checks.DKIM.Status, got.DKIMEnabled)
	}

	dkimTXT = tabdkim.DNSTXTValue(pubB64)
	got, checks, err = svc.TriggerVerify(ctx, adminActor(tenant.ID), zone.ID)
	if err != nil {
		t.Fatalf("TriggerVerify current DKIM: %v", err)
	}
	if checks.DKIM.Status != "pass" || !got.DKIMEnabled {
		t.Fatalf("current DKIM public key should pass and enable DKIM: status=%s enabled=%v", checks.DKIM.Status, got.DKIMEnabled)
	}
}
