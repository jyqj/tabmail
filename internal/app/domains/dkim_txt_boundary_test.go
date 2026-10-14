package domainapp

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func TestTriggerVerifyDKIMRejectsAmbiguousPublicKey(t *testing.T) {
	privateKey, publicKey, err := tabdkim.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, txt string
		want      bool
	}{
		{"revocation before matching key", "v=DKIM1; p=; p=" + publicKey, false},
		{"duplicate version", "v=DKIM0; v=DKIM1; p=" + publicKey, false},
		{"malformed tag", "v=DKIM1; broken; p=" + publicKey, false},
		{"current unambiguous key", tabdkim.DNSTXTValue(publicKey), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			tenant := &models.Tenant{ID: uuid.New(), Name: "synthetic-company"}
			st.SeedTenant(tenant)
			zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "example.test", TXTRecord: "tabmail-verify=synthetic", DKIMPrivateKeyPEM: &privateKey, DKIMSelector: "mail", DKIMEnabled: true}
			st.SeedZone(zone)
			svc := NewService(st, nil, "mx.example.test", nil, zerolog.Nop())
			svc.SetResolvers(func(name string) ([]string, error) {
				if name == tabdkim.DNSRecordName(zone.DKIMSelector, zone.Domain) {
					return []string{tc.txt}, nil
				}
				return []string{zone.TXTRecord, "v=spf1 include:example.test"}, nil
			}, func(string) ([]*net.MX, error) {
				return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
			})
			got, checks, err := svc.TriggerVerify(context.Background(), adminActor(tenant.ID), zone.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.DKIMEnabled != tc.want || (checks.DKIM.Status == "pass") != tc.want {
				t.Fatalf("DKIM enabled=%v status=%s; want verified=%v", got.DKIMEnabled, checks.DKIM.Status, tc.want)
			}
		})
	}
}
