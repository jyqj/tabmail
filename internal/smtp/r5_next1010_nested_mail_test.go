package smtp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// These tests use the production TCP listener, local SMTP fork, session and
// ingest service. The existing framing fixture substitutes only persistence.
// SMTP MAIL/RCPT state must survive an out-of-sequence MAIL, including a sender
// which would fail origin policy if submitted as a new transaction.
func TestNext1010SMTPNestedMAILPreservesEnvelope(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, transfer := range []string{"DATA", "BDAT"} {
			for _, second := range []struct{ name, from string }{
				{"allowed", "replacement@example.test"},
				{"blocked", "rejected@blocked.test"},
				{"null-path", ""},
			} {
				t.Run(fmt.Sprintf("durable=%t/%s/%s", durable, transfer, second.name), func(t *testing.T) {
					f := newExecFramingFixture(t, durable, 4096)
					next1010RejectOrigin(t, f)
					code, response := f.reply(t, "MAIL FROM:<"+second.from+">\r\n")
					if code != 503 {
						t.Errorf("nested MAIL = %q, want 503 with the original envelope retained", response)
					}
					raw := []byte("Subject: original transaction\r\n\r\nfirst body\r\n")
					next1010Send(t, f, transfer, raw)
					next1010Envelope(t, f, "sender@example.test")

					// DATA and BDAT LAST both end the old transaction. A new
					// accepted MAIL must be usable without an intervening RSET.
					f.expect(t, "MAIL FROM:<next@example.test>\r\n", 250)
					f.expect(t, "DATA\r\n", 502) // No recipient carries over.
					f.expect(t, "RCPT TO:<reader@framing.test>\r\n", 250)
					next := []byte("Subject: following transaction\r\n\r\nsecond body\r\n")
					next1010Send(t, f, transfer, next)
					f.expect(t, "NOOP\r\n", 250)
					f.finish(t)
					f.originals(t, raw, next)
					next1010Envelope(t, f, "sender@example.test", "next@example.test")
				})
			}
		}
	}
}

func TestNext1010SMTPNestedMAILBeforeRCPT(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, first := range []string{"first@example.test", ""} {
			t.Run(fmt.Sprintf("durable=%t/from=%s", durable, first), func(t *testing.T) {
				f := newExecFramingFixture(t, durable, 4096)
				f.expect(t, "RSET\r\n", 250)
				f.expect(t, "MAIL FROM:<"+first+">\r\n", 250)
				code, response := f.reply(t, "MAIL FROM:<replacement@example.test>\r\n")
				if code != 503 {
					t.Errorf("nested MAIL before RCPT = %q, want 503", response)
				}
				f.expect(t, "RCPT TO:<reader@framing.test>\r\n", 250)
				raw := []byte("Subject: accepted reverse path\r\n\r\nbody\r\n")
				next1010Send(t, f, "DATA", raw)
				f.finish(t)
				f.originals(t, raw)
				next1010Envelope(t, f, first)
			})
		}
	}
}

func TestNext1010SMTPNestedMAILResetStartsNewEnvelope(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, reset := range []string{"RSET", "EHLO reset.framing.test"} {
			t.Run(fmt.Sprintf("durable=%t/%s", durable, reset), func(t *testing.T) {
				f := newExecFramingFixture(t, durable, 4096)
				next1010RejectOrigin(t, f)
				code, response := f.reply(t, "MAIL FROM:<replacement@example.test>\r\n")
				if code != 503 {
					t.Errorf("nested MAIL = %q, want 503", response)
				}
				f.expect(t, reset+"\r\n", 250)
				// This is now a valid command sequence, so origin policy must
				// reject the sender and leave MAIL available for a retry.
				f.expect(t, "MAIL FROM:<rejected@blocked.test>\r\n", 550)
				f.expect(t, "MAIL FROM:<fresh@example.test>\r\n", 250)
				f.expect(t, "DATA\r\n", 502)
				f.expect(t, "RCPT TO:<reader@framing.test>\r\n", 250)
				raw := []byte("Subject: after explicit reset\r\n\r\nfresh body\r\n")
				next1010Send(t, f, "BDAT", raw)
				f.finish(t)
				f.originals(t, raw)
				next1010Envelope(t, f, "fresh@example.test")
			})
		}
	}
}

func TestNext1010SMTPNestedMAILDuringBDATRetainsTransfer(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(fmt.Sprintf("durable=%t", durable), func(t *testing.T) {
			f := newExecFramingFixture(t, durable, 4096)
			raw := []byte("Subject: chunk in progress\r\n\r\ncomplete body\r\n")
			prefix, suffix := raw[:17], raw[17:]
			f.expect(t, fmt.Sprintf("BDAT %d\r\n%s", len(prefix), prefix), 250)
			// Preserve the existing response for a transfer already underway.
			f.expect(t, "MAIL FROM:<replacement@example.test>\r\n", 502)
			f.expect(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(suffix), suffix), 250)
			f.finish(t)
			f.originals(t, raw)
			next1010Envelope(t, f, "sender@example.test")
		})
	}
}

func next1010RejectOrigin(t *testing.T, f *execFramingFixture) {
	t.Helper()
	if err := f.st.UpsertSMTPPolicy(context.Background(), &models.SMTPPolicy{
		DefaultAccept: true, DefaultStore: true, RejectOriginDomains: []string{"blocked.test"},
	}); err != nil {
		t.Fatal(err)
	}
	f.server.backend.ingest.InvalidatePolicy()
}

