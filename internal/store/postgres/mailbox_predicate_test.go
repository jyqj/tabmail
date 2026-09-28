package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"testing"
	"time"
)

// Exercise SQL, not substrings: mailbox lists, outbound receipts and submission
// lists must agree with the Go read decision when submitter visibility cannot
// independently admit the viewer.
func TestAR02ReadableMailboxQueryParity(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	viewer := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"client@example.test"}, To: []string{"client@example.test"}, Subject: "read eligibility", State: models.OutboundPending, RecipientLedger: true}
	must(t, f.st.CreateOutboundJob(ctx, j))
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name       string
		owner      *uuid.UUID
		read, send bool
		expires    *time.Time
	}{
		{"ungranted", &f.employee.ID, false, false, nil},
		{"owner", &f.other.ID, false, false, nil},
		{"read-grant", &f.employee.ID, true, false, nil},
		{"send-only", &f.employee.ID, false, true, nil},
		{"expired-owner", &f.other.ID, false, false, &past},
		{"expired-grant", &f.employee.ID, true, true, &past},
		{"future-grant", &f.employee.ID, true, false, &future},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.pool.Exec(ctx, `DELETE FROM mailbox_grants WHERE mailbox_id=$1 AND user_id=$2`, f.personal.ID, viewer.ID)
			must(t, err)
			_, err = f.pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2,expires_at=$3 WHERE id=$1`, f.personal.ID, tc.owner, tc.expires)
			must(t, err)
			var grant *models.MailboxGrant
			if tc.read || tc.send {
				grant = &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: viewer.ID, CanRead: tc.read, CanSend: tc.send}
				must(t, f.st.SetMailboxGrant(ctx, grant))
			}
			mb, err := f.st.GetMailbox(ctx, f.personal.ID)
			must(t, err)
			want := authz.EvaluateMailboxAccess(viewer, mb, grant).CanRead
			boxes, _, err := f.st.ListMailboxesScoped(ctx, authz.ZoneListFilter{TenantID: f.tenant.ID, AllZones: true, GrantedUserID: &viewer.ID}, models.Page{})
			must(t, err)
			gotBox := false
			for _, v := range boxes {
				if v.ID == mb.ID {
					gotBox = true
				}
			}
			jobs, _, err := f.st.ListOutboundJobsScoped(ctx, authz.OwnerListFilter{TenantID: f.tenant.ID, UserID: &viewer.ID, ReaderUserID: &viewer.ID}, models.Page{})
			must(t, err)
			gotJob := false
			for _, v := range jobs {
				if v.ID == j.ID {
					gotJob = true
				}
			}
			subs, _, err := f.st.ListSubmissions(ctx, viewer, models.Page{})
			must(t, err)
			gotSub := false
			for _, v := range subs {
				if v.ID == j.ID {
					gotSub = true
				}
			}
			if gotBox != want || gotJob != want || gotSub != want {
				t.Fatalf("Go read=%v; SQL mailbox=%v job=%v submission=%v", want, gotBox, gotJob, gotSub)
			}
		})
	}
}
