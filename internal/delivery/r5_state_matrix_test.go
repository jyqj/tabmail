package delivery

import (
	"errors"
	"reflect"
	"testing"

	"tabmail/internal/models"
)

func TestR5RecipientSourcesAndCompletionMatrix(t *testing.T) {
	states := []string{Pending, Accepted, Temporary, Permanent, Uncertain, "cancelled", "unknown", ""}
	sources := map[string][]string{Uncertain: {Pending, Temporary}, Accepted: {Uncertain}, Temporary: {Uncertain}, Permanent: {Uncertain}}
	for _, target := range states {
		if got := RecipientSources(target); !reflect.DeepEqual(got, sources[target]) {
			t.Errorf("sources(%q)=%v want %v", target, got, sources[target])
		}
		if got, want := IsCompletion(target), target == Accepted || target == Temporary || target == Permanent; got != want {
			t.Errorf("completion(%q)=%v want %v", target, got, want)
		}
		for _, current := range states {
			want := false
			for _, source := range sources[target] {
				want = want || source == current
			}
			if got := CanTransitionRecipient(current, target); got != want {
				t.Errorf("%q -> %q = %v want %v", current, target, got, want)
			}
		}
		returned := RecipientSources(target)
		if len(returned) > 0 {
			returned[0] = "corrupt"
			if !reflect.DeepEqual(RecipientSources(target), sources[target]) {
				t.Fatalf("sources(%q) shared mutable slice", target)
			}
		}
	}
}

func TestR5FinalStateMatrix(t *testing.T) {
	table := map[Finalization][2]models.OutboundState{
		FinishSent:   {models.OutboundSent, ""},
		FinishRetry:  {models.OutboundRetry, models.OutboundFailed},
		FinishFailed: {models.OutboundFailed, models.OutboundFailed},
		FinishDead:   {models.OutboundDead, models.OutboundDead},
	}
	for _, event := range []Finalization{0, FinishSent, FinishRetry, FinishFailed, FinishDead, 5, 255} {
		for flag := 0; flag < 2; flag++ {
			want := table[event][flag]
			got, err := FinalState(event, flag == 1)
			if got != want || (want == "") != errors.Is(err, ErrInvalidTransition) {
				t.Errorf("event=%d flight=%v: %q/%v want %q", event, flag == 1, got, err, want)
			}
		}
	}
}

func TestR5ReconciliationLedgerMatrix(t *testing.T) {
	alphabet := []string{Pending, Accepted, Temporary, Permanent, Uncertain, "cancelled", "unknown", ""}
	check := func(ledger []string) {
		known, allAccepted := len(ledger) > 0, len(ledger) > 0
		for _, state := range ledger {
			known = known && (state == Pending || state == Accepted || state == Temporary || state == Permanent)
			allAccepted = allAccepted && state == Accepted
		}
		want := models.OutboundState("")
		if known {
			want = models.OutboundFailed
			if allAccepted {
				want = models.OutboundSent
			}
		}
		if got, resolved := AfterReconciliation(ledger); got != want || resolved != known {
			t.Errorf("%v => %q,%v want %q,%v", ledger, got, resolved, want, known)
		}
	}
	check(nil)
	check([]string{})
	for _, first := range alphabet {
		check([]string{first})
		for _, second := range alphabet {
			check([]string{first, second})
			for _, third := range alphabet {
				check([]string{first, second, third})
			}
		}
	}
}
