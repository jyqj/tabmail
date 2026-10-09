package delivery

import (
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"testing"
	"time"
)

func TestRecipientTransitionMatrix(t *testing.T) {
	states := []string{Pending, Accepted, Temporary, Permanent, Uncertain, "unknown", ""}
	for _, from := range states {
		for _, to := range states {
			want := (to == Uncertain && (from == Pending || from == Temporary)) || (from == Uncertain && (to == Accepted || to == Temporary || to == Permanent))
			if got := CanTransitionRecipient(from, to); got != want {
				t.Fatalf("%q -> %q = %v, want %v", from, to, got, want)
			}
		}
	}
	sources := RecipientSources(Uncertain)
	sources[0] = Accepted
	if CanTransitionRecipient(Accepted, Uncertain) {
		t.Fatal("caller mutated shared transition policy")
	}
}

func TestFinalizationMatrix(t *testing.T) {
	for _, tc := range []struct {
		event   Finalization
		flight  bool
		want    models.OutboundState
		invalid bool
	}{
		{FinishSent, false, models.OutboundSent, false}, {FinishSent, true, "", true},
		{FinishRetry, false, models.OutboundRetry, false}, {FinishRetry, true, models.OutboundFailed, false},
		{FinishFailed, false, models.OutboundFailed, false}, {FinishFailed, true, models.OutboundFailed, false},
		{FinishDead, false, models.OutboundDead, false}, {FinishDead, true, models.OutboundDead, false},
		{0, false, "", true},
	} {
		got, err := FinalState(tc.event, tc.flight)
		if got != tc.want || (err != nil) != tc.invalid {
			t.Fatalf("event=%v inFlight=%v: %v, %v", tc.event, tc.flight, got, err)
		}
		if err != nil && !errors.Is(err, ErrInvalidTransition) {
			t.Fatal(err)
		}
	}
}

func TestLeaseExactExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	token := uuid.New()
	job := &models.OutboundJob{State: models.OutboundProcessing, DeliveryToken: &token, LeaseUntil: &now}
	if LeaseOwned(job, &token, now) {
		t.Fatal("exactly expired lease accepted")
	}
	if !LeaseOwned(job, &token, now.Add(-time.Nanosecond)) {
		t.Fatal("live lease rejected")
	}
	other := uuid.New()
	if LeaseOwned(job, &other, now.Add(-time.Second)) || LeaseOwned(job, nil, now.Add(-time.Second)) {
		t.Fatal("wrong token accepted")
	}
	job.State = models.OutboundSent
	if LeaseOwned(job, &token, now.Add(-time.Second)) {
		t.Fatal("completed job lease accepted")
	}
}

func TestReconciliationCannotInventAcceptance(t *testing.T) {
	for _, tc := range []struct {
		states   []string
		want     models.OutboundState
		resolved bool
	}{
		{nil, "", false}, {[]string{Uncertain}, "", false}, {[]string{"unknown"}, "", false},
		{[]string{Accepted, Accepted}, models.OutboundSent, true},
		{[]string{Accepted, Temporary}, models.OutboundFailed, true},
		{[]string{Accepted, Permanent}, models.OutboundFailed, true},
		{[]string{Pending}, models.OutboundFailed, true},
	} {
		got, ok := AfterReconciliation(tc.states)
		if got != tc.want || ok != tc.resolved {
			t.Fatalf("%v => %v,%v", tc.states, got, ok)
		}
	}
}
