//go:build r5fixtures

package testpg_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
	"testing"
	"time"
)

// Wait only on authoritative job+recipient terminal predicates. Ticker pacing
// is not an assumption that the network or worker has become ready.
func r5DeliveryWait(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := check()
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func TestR5FixtureWorkerRealSMTPAndRecipientLedger(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		behavior  testutil.R5SMTPBehavior
		job       models.OutboundState
		recipient string
	}{
		{"accepted", testutil.R5SMTPBehavior{}, models.OutboundSent, delivery.Accepted},
		{"rcpt_temporary", testutil.R5SMTPBehavior{RCPTCode: 451}, models.OutboundRetry, delivery.Temporary},
		{"rcpt_permanent", testutil.R5SMTPBehavior{RCPTCode: 550}, models.OutboundFailed, delivery.Permanent},
		{"data_rejected", testutil.R5SMTPBehavior{DataCode: 554}, models.OutboundFailed, delivery.Permanent},
		{"final_reply_lost", testutil.R5SMTPBehavior{DropFinal: true}, models.OutboundFailed, delivery.Uncertain},
		{"accepted_quit_lost", testutil.R5SMTPBehavior{DropQUIT: true}, models.OutboundSent, delivery.Accepted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := testpg.NewR5Fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c := f.Companies[0]
			smtp, err := testutil.StartR5SMTP(ctx, scenario.behavior)
			if err != nil {
				t.Fatal(err)
			}
			defer smtp.Close()
			select {
			case <-smtp.Ready():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			cfg := smtp.Config()
			cfg.MaxRetries = 3
			cfg.BatchSize = 1
			cfg.PollInterval = 10 * time.Millisecond
			cfg.RetryDelay = time.Hour
			svc := outbound.NewService(cfg, f.Store, f.Store, zerolog.Nop())
			svc.SetObjectStore(testutil.NewMemoryObjectStore())
			actor := c.UserActor("sender")
			user := c.Users["sender"].ID
			mailbox := c.Personal["sender"].ID
			job, err := svc.Submit(ctx, outbound.SendRequest{Principal: &actor, TenantID: c.Tenant.ID, UserID: &user, SenderMailboxID: &mailbox, ZoneID: c.Zone.ID, From: c.Personal["sender"].FullAddress, To: []string{"recipient@fixture.test"}, Subject: "Synthetic worker fixture", TextBody: "Synthetic worker body", IdempotencyKey: "owned-worker-command"})
			if err != nil {
				t.Fatal("real domain enqueue failed", err)
			}
			svc.StartWorker(ctx)
			defer func() { cancel(); _ = smtp.Close(); svc.Stop() }()
			err = r5DeliveryWait(ctx, func() (bool, error) {
				var state, recipient string
				err := f.Pool.QueryRow(ctx, `SELECT j.state,r.state FROM outbound_jobs j JOIN outbound_recipients r ON r.tenant_id=j.tenant_id AND r.job_id=j.id WHERE j.id=$1`, job.ID).Scan(&state, &recipient)
				return state == string(scenario.job) && recipient == scenario.recipient, err
			})
			if err != nil {
				t.Fatal("real worker did not reach exact job/recipient outcome", err)
			}
			recipients, err := f.Store.ListOutboundRecipients(ctx, c.Tenant.ID, job.ID)
			if err != nil || len(recipients) != 1 || recipients[0].State != scenario.recipient {
				t.Fatal("actual ledger outcome missing", err)
			}
			events := smtp.Events()
			if len(events) < 4 {
				t.Fatal("real worker did not reach loopback SMTP")
			}
			var begins int
			for _, event := range events {
				if event.Stage == "mail" {
					begins++
				}
			}
			if begins != 1 {
				t.Fatal("single owned command touched MAIL more than once")
			}
			wrong := uuid.New()
			if err = f.Store.CompleteOutboundRecipient(ctx, job.ID, &wrong, "recipient@fixture.test", delivery.Accepted, 250, "stale worker"); err == nil {
				t.Fatal("wrong recipient lease token committed")
			}
			current, err := f.Store.ListOutboundRecipients(ctx, c.Tenant.ID, job.ID)
			if err != nil || len(current) != 1 || current[0].State != scenario.recipient {
				t.Fatal("stale worker changed terminal ledger")
			}
		})
	}
}

