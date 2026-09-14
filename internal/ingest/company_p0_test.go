package ingest

import (
	"context"
	"encoding/json"
	"tabmail/internal/models"
	"testing"
)

func TestP0DurableFailureRetainsOriginal(t *testing.T) {
	st, obj, svc := newDurableCleanupService(t, 8, true)
	svc.maxRetries = 1
	if _, err := svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"user@mail.test"}}, []byte("Subject: oversized\r\n\r\nkeep the original")); err != nil {
		t.Fatal(err)
	}
	svc.ProcessBatch(context.Background())
	jobs, _, err := st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if obj.Count() != 1 || len(jobs) != 1 || jobs[0].State != "dead" {
		t.Fatalf("failed receipt lost recovery evidence: objects=%d jobs=%#v", obj.Count(), jobs)
	}
}
func TestP0ZeroRetentionMeansNullExpiry(t *testing.T) {
	st, _, svc := newDurableCleanupService(t, 1024*1024, true)
	ctx := context.Background()
	r, err := svc.resolver.Resolve(ctx, "user@mail.test")
	if err != nil || r == nil || r.Mailbox == nil {
		t.Fatalf("resolve: %#v %v", r, err)
	}
	tenant, _ := st.GetTenant(ctx, r.Mailbox.TenantID)
	plan, _ := st.GetPlan(ctx, tenant.PlanID)
	plan.RetentionHours = 0
	if err := st.UpdatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.deliver(ctx, Envelope{Source: "smtp", Recipients: []string{"user@mail.test"}}, []byte("Subject: forever\r\n\r\nhello")); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := st.ListMessages(ctx, r.Mailbox.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages: %#v %v", msgs, err)
	}
	b, _ := json.Marshal(msgs[0])
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if body["expires_at"] != nil {
		t.Fatalf("zero retention must use NULL expiry; got %v", body["expires_at"])
	}
}
