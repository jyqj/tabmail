package delivery_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// The adapter constructs real job/ledger models from JSON, not from case IDs.
// FakeStore supplies isolated I/O only; production methods own state, redaction
// and capability policy. It does not certify PostgreSQL or HTTP authorization.
func TestR5ProtocolDeliverySharedCases(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Cases []struct {
			ID    string `json:"id"`
			Input struct {
				Operation      json.RawMessage                             `json:"operation"`
				State          models.OutboundState                        `json:"job_state"`
				InFlightDomain string                                      `json:"in_flight_domain"`
				ContentAllowed bool                                        `json:"content_allowed"`
				Subject        string                                      `json:"subject"`
				TextBody       string                                      `json:"text_body"`
				SMTPResponse   string                                      `json:"smtp_response"`
				DeliveryToken  string                                      `json:"delivery_token"`
				Recipients     []struct{ Category, Address, State string } `json:"recipients"`
			} `json:"input"`
			Expected struct {
				Projection struct {
					Status            string   `json:"status"`
					VisibleAddresses  []string `json:"visible_addresses"`
					HiddenAddresses   []string `json:"hidden_addresses"`
					Retry             bool     `json:"retry"`
					DeliveryUncertain bool     `json:"delivery_uncertain"`
					RetryBlockReason  *string  `json:"retry_block_reason"`
					ForbiddenFields   []string `json:"forbidden_fields"`
				} `json:"projection"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	observations := map[string]company.Submission{}
	executed := 0
	for _, c := range manifest.Cases {
		if string(c.Input.Operation) != `"receipt"` || c.Input.State == "" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			ctx := context.Background()
			st := testutil.NewFakeStore()
			tenant := &models.Tenant{ID: uuid.New(), Name: "r5-shared-policy"}
			st.SeedTenant(tenant)
			owner := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "owner@sender.test", Role: models.RoleUser, IsActive: true}
			if err := st.CreateUser(ctx, owner); err != nil {
				t.Fatal(err)
			}
			zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "sender.test", IsVerified: true, MXVerified: true}
			st.SeedZone(zone)
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant.ID, ZoneID: zone.ID, FullAddress: "owner@sender.test", OwnerUserID: &owner.ID, AccessMode: models.AccessAPIKey}
			st.SeedMailbox(mb)
			token, err := uuid.Parse(c.Input.DeliveryToken)
			if err != nil {
				t.Fatal(err)
			}
			job := &models.OutboundJob{ID: uuid.New(), TenantID: tenant.ID, ZoneID: zone.ID, UserID: &owner.ID, SenderUserID: &owner.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, State: c.Input.State, InFlightDomain: c.Input.InFlightDomain, DeliveryToken: &token, Subject: c.Input.Subject, TextBody: c.Input.TextBody, SMTPResponse: c.Input.SMTPResponse}
			rows := []company.Recipient{}
			states := []string{}
			for _, r := range c.Input.Recipients {
				switch r.Category {
				case "to":
					job.To = append(job.To, r.Address)
				case "cc":
					job.CC = append(job.CC, r.Address)
				case "bcc":
					job.BCC = append(job.BCC, r.Address)
				default:
					t.Fatalf("unsupported recipient category %q", r.Category)
				}
				job.RcptTo = append(job.RcptTo, r.Address)
				states = append(states, r.State)
				rows = append(rows, company.Recipient{Address: r.Address, State: r.State, Diagnostic: "private diagnostic"})
			}
			want := c.Expected.Projection
			status := delivery.DeriveSubmissionStatus(job.State, states)
			if status != want.Status {
				t.Fatalf("production status=%q target=%q", status, want.Status)
			}
			view := submissions.RedactOutboundJobView(job, c.Input.ContentAllowed)
			visible := submissions.FilterRecipientsForJobView(view, rows)
			addresses := []string{}
			recipients := []company.SubmissionRecipient{}
			for _, r := range visible {
				addresses = append(addresses, r.Address)
				recipients = append(recipients, company.SubmissionRecipient{Address: r.Address, State: r.State})
			}
			if !reflect.DeepEqual(addresses, want.VisibleAddresses) {
				t.Fatalf("visible addresses %v target %v", addresses, want.VisibleAddresses)
			}
			if view.DeliveryUncertain != want.DeliveryUncertain {
				t.Fatalf("uncertainty %v target %v", view.DeliveryUncertain, want.DeliveryUncertain)
			}
			if job.InFlightDomain != c.Input.InFlightDomain || job.DeliveryToken == nil || job.TextBody != c.Input.TextBody {
				t.Fatal("projection mutated input job")
			}
			if err := st.CreateOutboundJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			out := outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
			svc := submissions.NewService(nil, st, out, zerolog.Nop())
			actor := authz.Actor{Type: authz.PrincipalUser, ID: owner.ID, TenantID: tenant.ID, Role: owner.Role, Permission: &models.EffectivePermission{CanSend: true}}
			caps := svc.OutboundCapabilities(ctx, tenant, actor, job.ID)
			if caps.Retry != want.Retry || (want.RetryBlockReason != nil && caps.RetryBlockReason != *want.RetryBlockReason) {
				t.Fatalf("production retry capabilities %+v target retry=%v reason=%v", caps, want.Retry, want.RetryBlockReason)
			}
			projection := company.Submission{ID: job.ID, MailboxID: mb.ID, MailFrom: job.MailFrom, Subject: job.Subject, Recipients: recipients, Status: status, DeliveryUncertain: view.DeliveryUncertain, ContentRedacted: true, Capabilities: caps}
			encoded, err := json.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err = json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, name := range want.ForbiddenFields {
				if _, exists := fields[name]; exists {
					t.Fatalf("receipt serialized forbidden field %q", name)
				}
			}
			for _, hidden := range want.HiddenAddresses {
				for _, r := range recipients {
					if r.Address == hidden {
						t.Fatalf("BCC exposed: %s", hidden)
					}
				}
			}
			observations[c.ID] = projection
		})
	}
	if executed != 3 {
		t.Fatalf("expected three shared delivery cases, got %d", executed)
	}
	if destination := os.Getenv("TABMAIL_R5_PROTOCOL_OBSERVATIONS"); destination != "" {
		raw, err := json.Marshal(struct {
			CaseSHA256 string                        `json:"case_sha256"`
			Receipts   map[string]company.Submission `json:"receipts"`
		}{fmt.Sprintf("%x", sha256.Sum256(raw)), observations})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(destination, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
