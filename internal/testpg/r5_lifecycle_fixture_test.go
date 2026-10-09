//go:build r5fixtures

package testpg_test

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
	"testing"
	"time"
)

func TestR5FixtureLifecycleSealedDraftAndReceipt(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	c := f.Companies[0]
	draft := r5HTTPDraft(t, f)
	var payload []byte
	if err := f.Pool.QueryRow(ctx, `SELECT payload FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/company/employees/" + c.Users["sender"].ID.String() + "/offboard"
	status, raw := f.Request(t, ctx, f.JWT(0, "admin"), "POST", base+"/preview", map[string]any{"successor_user_id": c.Users["reader"].ID, "options": company.OffboardingOptions{Drafts: "seal"}, "reason": "Owned fixture lifecycle disposition"}, "")
	r5RequireStatus(t, status, 200)
	plan := r5Data[company.OffboardingPlan](t, raw)
	status, _ = f.Request(t, ctx, f.JWT(0, "admin"), "POST", base, map[string]any{"plan_id": plan.ID}, "")
	r5RequireStatus(t, status, 200)
	var sealed bool
	var preserved []byte
	var owner uuid.UUID
	var audits, receipts int
	if err := f.Pool.QueryRow(ctx, `SELECT sealed_at IS NOT NULL,payload FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&sealed, &preserved); err != nil {
		t.Fatal(err)
	}
	if err := f.Pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, c.Personal["sender"].ID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := f.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='employee.offboard' AND resource_id=$2`, c.Tenant.ID, c.Users["sender"].ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := f.Pool.QueryRow(ctx, `SELECT count(*) FROM employee_offboarding_plans WHERE id=$1 AND state='executed'`, plan.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if !sealed || !bytes.Equal(payload, preserved) || owner != c.Users["reader"].ID || audits != 1 || receipts != 1 {
		t.Fatal("actual sealed custody/content/once receipt provenance differs")
	}
	status, _ = f.Request(t, ctx, f.JWT(0, "reader"), "GET", "/api/v1/company/drafts/"+draft.ID.String(), nil, "")
	r5RequireStatus(t, status, 404)
	status, _ = f.Request(t, ctx, f.JWT(0, "sender"), "GET", "/api/v1/company/drafts/"+draft.ID.String(), nil, "")
	r5RequireStatus(t, status, 401)
}

func TestR5FixtureIndexSourceLeaseAndObjectBoundary(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := f.Companies[0]
	raw := []byte("From: client@fixture.test\r\nTo: " + c.Personal["sender"].FullAddress + "\r\nSubject: Synthetic index\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nOwned index needle\r\n")
	source := rawobject.NewStore(f.Objects, f.Store)
	m := &models.Message{TenantID: c.Tenant.ID, MailboxID: c.Personal["sender"].ID, ZoneID: c.Zone.ID, Sender: "client@fixture.test", Recipients: []string{c.Personal["sender"].FullAddress}, Subject: "Synthetic index", RawObjectKey: rawobject.Key(raw), Size: int64(len(raw))}
	created, err := source.StoreMessage(ctx, m, raw, 100)
	if err != nil || !created {
		t.Fatal("actual immutable raw/message creation failed", err)
	}
	jobs, err := f.Store.ClaimMailIndexJobs(ctx, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatal("actual index lease missing", err)
	}
	parser := mailcontent.New(f.Objects)
	document, err := parser.Document(ctx, m.ID, m.RawObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if document.MessageID != m.ID || document.SourceKey != m.RawObjectKey || document.SourceSHA256 != company.Hash(string(raw)) || document.ParserVersion != mailcontent.Version {
		t.Fatal("actual parsed immutable provenance differs")
	}
	stale := jobs[0]
	stale.Token = uuid.New()
	if err = f.Store.CompleteMailIndexJob(ctx, stale, *document); err == nil {
		t.Fatal("foreign index lease token wrote document")
	}
	var documents int
	if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE tenant_id=$1 AND message_id=$2`, c.Tenant.ID, m.ID).Scan(&documents); err != nil || documents != 0 {
		t.Fatal("stale index completion changed actual document")
	}
	if err = f.Store.CompleteMailIndexJob(ctx, jobs[0], *document); err != nil {
		t.Fatal(err)
	}
	var key, hash string
	var version int
	if err = f.Pool.QueryRow(ctx, `SELECT source_key,source_sha256,parser_version FROM mail_documents WHERE tenant_id=$1 AND message_id=$2`, c.Tenant.ID, m.ID).Scan(&key, &hash, &version); err != nil {
		t.Fatal(err)
	}
	if key != m.RawObjectKey || hash != document.SourceSHA256 || version != mailcontent.Version {
		t.Fatal("durable index lost real source/version")
	}
	if err = f.Store.FailMailIndexJob(ctx, jobs[0], "stale failure attempt"); err == nil {
		t.Fatal("completed index lease accepted a stale failure")
	}
	if err = f.Objects.FailNext("get", testutil.ErrR5ObjectFault); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/company/mailboxes/" + m.MailboxID.String() + "/messages/" + m.ID.String() + "/source"
	status, body := f.Request(t, ctx, f.JWT(0, "sender"), "GET", path, nil, "")
	r5RequireStatus(t, status, 500)
	if bytes.Contains(body, []byte("Owned index needle")) {
		t.Fatal("actual failed object read returned source bytes")
	}
	status, body = f.Request(t, ctx, f.JWT(0, "sender"), "GET", path, nil, "")
	r5RequireStatus(t, status, 200)
	if !bytes.Equal(body, raw) {
		t.Fatal("actual recovered authorized source differs from immutable bytes")
	}
	status, _ = f.Request(t, ctx, f.JWT(1, "sender"), "GET", path, nil, "")
	r5RequireStatus(t, status, 404)
}
