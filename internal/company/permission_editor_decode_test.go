package company

import (
	"strings"
	"testing"
)

const patchTestRevision = `"expected_revision":{"user_id":"11111111-1111-4111-8111-111111111111","tenant_id":"22222222-2222-4222-8222-222222222222","user_revision":"9007199254740993","profile_id":null,"profile_revision":null}`

func TestPermissionEditorDecodeIntent(t *testing.T) {
	cmd, err := DecodePermissionEditorCommand(strings.NewReader(`{` + patchTestRevision + `,"patch":{"can_send":false,"daily_send_quota":0,"max_domains":null,"domain_access":{"mode":"none","zone_ids":[]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cmd.Patch.CanSend.Present || cmd.Patch.CanSend.Inherit || cmd.Patch.CanSend.Value {
		t.Fatal("explicit false lost")
	}
	if !cmd.Patch.DailySendQuota.Present || cmd.Patch.DailySendQuota.Inherit || cmd.Patch.DailySendQuota.Value != 0 {
		t.Fatal("explicit zero lost")
	}
	if !cmd.Patch.MaxDomains.Present || !cmd.Patch.MaxDomains.Inherit {
		t.Fatal("null must inherit")
	}
	if cmd.Patch.MaxMailboxes.Present {
		t.Fatal("omitted must remain absent")
	}
	if cmd.Patch.DomainAccess.Value.Mode != "none" {
		t.Fatal("denial mode lost")
	}
	if cmd.ExpectedRevision.UserRevision != "9007199254740993" {
		t.Fatal("revision precision lost")
	}
}
func TestPermissionEditorDecodeRejectsAmbiguity(t *testing.T) {
	for _, patch := range []string{
		`{"unknown":1}`, `{"Can_Send":true}`, `{"can_send":false,"can_send":true}`,
		`{"can_send":1}`, `{"daily_send_quota":-1}`, `{"daily_send_quota":1.5}`,
		`{"daily_send_quota":"0"}`, `{"max_domains":9223372036854775808}`,
		`{"domain_access":{"mode":"none","unknown":true}}`,
		`{"domain_access":{"mode":"none","Mode":"all"}}`,
		`{"domain_access":{"mode":"list","zone_ids":[]}}`,
		`{"domain_access":{"mode":"all","zone_ids":["11111111-1111-4111-8111-111111111111"]}}`,
		`{"domain_access":{"mode":"list","zone_ids":["bogus"]}}`,
		`{"domain_access":{"mode":"list","zone_ids":["00000000-0000-0000-0000-000000000000"]}}`,
		`{"domain_access":{"mode":"list","zone_ids":["11111111-1111-4111-8111-111111111111","11111111-1111-4111-8111-111111111111"]}}`,
		`{"domain_access":{"mode":"bogus"}}`,
	} {
		t.Run(patch, func(t *testing.T) {
			if _, err := DecodePermissionEditorCommand(strings.NewReader(`{` + patchTestRevision + `,"patch":` + patch + `}`)); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	for _, input := range []string{`null`, `[]`, `{}`, `{` + patchTestRevision + `,"patch":null}`, `{` + patchTestRevision + `,"patch":{}} {}`, `{` + patchTestRevision + `,"patch":{},"Patch":{"can_send":true}}`} {
		if _, err := DecodePermissionEditorCommand(strings.NewReader(input)); err == nil {
			t.Fatalf("invalid envelope accepted: %s", input)
		}
	}
}
func TestPermissionEditorDecodeEmptyAndDomainModes(t *testing.T) {
	for _, patch := range []string{`{}`, `{"domain_access":null}`, `{"domain_access":{"mode":"inherit"}}`, `{"domain_access":{"mode":"all","zone_ids":null}}`, `{"domain_access":{"mode":"none"}}`} {
		if _, err := DecodePermissionEditorCommand(strings.NewReader(`{` + patchTestRevision + `,"patch":` + patch + `}`)); err != nil {
			t.Fatalf("valid intent %s: %v", patch, err)
		}
	}
}

func TestPermissionEditorDecodeRequiresCompleteObservation(t *testing.T) {
	for _, part := range []string{`"profile_id":null,`, `,"profile_revision":null`, `"user_id":"11111111-1111-4111-8111-111111111111",`, `"tenant_id":"22222222-2222-4222-8222-222222222222",`, `"user_revision":"9007199254740993",`} {
		input := `{` + strings.Replace(patchTestRevision, part, "", 1) + `,"patch":{}}`
		if _, err := DecodePermissionEditorCommand(strings.NewReader(input)); err == nil {
			t.Fatalf("omitted observation accepted: %s", part)
		}
	}
}
func TestPermissionAssignmentDecodeExplicitIntent(t *testing.T) {
	for _, fields := range []string{`"profile_id":null,"profile_revision":null`, `"profile_id":"33333333-3333-4333-8333-333333333333","profile_revision":"7"`} {
		cmd, err := DecodePermissionAssignmentCommand(strings.NewReader(`{` + patchTestRevision + `,` + fields + `,"patch":{"can_send":false,"max_domains":null}}`))
		if err != nil {
			t.Fatal(err)
		}
		if !cmd.Patch.CanSend.Present || cmd.Patch.CanSend.Inherit || cmd.Patch.CanSend.Value || !cmd.Patch.MaxDomains.Inherit {
			t.Fatal("assignment patch intent lost")
		}
	}
}
func TestPermissionAssignmentDecodeRejectsMissingOrAmbiguousIntent(t *testing.T) {
	fields := `"profile_id":null,"profile_revision":null`
	valid := `{` + patchTestRevision + `,` + fields + `,"patch":{}}`
	for _, input := range []string{
		`{` + patchTestRevision + `,"patch":{}}`,
		`{"profile_id":null,"profile_revision":null,"patch":{}}`,
		`{` + patchTestRevision + `,"profile_revision":null,"patch":{}}`,
		`{` + patchTestRevision + `,"profile_id":null,"patch":{}}`,
		strings.Replace(valid, `,"patch":{}`, "", 1),
		strings.Replace(valid, fields, `"profile_id":"33333333-3333-4333-8333-333333333333","profile_revision":null`, 1),
		strings.Replace(valid, fields, `"profile_id":null,"profile_revision":"1"`, 1),
		strings.Replace(valid, fields, `"profile_id":"00000000-0000-0000-0000-000000000000","profile_revision":"1"`, 1),
		strings.Replace(valid, fields, `"profile_id":"33333333-3333-4333-8333-333333333333","profile_revision":"01"`, 1),
		strings.Replace(valid, fields, `"profile_id":"bad","profile_revision":"1"`, 1),
		strings.Replace(valid, fields, fields+`,"Profile_ID":null`, 1),
		strings.Replace(valid, fields, fields+`,"profile_id":null`, 1),
		strings.Replace(valid, fields, fields+`,"unexpected":true`, 1),
		strings.Replace(valid, `"patch":{}`, `"patch":{"can_send":true,"can_send":false}`, 1),
		strings.Replace(valid, `"patch":{}`, `"patch":{"domain_access":{"mode":"none","Mode":"all"}}`, 1),
		valid + `{}`, `null`, `[]`,
	} {
		if _, err := DecodePermissionAssignmentCommand(strings.NewReader(input)); err == nil {
			t.Fatalf("invalid assignment accepted: %s", input)
		}
	}
}