func next1010Send(t *testing.T, f *execFramingFixture, transfer string, raw []byte) {
	t.Helper()
	if transfer == "DATA" {
		f.expect(t, "DATA\r\n", 354)
		f.expect(t, string(raw)+".\r\n", 250)
		return
	}
	f.expect(t, fmt.Sprintf("BDAT %d LAST\r\n%s", len(raw), raw), 250)
}

func next1010Envelope(t *testing.T, f *execFramingFixture, senders ...string) {
	t.Helper()
	want := map[string]int{}
	for _, sender := range senders {
		want[sender]++
	}
	got := map[string]int{}
	wantRecipients := []string{"reader@framing.test"}
	if f.durable {
		jobs, total, err := f.st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 20}, "", "", "")
		if err != nil || total != len(senders) || len(jobs) != len(senders) {
			t.Fatalf("durable envelopes: total=%d jobs=%d err=%v, want %d", total, len(jobs), err, len(senders))
		}
		for _, job := range jobs {
			got[job.MailFrom]++
			if !reflect.DeepEqual(job.Recipients, wantRecipients) {
				t.Errorf("durable recipients = %q, want %q", job.Recipients, wantRecipients)
			}
		}
	} else {
		messages, total, err := f.st.ListMessages(context.Background(), f.mailbox.ID, models.Page{Page: 1, PerPage: 20})
		if err != nil || total != len(senders) || len(messages) != len(senders) {
			t.Fatalf("immediate envelopes: total=%d messages=%d err=%v, want %d", total, len(messages), err, len(senders))
		}
		for _, message := range messages {
			got[message.Sender]++
			if !reflect.DeepEqual(message.Recipients, wantRecipients) {
				t.Errorf("immediate recipients = %q, want %q", message.Recipients, wantRecipients)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("persisted senders = %v, want %v", got, want)
	}
}

type next1010MailPolicyStore struct {
	*testutil.FakeStore
	err error
}

func (s *next1010MailPolicyStore) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.FakeStore.GetSMTPPolicy(ctx)
}

// The wire guard must run before the backend is called. Independently, a
// backend MAIL that fails admission must never publish a rejected sender.
func TestNext1010SMTPMAILPublishesOnlyAfterAdmission(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		for _, rejection := range []string{"origin-policy", "policy-lookup", "sender-syntax", "unsupported-auth"} {
			t.Run(fmt.Sprintf("accepted=%t/%s", accepted, rejection), func(t *testing.T) {
				f := r5NewAdmissionFixture(time.Second, true)
				st := &next1010MailPolicyStore{FakeStore: f.st.FakeStore}
				if err := st.UpsertSMTPPolicy(context.Background(), &models.SMTPPolicy{
					DefaultAccept: true, DefaultStore: true, RejectOriginDomains: []string{"blocked.test"},
				}); err != nil {
					t.Fatal(err)
				}
				rv := resolver.New(st, policy.NamingFull, true)
				svc := ingest.NewService(st, f.obj, rv, nil, nil, models.SMTPPolicy{}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
				s := &session{backend: &backend{cfg: config.SMTP{Timeout: time.Second}, ingest: svc, resolver: rv}, logger: zerolog.Nop()}
				if accepted {
					if err := s.Mail("original@example.test", nil); err != nil {
						t.Fatal(err)
					}
					if err := s.Rcpt("reader@admission.test", nil); err != nil {
						t.Fatal(err)
					}
				}
				fromBefore := s.from
				recipientsBefore := append([]string(nil), s.recipients...)
				resolvedBefore := s.results["reader@admission.test"]
				from, wantCode := "replacement@example.test", 451
				var opts *gosmtp.MailOptions
				switch rejection {
				case "origin-policy":
					from, wantCode = "rejected@blocked.test", 550
				case "policy-lookup":
					st.err = errors.New("injected policy lookup failure")
				case "sender-syntax":
					from, wantCode = "invalid@bad..test", 501
				case "unsupported-auth":
					auth := "claimed@example.test"
					opts = &gosmtp.MailOptions{Auth: &auth}
					wantCode = gosmtp.ErrAuthUnsupported.Code
				}
				svc.InvalidatePolicy()
				var smtpError *gosmtp.SMTPError
				if err := s.Mail(from, opts); !errors.As(err, &smtpError) || smtpError.Code != wantCode {
					t.Fatalf("rejected backend MAIL = %v, want SMTP %d", err, wantCode)
				}
				if s.from != fromBefore || !reflect.DeepEqual(s.recipients, recipientsBefore) || s.results["reader@admission.test"] != resolvedBefore {
					t.Errorf("rejected MAIL changed envelope: from=%q rcpt=%q, want from=%q rcpt=%q", s.from, s.recipients, fromBefore, recipientsBefore)
				}
				st.err = nil
				svc.InvalidatePolicy()
				s.Reset()
				if err := s.Mail("next@example.test", nil); err != nil || s.from != "next@example.test" {
					t.Fatalf("accepted MAIL after reset = %v, from=%q", err, s.from)
				}
				s.Reset()
				if err := s.Mail("", nil); err != nil || s.from != "" {
					t.Errorf("accepted null reverse path = %v, from=%q", err, s.from)
				}
			})
		}
	}
}
