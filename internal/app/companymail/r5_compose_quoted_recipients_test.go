package companymail

import (
	"bytes"
	"context"
	"net/mail"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// This consumes the actual MIME/compose result through the real authorized
// submission service and MIME builder. Persistence alone uses the existing fake;
// there is no SMTP send, PostgreSQL transaction, or stored-draft consumption.
func requireComposedRecipientsSubmit(t *testing.T, repo *contentRepo, actor authz.Actor, payload *company.DraftPayload, wantTo, wantCC []string) {
	t.Helper()
	if !reflect.DeepEqual(payload.To, wantTo) || !reflect.DeepEqual(payload.CC, wantCC) || len(payload.BCC) != 0 {
		t.Errorf("composed roles lost canonical addresses: To=%q want=%q CC=%q want=%q BCC=%d", payload.To, wantTo, payload.CC, wantCC, len(payload.BCC))
	}
	for _, address := range append(append([]string(nil), payload.To...), payload.CC...) {
		if _, err := outbound.ParseRecipientAddress(address); err != nil {
			t.Errorf("composed recipient cannot enter the send pipeline: %q: %v", address, err)
		}
	}
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: actor.TenantID, Name: "synthetic-compose-company"}
	st.SeedTenant(tenant)
	user := &models.User{ID: actor.ID, TenantID: tenant.ID, Email: "me@company.test", Role: models.RoleUser, IsActive: true}
	if err := st.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "company.test", IsVerified: true, MXVerified: true}
	st.SeedZone(zone)
	mailbox := repo.access.Mailbox
	mailbox.ZoneID, mailbox.OwnerUserID = zone.ID, &user.ID
	st.SeedMailbox(&mailbox)
	actor.Permission = &models.EffectivePermission{CanSend: true, DailySendQuota: 100}
	out := outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	submitter := submissions.NewService(nil, st, out, zerolog.Nop())
	job, replay, failure := submitter.SubmitAuthorized(context.Background(), tenant, actor, submissions.SubmitInput{
		From: mailbox.FullAddress, To: payload.To, CC: payload.CC, BCC: payload.BCC,
		Subject: payload.Subject, TextBody: payload.TextBody, HTMLBody: payload.HTMLBody, Headers: payload.Headers,
	})
	if failure != nil || replay || job == nil {
		t.Fatalf("valid generated reply could not be submitted: failure=%+v replay=%v job=%v", failure, replay, job != nil)
	}
	if !reflect.DeepEqual(job.To, wantTo) || !reflect.DeepEqual(job.CC, wantCC) || len(job.BCC) != 0 {
		t.Fatal("submission changed composed recipient roles or identity")
	}
	persisted, err := st.GetOutboundJob(context.Background(), job.ID)
	if err != nil || persisted == nil || !reflect.DeepEqual(persisted.RcptTo, append(append([]string{}, wantTo...), wantCC...)) {
		t.Fatal("reply did not retain the expected complete envelope at the enqueue boundary")
	}
	raw, err := outbound.Build(outbound.Message{From: job.MailFrom, To: job.To, CC: job.CC, BCC: job.BCC,
		Subject: job.Subject, TextBody: job.TextBody, HTMLBody: job.HTMLBody, Headers: payload.Headers})
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct {
		name string
		want []string
	}{{"To", wantTo}, {"Cc", wantCC}} {
		if len(role.want) == 0 {
			if message.Header.Get(role.name) != "" {
				t.Fatalf("unexpected %s header", role.name)
			}
			continue
		}
		addresses, err := message.Header.AddressList(role.name)
		if err != nil || len(addresses) != len(role.want) {
			t.Fatalf("reply MIME has invalid %s recipients: %v", role.name, err)
		}
		for i, address := range addresses {
			want, err := outbound.ParseRecipientAddress(role.want[i])
			if err != nil || strings.ToLower(address.Address) != want.Identity {
				t.Fatalf("MIME changed %s recipient identity", role.name)
			}
		}
	}
	if message.Header.Get("Bcc") != "" || strings.Contains(string(raw), "original-secret@client.test") {
		t.Fatal("original Bcc entered the generated reply")
	}
}

