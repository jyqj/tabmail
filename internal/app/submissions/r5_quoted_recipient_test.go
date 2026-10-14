package submissions

import (
	"context"
	"reflect"
	"testing"

	"tabmail/internal/models"
)

// Exercise the actual application entry point used by free and draft submit.
// The existing fake exposes the atomic enqueue boundary; no DB transaction or
// real draft consumption is claimed by these controls.
func TestR5QuotedRecipientAuthorizedSubmission(t *testing.T) {
	for _, tc := range []struct{ name, address, envelope, identity string }{
		{"quoted_at", `Desk <"Team@Desk"@Recipient.test>`, `"team@desk"@recipient.test`, "team@desk@recipient.test"},
		{"quoted_space", `"Team Desk"@Recipient.test`, `"team desk"@recipient.test`, "team desk@recipient.test"},
		{"ordinary_control", `Desk <Team@Recipient.test>`, "team@recipient.test", "team@recipient.test"},
	} {
		for role, roleName := range []string{"to", "cc", "bcc"} {
			for _, suppressed := range []bool{false, true} {
				name := tc.name + "/" + roleName + "/allowed"
				if suppressed {
					name = tc.name + "/" + roleName + "/suppressed"
				}
				t.Run(name, func(t *testing.T) {
					f, st := r5SuppressionAddressFixture(t)
					if suppressed {
						if err := st.AddSuppression(context.Background(), &models.SuppressionEntry{
							TenantID: f.tenant.ID, Address: tc.identity, Reason: "synthetic hard bounce",
						}); err != nil {
							t.Fatal(err)
						}
					}
					in, backing := r5SuppressionAddressInput(f, role, tc.address)
					before := append([]string(nil), backing...)
					job, replay, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), in)
					if suppressed {
						if failure == nil || failure.Kind != FailureBadRequest || job != nil || replay {
							t.Fatalf("suppressed quote identity=%+v job=%v replay=%v", failure, job, replay)
						}
						r5RequireNoSubmissionEnqueue(t, f, st)
					} else {
						if failure != nil || job == nil || replay || st.enqueueCalls != 1 {
							t.Fatalf("valid quoted recipient rejected: failure=%+v job=%v replay=%v enqueues=%d", failure, job != nil, replay, st.enqueueCalls)
						}
						if job.RcptTo[role] != tc.envelope {
							t.Errorf("persisted recipient=%q want=%q", job.RcptTo[role], tc.envelope)
						}
					}
					want := []string{"to@example.test", "copy@example.test", "hidden@example.test"}
					want[role] = tc.identity
					if suppressed {
						want = want[:role+1]
					}
					if !reflect.DeepEqual(st.lookups, want) || !reflect.DeepEqual(backing, before) {
						t.Errorf("suppression or caller identity changed: lookups=%q want=%q backing=%q", st.lookups, want, backing)
					}
				})
			}
		}
	}
}