func TestR5FixtureMixedLedgerNeverResendsAcceptedRecipient(t *testing.T) {
	f := testpg.NewR5Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := f.Companies[0]
	smtp, err := testutil.StartR5SMTP(ctx, testutil.R5SMTPBehavior{})
	if err != nil {
		t.Fatal(err)
	}
	defer smtp.Close()
	cfg := smtp.Config()
	cfg.MaxRetries = 3
	cfg.PollInterval = 10 * time.Millisecond
	cfg.BatchSize = 1
	cfg.RetryDelay = time.Hour
	svc := outbound.NewService(cfg, f.Store, f.Store, zerolog.Nop())
	svc.SetObjectStore(testutil.NewMemoryObjectStore())
	actor := c.UserActor("sender")
	user := c.Users["sender"].ID
	mailbox := c.Personal["sender"].ID
	recipients := []string{"accepted@fixture.test", "pending@fixture.test"}
	job, err := svc.Submit(ctx, outbound.SendRequest{Principal: &actor, TenantID: c.Tenant.ID, UserID: &user, SenderMailboxID: &mailbox, ZoneID: c.Zone.ID, From: c.Personal["sender"].FullAddress, To: recipients, Subject: "Synthetic mixed fixture", TextBody: "Synthetic mixed fixture body", IdempotencyKey: "owned-mixed-command"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.Store.ClaimOutboundJobs(ctx, time.Now(), 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID || claimed[0].DeliveryToken == nil {
		t.Fatal("actual mixed ledger claim missing", err)
	}
	token := claimed[0].DeliveryToken
	began, err := f.Store.BeginOutboundRecipient(ctx, job.ID, token, recipients[0])
	if err != nil || !began {
		t.Fatal("actual durable first recipient begin failed", err)
	}
	var headers map[string]string
	if len(job.HeadersJSON) > 0 {
		if err = json.Unmarshal(job.HeadersJSON, &headers); err != nil {
			t.Fatal(err)
		}
	}
	mime, err := outbound.Build(outbound.Message{From: job.MailFrom, To: job.To, CC: job.CC, BCC: job.BCC, Subject: job.Subject, TextBody: job.TextBody, HTMLBody: job.HTMLBody, Headers: headers, MessageID: job.MessageIDHeader})
	if err != nil {
		t.Fatal(err)
	}
	if err = outbound.DeliverRelay(ctx, cfg, job.MailFrom, []string{recipients[0]}, mime); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.CompleteOutboundRecipient(ctx, job.ID, token, recipients[0], delivery.Accepted, 250, "Actual loopback next-hop accepted"); err != nil {
		t.Fatal(err)
	}
	before, err := f.Store.ListOutboundRecipients(ctx, c.Tenant.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, r := range before {
		states[r.Address] = r.State
	}
	if states[recipients[0]] != delivery.Accepted || states[recipients[1]] != delivery.Pending {
		t.Fatal("actual mixed accepted/pending fixture was not established")
	}
	if err = f.Store.MarkOutboundJobRetry(ctx, job.ID, token, "Owned mixed fixture continuation", time.Now()); err != nil {
		t.Fatal(err)
	}
	svc.StartWorker(ctx)
	defer func() { cancel(); _ = smtp.Close(); svc.Stop() }()
	if err = r5DeliveryWait(ctx, func() (bool, error) {
		current, e := f.Store.GetOutboundJob(ctx, job.ID)
		return current != nil && current.State == models.OutboundSent, e
	}); err != nil {
		t.Fatal(err)
	}
	after, err := f.Store.ListOutboundRecipients(ctx, c.Tenant.ID, job.ID)
	if err != nil || len(after) != 2 {
		t.Fatal("actual resumed recipient ledger missing", err)
	}
	for _, r := range after {
		if r.State != delivery.Accepted {
			t.Fatal("pending recipient was not actually accepted")
		}
	}
	mails := 0
	for _, event := range smtp.Events() {
		if event.Stage == "mail" {
			mails++
		}
	}
	if mails != 2 {
		t.Fatal("worker resent already accepted recipient or omitted pending one")
	}
	if err = f.Store.CompleteOutboundRecipient(ctx, job.ID, token, recipients[0], delivery.Permanent, 550, "stale original token"); err == nil {
		t.Fatal("old mixed-ledger lease rewrote accepted result")
	}
}
