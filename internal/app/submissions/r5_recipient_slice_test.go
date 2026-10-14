package submissions

import (
	"bytes"
	"context"
	"errors"
	"net/mail"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// The application accepts slices from callers other than JSON decoding. These
// may share capacity while retaining different recipient roles.
func TestR5SubmissionRecipientSlicesPreserveRoles(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "independent_roles"
		if shared {
			name = "shared_capacity"
		}
		t.Run(name, func(t *testing.T) {
			f := newSubmissionFixture(t)
			backing := []string{"To@Example.test", "unused capacity", "Copy@Example.test", "Hidden@Example.test"}
			before := append([]string(nil), backing...)
			to, cc, bcc := []string{backing[0]}, []string{backing[2]}, []string{backing[3]}
			if shared {
				to, cc, bcc = backing[:1], backing[2:3], backing[3:4]
			}
			job, replayed, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{
				From: f.mb.FullAddress, To: to, CC: cc, BCC: bcc, Subject: "hello", TextBody: "body",
			})
			if failure != nil || replayed || job == nil {
				t.Fatalf("submit = (%v, %v, %+v)", job, replayed, failure)
			}
			if !reflect.DeepEqual(backing, before) || to[0] != before[0] || cc[0] != before[2] || bcc[0] != before[3] {
				t.Errorf("caller recipient data changed: backing=%q, to=%q, cc=%q, bcc=%q", backing, to, cc, bcc)
			}
			persisted, err := f.st.GetOutboundJob(context.Background(), job.ID)
			if err != nil || persisted == nil {
				t.Fatalf("persisted job = (%v, %v)", persisted, err)
			}
			if !reflect.DeepEqual(persisted.To, []string{"to@example.test"}) || !reflect.DeepEqual(persisted.CC, []string{"copy@example.test"}) || !reflect.DeepEqual(persisted.BCC, []string{"hidden@example.test"}) {
				t.Errorf("persisted recipient roles changed: to=%q, cc=%q, bcc=%q", persisted.To, persisted.CC, persisted.BCC)
			}
			if !reflect.DeepEqual(persisted.RcptTo, []string{"to@example.test", "copy@example.test", "hidden@example.test"}) {
				t.Errorf("envelope recipients = %q", persisted.RcptTo)
			}
			raw, err := outbound.Build(outbound.Message{From: persisted.MailFrom, To: persisted.To, CC: persisted.CC, BCC: persisted.BCC, Subject: persisted.Subject, TextBody: persisted.TextBody, MessageID: persisted.MessageIDHeader})
			if err != nil {
				t.Fatal(err)
			}
			message, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if message.Header.Get("To") != "to@example.test" || message.Header.Get("Cc") != "copy@example.test" || message.Header.Get("Bcc") != "" || strings.Contains(string(raw), "hidden@example.test") {
				t.Errorf("MIME recipient privacy changed: To=%q Cc=%q Bcc=%q", message.Header.Get("To"), message.Header.Get("Cc"), message.Header.Get("Bcc"))
			}
		})
	}
}

type r5RecipientSuppressionStore struct {
	*testutil.FakeStore
	stopAt string
	cause  error
	seen   []string
}

func (s *r5RecipientSuppressionStore) IsSuppressed(ctx context.Context, tenant uuid.UUID, address string) (bool, error) {
	s.seen = append(s.seen, address)
	if address == s.stopAt {
		return s.cause == nil, s.cause
	}
	return s.FakeStore.IsSuppressed(ctx, tenant, address)
}

func TestR5SubmissionRecipientSlicesPreserveRejectedInput(t *testing.T) {
	addresses := []string{"to@example.test", "copy@example.test", "hidden@example.test"}
	for index, role := range []string{"to", "cc", "bcc"} {
		for _, failed := range []bool{false, true} {
			name := role + "/suppressed"
			if failed {
				name = role + "/store_error"
			}
			t.Run(name, func(t *testing.T) {
				f := newSubmissionFixture(t)
				st := &r5RecipientSuppressionStore{FakeStore: f.st, stopAt: addresses[index]}
				wantKind := FailureBadRequest
				if failed {
					st.cause = errors.New("controlled suppression failure")
					wantKind = FailureInternal
				}
				f.svc.store = st
				backing := []string{addresses[0], "unused capacity", addresses[1], addresses[2]}
				before := append([]string(nil), backing...)
				job, replayed, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{
					From: f.mb.FullAddress, To: backing[:1], CC: backing[2:3], BCC: backing[3:4], Subject: "hello", TextBody: "body",
				})
				if failure == nil || failure.Kind != wantKind || job != nil || replayed {
					t.Fatalf("submit = (%v, %v, %+v), want failure kind %v", job, replayed, failure, wantKind)
				}
				if !reflect.DeepEqual(backing, before) {
					t.Errorf("failed submission changed caller data: got %q, want %q", backing, before)
				}
				if !reflect.DeepEqual(st.seen, addresses[:index+1]) {
					t.Errorf("suppression lookups = %q, want %q", st.seen, addresses[:index+1])
				}
				count, err := f.st.CountOutboundSince(context.Background(), f.tenant.ID, &f.owner.ID, time.Time{})
				if err != nil || count != 0 {
					t.Fatalf("rejected submission queued %d jobs: %v", count, err)
				}
			})
		}
	}
}
