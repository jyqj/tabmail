package company

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestOutboundInspectionExplicitAllowlist(t *testing.T) {
	const secret = "PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY"
	token := uuid.New()
	now := time.Now().UTC()
	j := &models.OutboundJob{ID: uuid.New(), TenantID: uuid.New(), State: models.OutboundSent, CreatedAt: now, UpdatedAt: now, MailFrom: "sender@fixture.test", To: []string{"to@fixture.test"}, CC: []string{"cc@fixture.test"}, BCC: []string{"hidden@fixture.test"}, RcptTo: []string{"to@fixture.test", "cc@fixture.test", "hidden@fixture.test"}, Subject: "allowed-content", TextBody: "allowed-body", HTMLBody: "<p>allowed</p>", RecipientLedger: true, HeadersJSON: json.RawMessage(`{"From":"sender@fixture.test","To":"to@fixture.test","Cc":"cc@fixture.test","Subject":"allowed-content","Date":"Thu, 01 Oct 2026 10:00:00 +0000","Message-ID":"<message@fixture.test>","Reply-To":"reply@fixture.test","In-Reply-To":"<prior@fixture.test>","References":"<prior@fixture.test>","MIME-Version":"1.0","Content-Type":"text/plain; charset=utf-8","Content-Transfer-Encoding":"8bit","Content-Disposition":"inline","Content-ID":"<part@fixture.test>","Bcc":"PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY","DKIM-Signature":"PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY","Authorization":"PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY","X-API-Key":"PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY","Received":"PRIVATE-LEASE-DKIM-SMTP-API-JWT-CANARY"}`), LastError: secret, SMTPResponse: secret, RawMIME: []byte(secret), ContentDigest: secret, SubmitActor: secret, IdempotencyKey: secret, RequestHash: secret, DeliveryToken: &token, LeaseUntil: &now, ClaimedAt: &now, InFlightDomain: ""}
	ledger := []Recipient{{Address: j.To[0], State: "accepted", SMTPCode: 250, Diagnostic: "250 2.0.0 " + secret, Attempts: 1}, {Address: j.CC[0], State: "accepted", SMTPCode: 250, Diagnostic: secret, Attempts: 1}, {Address: j.BCC[0], State: "permanent", SMTPCode: 550, Diagnostic: "550 5.1.1 " + secret, Attempts: 2}}
	view := ProjectOutboundInspection(j, ledger)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("explicit inspection projection leaked a forbidden field")
	}
	if view.Job.Status != "partially_accepted" || len(view.Job.Headers) != 14 || view.Recipients[2].Kind != "bcc" || view.Recipients[2].EnhancedCode != "5.1.1" {
		t.Fatal("inspection lost safe headers/private-ledger status/code")
	}
	var value map[string]any
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	job := value["job"].(map[string]any)
	expected := []string{"id", "tenant_id", "state", "status", "created_at", "updated_at", "mail_from", "to", "cc", "bcc", "subject", "text_body", "html_body", "headers"}
	if len(job) != len(expected) {
		t.Fatal("DTO exposed an unexpected model field")
	}
	for _, key := range expected {
		if _, ok := job[key]; !ok {
			t.Fatalf("missing explicit inspection field %s", key)
		}
	}
	view.Job.To[0] = "changed"
	view.Job.CC[0] = "changed"
	view.Job.BCC[0] = "changed"
	view.Job.Headers["From"] = "changed"
	if j.To[0] == "changed" || j.CC[0] == "changed" || j.BCC[0] == "changed" {
		t.Fatal("projection mutated internal job slices")
	}
}
func TestOutboundInspectionFullLedgerStatusEvidence(t *testing.T) {
	j := &models.OutboundJob{State: models.OutboundSent, RecipientLedger: true, RcptTo: []string{"visible@fixture.test", "hidden@fixture.test"}, To: []string{"visible@fixture.test"}, BCC: []string{"hidden@fixture.test"}}
	for _, tc := range []struct {
		name string
		rows []Recipient
		want string
	}{{"next-hop-not-final-delivery", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.BCC[0], State: "accepted"}}, "accepted"}, {"private-failure", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.BCC[0], State: "permanent"}}, "partially_accepted"}, {"private-uncertainty", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.BCC[0], State: "uncertain"}}, "needs_attention"}, {"truncated", []Recipient{{Address: j.To[0], State: "accepted"}}, "needs_attention"}, {"wrong-address", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: "other@fixture.test", State: "accepted"}}, "needs_attention"}, {"duplicate", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.To[0], State: "accepted"}}, "needs_attention"}, {"empty", nil, "needs_attention"}, {"unknown-state", []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.BCC[0], State: "SMTP-secret"}}, "needs_attention"}} {
		t.Run(tc.name, func(t *testing.T) {
			view := ProjectOutboundInspection(j, tc.rows)
			if view.Job.Status != tc.want {
				t.Fatalf("status=%s want=%s", view.Job.Status, tc.want)
			}
			for _, row := range view.Recipients {
				if row.State == "SMTP-secret" {
					t.Fatal("unknown raw state escaped")
				}
			}
		})
	}
	j.InFlightDomain = "internal-marker"
	if ProjectOutboundInspection(j, []Recipient{{Address: j.To[0], State: "accepted"}, {Address: j.BCC[0], State: "accepted"}}).Job.Status != "needs_attention" {
		t.Fatal("in-flight ambiguity suppressed")
	}
}
func TestOutboundInspectionHeadersFailClosed(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`{"From":3}`), json.RawMessage(`not-json`), json.RawMessage(`{"Subject":"safe\r\nX-Secret: secret","Bcc":"secret","X-Custom":"secret"}`)} {
		view := ProjectOutboundInspection(&models.OutboundJob{HeadersJSON: raw}, nil)
		if !reflect.DeepEqual(view.Job.Headers, map[string]string{}) {
			t.Fatal("unsafe/malformed headers did not fail closed")
		}
	}
}
func TestOutboundInspectionEnhancedCodeOnly(t *testing.T) {
	for _, tc := range []struct{ diagnostic, want string }{{"550 5.1.1 secret", "5.1.1"}, {"250 2.0.0 secret", "2.0.0"}, {"4.100.999", "4.100.999"}, {"secret", ""}, {"x5.1.1secret", ""}, {"5.1234.1 secret", ""}, {"6.1.1 secret", ""}} {
		view := ProjectOutboundInspection(&models.OutboundJob{}, []Recipient{{State: "permanent", Diagnostic: tc.diagnostic}})
		if view.Recipients[0].EnhancedCode != tc.want {
			t.Fatalf("enhanced code=%q want=%q", view.Recipients[0].EnhancedCode, tc.want)
		}
	}
}
