package domainapp

import (
	"bytes"
	"context"
	"net"
	"slices"
	"testing"

	msgauthdkim "github.com/emersion/go-msgauth/dkim"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func progressDKIMSignedMail(t *testing.T) (string, string, []byte) {
	t.Helper()
	privateKey, publicKey, err := tabdkim.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: sender@example.test\r\nTo: recipient@example.test\r\nSubject: Controlled DKIM readiness\r\nDate: Thu, 08 Oct 2026 00:00:00 +0000\r\nMessage-ID: <readiness@example.test>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nSynthetic message body\r\n")
	signed, err := tabdkim.SignMessage(raw, "example.test", "mail", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, publicKey, signed
}

func progressDKIMVerify(t *testing.T, signed []byte, records []string, lookupErr error) bool {
	t.Helper()
	verifications, err := msgauthdkim.VerifyWithOptions(bytes.NewReader(signed), &msgauthdkim.VerifyOptions{
		LookupTXT: func(name string) ([]string, error) {
			if name != "mail._domainkey.example.test" {
				t.Errorf("unexpected verifier DNS query: %q", name)
				return nil, &net.DNSError{Err: "unexpected synthetic query", Name: name}
			}
			return records, lookupErr
		},
	})
	if err != nil || len(verifications) != 1 {
		t.Fatalf("shipping signer did not produce one verifiable signature: verifications=%d err=%v", len(verifications), err)
	}
	if verifications[0].Domain != "example.test" {
		t.Fatalf("signature used a different domain: %q", verifications[0].Domain)
	}
	return verifications[0].Err == nil
}

// The verifier is real: only DNS is controlled. This control proves that a
// successful result checks the signed bytes, not just signature/key syntax.
func TestProgressDKIMVerifierChecksSignedBytes(t *testing.T) {
	_, publicKey, signed := progressDKIMSignedMail(t)
	for _, tampered := range []bool{false, true} {
		name := "original"
		if tampered {
			name = "changed body"
		}
		t.Run(name, func(t *testing.T) {
			message := signed
			if tampered {
				message = bytes.Replace(signed, []byte("Synthetic message body"), []byte("Different message body"), 1)
			}
			if got := progressDKIMVerify(t, message, []string{tabdkim.DNSTXTValue(publicKey)}, nil); got == tampered {
				t.Fatalf("signature verification=%v after tampering=%v", got, tampered)
			}
		})
	}
}

func TestProgressDKIMReadinessMatchesSignedMail(t *testing.T) {
	privateKey, publicKey, signed := progressDKIMSignedMail(t)
	_, otherPublicKey, err := tabdkim.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	current, other := tabdkim.DNSTXTValue(publicKey), tabdkim.DNSTXTValue(otherPublicKey)
	lookupErr := &net.DNSError{Err: "controlled temporary failure", Name: "mail._domainkey.example.test", IsTimeout: true, IsTemporary: true}
	for _, tc := range []struct {
		name    string
		records []string
		err     error
		want    bool
	}{
		{"implicit hash and service", []string{current}, nil, true},
		{"explicit hash and service", []string{current + "; h=sha256; s=email"}, nil, true},
		{"hash alternatives", []string{current + "; h=sha1:sha256"}, nil, true},
		{"unknown hash alongside allowed hash", []string{current + "; h=future:sha256"}, nil, true},
		{"service alternatives", []string{current + "; s=other:email"}, nil, true},
		{"wildcard service", []string{current + "; s=*"}, nil, true},
		{"wildcard in service list", []string{current + "; s=other:*"}, nil, true},
		{"list whitespace", []string{current + "; h=sha1 \t: \tsha256; s=other \t: \temail"}, nil, true},
		{"sha1 only", []string{current + "; h=sha1"}, nil, false},
		{"unknown hash only", []string{current + "; h=future"}, nil, false},
		{"empty hash list", []string{current + "; h="}, nil, false},
		{"hash value is case sensitive", []string{current + "; h=SHA256"}, nil, false},
		{"hash has no wildcard", []string{current + "; h=*"}, nil, false},
		{"other service only", []string{current + "; s=other"}, nil, false},
		{"empty service list", []string{current + "; s="}, nil, false},
		{"service value is case sensitive", []string{current + "; s=EMAIL"}, nil, false},
		{"allowed hash denied service", []string{current + "; h=sha256; s=other"}, nil, false},
		{"denied hash allowed service", []string{current + "; h=sha1; s=email"}, nil, false},
		{"duplicate current records", []string{current, current}, nil, false},
		{"current then old key", []string{current, other}, nil, false},
		{"old then current key", []string{other, current}, nil, false},
		{"current then revoked key", []string{current, "v=DKIM1; p="}, nil, false},
		{"revoked then current key", []string{"v=DKIM1; p=", current}, nil, false},
		{"current then unrelated TXT", []string{current, "unrelated=synthetic"}, nil, false},
		{"no records", nil, nil, false},
		{"old key only", []string{other}, nil, false},
		{"revoked key only", []string{"v=DKIM1; p="}, nil, false},
		{"lookup error without records", nil, lookupErr, false},
		{"lookup error with current record", []string{current}, lookupErr, false},
		{"explicit restrictions with trailing separator", []string{current + "; h=sha256; s=email; \t"}, nil, true},
		{"default RSA with explicit restrictions", []string{"v=DKIM1; p=" + publicKey + "; h=sha256; s=email"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if verified := progressDKIMVerify(t, signed, tc.records, tc.err); verified != tc.want {
				t.Fatalf("real verifier result=%v, expected=%v", verified, tc.want)
			}
			ctx := context.Background()
			st := testutil.NewFakeStore()
			tenant := &models.Tenant{ID: uuid.New(), Name: "synthetic-company"}
			st.SeedTenant(tenant)
			zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "example.test", TXTRecord: "tabmail-verify=synthetic", DKIMPrivateKeyPEM: &privateKey, DKIMSelector: "mail", DKIMEnabled: true}
			st.SeedZone(zone)
			records, dnsErr := tc.records, tc.err
			svc := NewService(st, nil, "mx.example.test", nil, zerolog.Nop())
			svc.SetResolvers(func(name string) ([]string, error) {
				switch name {
				case "mail._domainkey.example.test":
					return records, dnsErr
				case zone.Domain:
					return []string{zone.TXTRecord, "v=spf1 include:example.test"}, nil
				default:
					return nil, nil
				}
			}, func(string) ([]*net.MX, error) {
				return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
			})
			actor := adminActor(tenant.ID)
			status, err := svc.VerificationStatus(ctx, actor, zone.ID)
			if err != nil {
				t.Fatal(err)
			}
			if status.DKIMEnabled != tc.want || (status.Checks.DKIM.Status == "pass") != tc.want {
				t.Errorf("status disagrees with signed mail: enabled=%v check=%s want=%v", status.DKIMEnabled, status.Checks.DKIM.Status, tc.want)
			}
			before, err := st.GetZone(ctx, zone.ID)
			if err != nil || before == nil || !before.DKIMEnabled {
				t.Fatalf("read-only verification status changed persisted readiness: zone=%v err=%v", before, err)
			}
			got, checks, err := svc.TriggerVerify(ctx, actor, zone.ID)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := st.GetZone(ctx, zone.ID)
			if err != nil || stored == nil {
				t.Fatalf("cannot read persisted verification: zone=%v err=%v", stored, err)
			}
			if got.DKIMEnabled != tc.want || stored.DKIMEnabled != tc.want || (checks.DKIM.Status == "pass") != tc.want {
				t.Errorf("verification persisted unusable key: returned=%v stored=%v check=%s want=%v", got.DKIMEnabled, stored.DKIMEnabled, checks.DKIM.Status, tc.want)
			}
			if !stored.IsVerified || !stored.MXVerified {
				t.Error("DKIM qualification changed independent ownership or MX results")
			}
			for _, record := range tc.records {
				if !slices.Contains(checks.DKIM.Details, record) {
					t.Error("verification discarded a DNS diagnostic record")
				}
			}
			if tc.err != nil && !slices.Contains(checks.DKIM.Details, tc.err.Error()) {
				t.Error("verification discarded the DNS failure diagnostic")
			}
			// A corrected single record must enable signing again. This guards
			// against sticky failure state after a previously enabled key fails.
			records, dnsErr = []string{current}, nil
			got, checks, err = svc.TriggerVerify(ctx, actor, zone.ID)
			stored, storedErr := st.GetZone(ctx, zone.ID)
			if err != nil || storedErr != nil || got == nil || stored == nil || !got.DKIMEnabled || !stored.DKIMEnabled || checks.DKIM.Status != "pass" {
				t.Fatalf("healthy DNS repair did not restore DKIM readiness: err=%v storedErr=%v", err, storedErr)
			}
		})
	}
}
