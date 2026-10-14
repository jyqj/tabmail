//go:build r5protocol

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Adapter/fixture revision 2 preserves the original RC02/BC03 input and markers.
// This is the persisted shape of an old asset whose structured job is already
// gone, not a current enqueue with its COMPLETE snapshot forcibly downgraded.
// All current triggers and constraints remain active. Actual schema-16 upgrade
// is covered separately by TestR5SentRecipientSnapshotUpgradeV16BoundedTrustedBackfill.
func r5UnprovableLegacyAssetV2(t *testing.T, f *companyFixture) *models.OutboundJob {
	t.Helper()
	ctx := context.Background()
	j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"to@fixture.test"}, CC: []string{"cc@fixture.test"}, Subject: "safe receipt", TextBody: "PRIVATE_LEGACY_BODY", HTMLBody: "<b>PRIVATE_LEGACY_BODY</b>", HeadersJSON: json.RawMessage(`{"X-Private":"PRIVATE_LEGACY_HEADER"}`), CreatedAt: time.Now().UTC()}
	_, e := f.pool.Exec(ctx, `INSERT INTO sent_mail_assets(id,tenant_id,sender_mailbox_id,zone_id,mail_from,to_addrs,cc_addrs,subject,text_body,html_body,headers_json,created_at,search_text)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,concat_ws(E'\n',$8::text,$5::text,array_to_string($6::text[],','),array_to_string($7::text[],','),$9::text))`, j.ID, j.TenantID, j.SenderMailboxID, j.ZoneID, j.MailFrom, j.To, j.CC, j.Subject, j.TextBody, j.HTMLBody, j.HeadersJSON, j.CreatedAt)
	must(t, e)
	_, e = f.pool.Exec(ctx, `INSERT INTO sent_mail_items(tenant_id,asset_id,mailbox_id,created_at) VALUES($1,$2,$3,$4)`, j.TenantID, j.ID, j.SenderMailboxID, j.CreatedAt)
	must(t, e)
	var valid bool
	must(t, f.pool.QueryRow(ctx, `SELECT bcc_addrs IS NULL AND recipient_completeness='legacy_unknown' AND recipient_snapshot_version=0 AND NOT EXISTS(SELECT 1 FROM outbound_jobs WHERE id=a.id) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&valid))
	if !valid {
		t.Fatal("historical unprovable fixture premise missing")
	}
	return j
}

func r5AssertUnknownBCCV2(t *testing.T, w *httptest.ResponseRecorder, marker string) {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	var completeness string
	raw, present := envelope.Data["recipient_completeness"]
	if present {
		must(t, json.Unmarshal(raw, &completeness))
	}
	bcc, hasBCC := envelope.Data["bcc"]
	if !present || completeness != "legacy_unknown" || !hasBCC || !bytes.Equal(bytes.TrimSpace(bcc), []byte("null")) {
		t.Errorf("%s: unprovable historical content must explicitly carry legacy_unknown and null BCC", marker)
	}
}

// Strict decoding closes nested fields too. Exact marshaled-value comparison
// also detects absent required false/zero fields that a typed decode would hide.
func r5AssertExpiredReplayReceiptV2(t *testing.T, w *httptest.ResponseRecorder, list bool, job *models.OutboundJob) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	raw := envelope.Data
	if list {
		var rows []json.RawMessage
		must(t, json.Unmarshal(raw, &rows))
		if len(rows) != 1 {
			t.Fatal("list must retain exactly the original receipt")
		}
		raw = rows[0]
	}
	var got company.OutboundReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	must(t, decoder.Decode(&got))
	tenant, attempts := job.TenantID, 0
	retryReason := "state_not_retryable"
	if list {
		retryReason = "unknown"
	}
	expected := company.OutboundReceipt{ID: job.ID, TenantID: &tenant, State: models.OutboundPending, Status: "submitted", Progress: company.OutboundReceiptProgress{Completeness: "known", Counts: &company.OutboundReceiptCounts{Total: 2, Pending: 2}}, CreatedAt: &job.CreatedAt, UpdatedAt: &job.UpdatedAt, AttemptCount: &attempts, Capabilities: &company.SubmissionCapabilities{ViewContent: false, Retry: false, RetryBlockReason: retryReason}}
	wantRaw, e := json.Marshal(expected)
	must(t, e)
	var actual, want any
	must(t, json.Unmarshal(raw, &actual))
	must(t, json.Unmarshal(wantRaw, &want))
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("closed receipt mismatch: got=%s want=%s", raw, wantRaw)
	}
}

// Separate domain: once captured, COMPLETE is durable even if no trustworthy
// source remains. Neither reads nor bounded backfill may overwrite that fact.
func TestR5ProtocolDurableBCCSnapshotV2(t *testing.T) {
	for _, mode := range []string{"missing_source", "same_id_foreign_source"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			j := r5LegacyJob(t, f)
			var before []byte
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&before))
			_, e := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
			must(t, e)
			if mode == "same_id_foreign_source" {
				foreign := &models.Tenant{Name: "durable foreign source", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreign))
				zone := &models.DomainZone{TenantID: foreign.ID, Domain: "durable-foreign.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, zone))
				must(t, f.st.CreateOutboundJob(ctx, &models.OutboundJob{ID: j.ID, TenantID: foreign.ID, ZoneID: zone.ID, MailFrom: "foreign@durable-foreign.test", To: []string{"visible@foreign.test"}, BCC: []string{"FOREIGN_PRIVATE_BCC@foreign.test"}, RcptTo: []string{"visible@foreign.test", "FOREIGN_PRIVATE_BCC@foreign.test"}, State: models.OutboundSent}))
			}
			h := r5SharedRouter(t, f)
			for i := 0; i < 2; i++ {
				w := r5Observed(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil)
				if w.Code != 200 {
					t.Fatal("durable original content unavailable")
				}
				c := r3Data[company.SubmissionContent](t, w)
				if c.ID != j.ID || c.RecipientCompleteness != "complete" || !reflect.DeepEqual(c.BCC, j.BCC) || c.Subject != j.Subject || c.TextBody != j.TextBody || !reflect.DeepEqual(c.To, j.To) || !reflect.DeepEqual(c.CC, j.CC) {
					t.Fatal("durable original recipient snapshot changed")
				}
				_, e = f.st.BackfillSentRecipientSnapshotsV1(ctx, f.tenant.ID, nil, 100)
				must(t, e)
			}
			var after []byte
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&after))
			if !bytes.Equal(before, after) {
				t.Fatal("source removal/replacement or backfill rewrote COMPLETE asset")
			}
		})
	}
}

// Every immutable payload field is identical; each counterexample changes only
// the named provenance axis. A same-ID source is insufficient for backfill.
func TestR5ProtocolLegacyBCCIdentityV2(t *testing.T) {
	for _, axis := range []string{"tenant", "zone", "mailbox"} {
		t.Run(axis, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			j := r5UnprovableLegacyAssetV2(t, f)
			tenant, zone, mailbox := f.tenant.ID, f.zone.ID, f.personal.ID
			switch axis {
			case "tenant":
				foreign := &models.Tenant{Name: "identity-only foreign", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreign))
				tenant = foreign.ID
			case "zone":
				other := &models.DomainZone{TenantID: f.tenant.ID, Domain: "identity-zone.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, other))
				zone = other.ID
			case "mailbox":
				mailbox = f.shared.ID
			}
			// No sender mailbox at INSERT means no second archive is fabricated.
			// UPDATE then establishes an exact payload with one mismatching identity.
			_, e := f.pool.Exec(ctx, `INSERT INTO outbound_jobs(id,tenant_id,zone_id,mail_from,rcpt_to,to_addrs,cc_addrs,bcc_addrs,subject,text_body,html_body,headers_json,template_version_id,created_at,state)
 SELECT id,$2,$3,mail_from,ARRAY['FOREIGN_PRIVATE_BCC@fixture.test'],to_addrs,cc_addrs,ARRAY['FOREIGN_PRIVATE_BCC@fixture.test'],subject,text_body,html_body,headers_json,template_version_id,created_at,'sent' FROM sent_mail_assets WHERE id=$1`, j.ID, tenant, zone)
			must(t, e)
			_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET sender_mailbox_id=$2 WHERE id=$1`, j.ID, mailbox)
			must(t, e)
			var matches bool
			must(t, f.pool.QueryRow(ctx, `SELECT sent_recipient_source_matches_v1(a,j) FROM sent_mail_assets a JOIN outbound_jobs j ON j.id=a.id WHERE a.id=$1`, j.ID).Scan(&matches))
			if matches {
				t.Fatal("single-axis foreign identity admitted")
			}
			var before []byte
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&before))
			h := r5SharedRouter(t, f)
			for i := 0; i < 2; i++ {
				_, e = f.st.BackfillSentRecipientSnapshotsV1(ctx, f.tenant.ID, nil, 100)
				must(t, e)
				w := r5Observed(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil)
				if w.Code != 200 {
					t.Fatal("original historical asset unavailable")
				}
				r5AssertUnknownBCCV2(t, w, "R5_PROTOCOL_CAPABILITY_TARGET_BC03")
				c := r3Data[company.SubmissionContent](t, w)
				if c.ID != j.ID || c.Subject != j.Subject || c.TextBody != j.TextBody || !reflect.DeepEqual(c.To, j.To) || !reflect.DeepEqual(c.CC, j.CC) {
					t.Fatal("identity mismatch changed original content")
				}
			}
			var after []byte
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&after))
			if !bytes.Equal(before, after) {
				t.Fatal("foreign identity backfilled historical asset")
			}
		})
	}
}