func TestR5ComposeQuotedRecipientsRemainSendable(t *testing.T) {
	for _, address := range []struct{ name, header, envelope string }{
		{"quoted_at", `Desk <"Ops@Desk"@Client.test>`, `"ops@desk"@client.test`},
		{"quoted_comma", `Desk <"Ops,Desk"@Client.test>`, `"ops,desk"@client.test`},
		{"quoted_space", `"Ops Desk"@Client.test`, `"ops desk"@client.test`},
		{"quoted_escape", `"Ops\"Desk"@Client.test`, `"ops\"desk"@client.test`},
		{"quoted_backslash", `"Ops\\Desk"@Client.test`, `"ops\\desk"@client.test`},
		{"ordinary", `Desk <Ops@Client.test>`, "ops@client.test"},
		{"unicode", `员工@Client.test`, "员工@client.test"},
	} {
		for _, source := range []string{"from", "reply_to", "reply_all_to", "reply_all_cc"} {
			t.Run(address.name+"/"+source, func(t *testing.T) {
				s, repo, objects, actor, mailbox, message := contentFixture()
				mode, headers := "reply", "From: "+address.header+"\r\n"
				wantTo, wantCC := []string{address.envelope}, []string{}
				switch source {
				case "reply_to":
					headers = "From: ignored@client.test\r\nReply-To: " + address.header + "\r\n"
				case "reply_all_to":
					mode = "reply_all"
					headers = "From: author@client.test\r\nTo: me@company.test, " + address.header + "\r\nCc: copy@client.test\r\n"
					wantTo, wantCC = []string{"author@client.test", address.envelope}, []string{"copy@client.test"}
				case "reply_all_cc":
					mode = "reply_all"
					headers = "From: author@client.test\r\nTo: me@company.test\r\nCc: " + address.header + "\r\n"
					wantTo, wantCC = []string{"author@client.test"}, []string{address.envelope}
				}
				objects.raw = []byte(headers + "Bcc: original-secret@client.test\r\nSubject: synthetic reply\r\nMessage-ID: <quoted-source@client.test>\r\n\r\nsynthetic body")
				payload, err := s.Compose(context.Background(), actor, mailbox, message, mailbox, mode)
				if err != nil || payload == nil {
					t.Fatalf("valid quoted original rejected: %v", err)
				}
				if !objects.closed || len(*repo.events) != 0 || payload.Headers["In-Reply-To"] != "<quoted-source@client.test>" {
					t.Fatal("reply changed source ownership, thread identity, or attachment side effects")
				}
				requireComposedRecipientsSubmit(t, repo, actor, payload, wantTo, wantCC)
			})
		}
	}
}

func TestR5ComposeQuotedRecipientsKeepIdentityDeduplication(t *testing.T) {
	s, repo, objects, actor, mailbox, message := contentFixture()
	objects.raw = []byte("From: ignored@client.test\r\n" +
		`Reply-To: Desk <"Ops@Desk"@Client.test>` + "\r\n" +
		`To: me@company.test, "ME"@COMPANY.TEST, "ops@desk"@client.test, "Sales,West"@Client.test` + "\r\n" +
		`Cc: "OPS@DESK"@CLIENT.TEST, "sales,west"@client.test, "Ops Desk"@Client.test` + "\r\n" +
		"Bcc: original-secret@client.test\r\nSubject: synthetic reply\r\n\r\nsynthetic body")
	payload, err := s.Compose(context.Background(), actor, mailbox, message, mailbox, "reply_all")
	if err != nil {
		t.Fatal(err)
	}
	requireComposedRecipientsSubmit(t, repo, actor, payload,
		[]string{`"ops@desk"@client.test`, `"sales,west"@client.test`}, []string{`"ops desk"@client.test`})
}
