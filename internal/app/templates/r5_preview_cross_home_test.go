package templates

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// These cases exercise the real Preview service. Only repository replies are
// controlled; transaction fencing itself remains a PostgreSQL concern.
func crossHomePreviewFixture(published bool) *previewBoundaryFixture {
	f := newPreviewBoundaryFixture(published)
	f.actor.Role, f.actor.IsSuperAdmin = models.RoleSuperAdmin, true
	version := int64(7)
	f.actor.SessionVersion = &version
	f.repo.actor = f.actor
	f.repo.user.TenantID = uuid.New()
	f.repo.user.Role, f.repo.user.SessionVersion = models.RoleSuperAdmin, version
	f.repo.user.DisplayName = "Home-tenant operator"
	f.repo.settings.Name = "Selected company"
	f.repo.employee, f.repo.company = f.repo.user.DisplayName, f.repo.settings.Name
	return f
}

func TestR5PreviewCrossHomeCurrentManagement(t *testing.T) {
	for _, mode := range []string{"selected-company", "fresh-role-over-stale-flags", "same-home-admin"} {
		t.Run(mode, func(t *testing.T) {
			f := crossHomePreviewFixture(false)
			switch mode {
			case "fresh-role-over-stale-flags":
				f.actor.Role, f.actor.IsSuperAdmin = models.RoleUser, false
				f.repo.actor = f.actor
			case "same-home-admin":
				f.repo.user.TenantID, f.repo.user.Role = f.actor.TenantID, models.RoleAdmin
				f.actor.Role, f.actor.IsSuperAdmin, f.actor.IsAdmin = models.RoleAdmin, false, true
				f.repo.actor = f.actor
			}
			got, err := f.svc.Preview(context.Background(), f.actor, f.input)
			want := &Rendered{Subject: "Hello Customer", TextBody: "Home-tenant operator at Selected company from sender@company.test", HTMLBody: "<p>Customer</p>"}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("current management preview: got=%+v err=%v want=%+v", got, err, want)
			}
			if f.repo.mailbox.CanRead || f.repo.mailbox.CanSend || f.repo.counts["version"] != 0 || f.repo.counts["mailbox"] != 3 || f.repo.counts["user"] != 2 || f.repo.counts["settings"] != 2 {
				t.Fatalf("management bypassed source/final checks or required content rights: counts=%v mailbox=%+v", f.repo.counts, f.repo.mailbox)
			}
		})
	}
}

func TestR5PreviewCrossHomeRejectsStaleIdentity(t *testing.T) {
	for _, mode := range []string{"ordinary-admin-foreign", "ordinary-user-foreign", "inactive-superadmin", "wrong-session", "noninteractive"} {
		t.Run(mode, func(t *testing.T) {
			f := crossHomePreviewFixture(false)
			switch mode {
			case "ordinary-admin-foreign":
				f.repo.user.Role = models.RoleAdmin
			case "ordinary-user-foreign":
				f.repo.user.Role = models.RoleUser
			case "inactive-superadmin":
				f.repo.user.IsActive = false
			case "wrong-session":
				f.repo.user.SessionVersion++
			case "noninteractive":
				f.actor.Type = authz.PrincipalAPIKey
				f.repo.actor = f.actor
			}
			f.assertRejected(t, context.Background())
			if f.repo.counts["user"] != 1 || f.repo.counts["settings"] != 0 {
				t.Fatalf("invalid current identity reached company data: %v", f.repo.counts)
			}
		})
	}
}

func TestR5PreviewCrossHomeRechecksBeforeRelease(t *testing.T) {
	for _, change := range []string{"demoted", "frozen", "session-changed", "company-changed", "management-revoked", "mailbox-renamed"} {
		t.Run(change, func(t *testing.T) {
			f := crossHomePreviewFixture(false)
			reached := false
			f.repo.hook = func(port string, call int) {
				switch {
				case change == "demoted" && port == "user" && call == 2:
					reached = true
					f.repo.user.Role = models.RoleAdmin
				case change == "frozen" && port == "user" && call == 2:
					reached = true
					f.repo.user.IsActive = false
				case change == "session-changed" && port == "user" && call == 2:
					reached = true
					f.repo.user.SessionVersion++
				case change == "company-changed" && port == "settings" && call == 2:
					reached = true
					f.repo.settings.TenantID = uuid.New()
				case change == "management-revoked" && port == "mailbox" && call == 3:
					reached = true
					f.repo.mailbox.CanManage = false
				case change == "mailbox-renamed" && port == "mailbox" && call == 3:
					reached = true
					f.repo.mailbox.Mailbox.FullAddress = "renamed@company.test"
				}
			}
			f.assertRejected(t, context.Background())
			if !reached {
				t.Fatalf("legitimate initial cross-home preview never reached its final %s guard: %v", change, f.repo.counts)
			}
		})
	}
}

func TestR5PreviewCrossHomePublishedCapabilitiesStaySeparate(t *testing.T) {
	for _, mode := range []string{"management-without-send", "published-use", "published-grant-revoked"} {
		t.Run(mode, func(t *testing.T) {
			f := crossHomePreviewFixture(true)
			f.repo.mailbox.CanManage = true
			switch mode {
			case "management-without-send":
				f.repo.mailbox.CanSend = false
			case "published-grant-revoked":
				f.repo.hook = func(port string, call int) {
					if port == "version" && call == 2 {
						f.repo.faults[port] = errors.New("published grant revoked")
					}
				}
			}
			got, err := f.svc.Preview(context.Background(), f.actor, f.input)
			if mode == "published-use" {
				if err != nil || got == nil || got.TextBody != "Home-tenant operator at Selected company from sender@company.test" {
					t.Fatalf("existing published use changed: %+v %v", got, err)
				}
			} else if err == nil || got != nil {
				t.Fatalf("management authority bypassed published rights: %+v %v", got, err)
			}
			wantVersions := 2
			if mode == "management-without-send" {
				wantVersions = 0
			}
			if f.repo.counts["version"] != wantVersions || f.repo.counts["user"] != 0 || f.repo.counts["settings"] != 0 {
				t.Fatalf("published preview changed source contract: %v", f.repo.counts)
			}
		})
	}
}
