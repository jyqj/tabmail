package company_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/company"
)

func TestPermissionPatchFieldIntent(t *testing.T) {
	for _, tc := range []struct {
		name, raw        string
		present, inherit bool
		quota            int
		send             bool
	}{
		{"omitted", `{}`, false, false, 0, false},
		{"inherit", `{"can_send":null,"daily_send_quota":null}`, true, true, 0, false},
		{"false-zero", `{"can_send":false,"daily_send_quota":0}`, true, false, 0, false},
		{"explicit", `{"can_send":true,"daily_send_quota":31}`, true, false, 31, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p company.PermissionPatch
			if err := json.Unmarshal([]byte(tc.raw), &p); err != nil {
				t.Fatal(err)
			}
			if p.CanSend.Present != tc.present || p.CanSend.Inherit != tc.inherit || p.CanSend.Value != tc.send || p.DailySendQuota.Present != tc.present || p.DailySendQuota.Inherit != tc.inherit || p.DailySendQuota.Value != tc.quota {
				t.Fatalf("field intent collapsed: %+v", p)
			}
			if err := p.Validate(); err != nil {
				t.Fatal(err)
			}
			if p.Empty() == tc.present {
				t.Fatal("empty patch detection lost omission")
			}
		})
	}
}
func TestPermissionPatchRejectsMalformedQuotas(t *testing.T) {
	for _, raw := range []string{`{"daily_send_quota":-1}`, `{"daily_send_quota":0.5}`, `{"daily_send_quota":"0"}`, `{"daily_receive_quota":-1}`, `{"max_mailboxes":-1}`, `{"max_domains":-1}`} {
		t.Run(raw, func(t *testing.T) {
			var p company.PermissionPatch
			err := json.Unmarshal([]byte(raw), &p)
			if err == nil {
				err = p.Validate()
			}
			if err == nil {
				t.Fatal("malformed quota accepted")
			}
		})
	}
}
func TestPermissionDomainAccessIntent(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		name  string
		value company.DomainAccess
		valid bool
	}{
		{"inherit", company.DomainAccess{Mode: "inherit"}, true},
		{"all", company.DomainAccess{Mode: "all", ZoneIDs: []uuid.UUID{}}, true},
		{"none", company.DomainAccess{Mode: "none", ZoneIDs: []uuid.UUID{}}, true},
		{"list", company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{id}}, true},
		{"empty-list", company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{}}, false},
		{"all-with-list", company.DomainAccess{Mode: "all", ZoneIDs: []uuid.UUID{id}}, false},
		{"none-with-list", company.DomainAccess{Mode: "none", ZoneIDs: []uuid.UUID{id}}, false},
		{"inherit-with-list", company.DomainAccess{Mode: "inherit", ZoneIDs: []uuid.UUID{id}}, false},
		{"zero-id", company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{uuid.Nil}}, false},
		{"duplicate", company.DomainAccess{Mode: "list", ZoneIDs: []uuid.UUID{id, id}}, false},
		{"unknown", company.DomainAccess{Mode: "allow"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.value.Validate() == nil) != tc.valid {
				t.Fatal("ambiguous domain intent accepted or valid intent rejected")
			}
		})
	}
	var p company.PermissionPatch
	if err := json.Unmarshal([]byte(`{"domain_access":{"mode":"list","zone_ids":["not-a-uuid"]}}`), &p); err == nil {
		t.Fatal("invalid UUID accepted")
	}
}
func TestPermissionRevisionExactObservation(t *testing.T) {
	id, tenant, pid := uuid.New(), uuid.New(), uuid.New()
	rev := "9007199254740993"
	base := company.PermissionRevision{UserID: id, TenantID: tenant, UserRevision: rev, ProfileID: &pid, ProfileRevision: &rev}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	if !base.Equal(base) {
		t.Fatal("same observation not equal")
	}
	for _, field := range []string{"user", "tenant", "user-revision", "profile", "profile-revision", "profile-presence"} {
		x := base
		switch field {
		case "user":
			x.UserID = uuid.New()
		case "tenant":
			x.TenantID = uuid.New()
		case "user-revision":
			x.UserRevision = "9007199254740994"
		case "profile":
			v := uuid.New()
			x.ProfileID = &v
		case "profile-revision":
			v := "9007199254740994"
			x.ProfileRevision = &v
		case "profile-presence":
			x.ProfileID = nil
			x.ProfileRevision = nil
		}
		if base.Equal(x) {
			t.Fatalf("compound revision ignores %s", field)
		}
	}
	for _, s := range []string{"", "0", "-1", "01", "1.0", "9223372036854775808"} {
		x := base
		x.UserRevision = s
		if x.Validate() == nil {
			t.Fatalf("invalid decimal accepted: %q", s)
		}
	}
	x := base
	x.ProfileRevision = nil
	if x.Validate() == nil {
		t.Fatal("unpaired profile identity accepted")
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var restored company.PermissionRevision
	if err = json.Unmarshal(raw, &restored); err != nil || !base.Equal(restored) {
		t.Fatalf("decimal revision lost in JSON roundtrip: %s %v", raw, err)
	}
}
