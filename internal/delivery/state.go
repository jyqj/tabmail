// Package delivery is the pure state policy shared by the PostgreSQL adapter,
// the in-memory test adapter and read projections. It does not perform I/O or
// replace the database's atomic tenant/token/lease/row-lock guards.
package delivery

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

const (
	Pending   = "pending"
	Accepted  = "accepted"
	Temporary = "temporary"
	Permanent = "permanent"
	Uncertain = "uncertain"
)

var ErrInvalidTransition = errors.New("invalid outbound delivery transition")

// RecipientSources defines the only legal recipient transitions. Adapters use
// the same matrix: SQL binds these states to its UPDATE guard; FakeStore tests
// the current value against them under its mutex. Returned slices are fresh.
func RecipientSources(target string) []string {
	switch target {
	case Uncertain:
		return []string{Pending, Temporary}
	case Accepted, Temporary, Permanent:
		return []string{Uncertain}
	default:
		return nil
	}
}

func CanTransitionRecipient(current, target string) bool {
	return slices.Contains(RecipientSources(target), current)
}

func IsCompletion(state string) bool {
	return state == Accepted || state == Temporary || state == Permanent
}

// LeaseOwned is the in-memory form of the SQL fencing predicate. Wall time is
// passed in so exact expiry can be tested without sleeps.
func LeaseOwned(job *models.OutboundJob, token *uuid.UUID, now time.Time) bool {
	return job != nil && token != nil && job.DeliveryToken != nil &&
		*token == *job.DeliveryToken && job.State == models.OutboundProcessing &&
		job.LeaseUntil != nil && job.LeaseUntil.After(now)
}

type Finalization uint8

const (
	FinishSent Finalization = iota + 1
	FinishRetry
	FinishFailed
	FinishDead
)

// FinalState makes ambiguous acceptance fail closed: retry while an operation
// is in flight becomes an operator hold, never a second network delivery.
func FinalState(event Finalization, inFlight bool) (models.OutboundState, error) {
	switch event {
	case FinishSent:
		if inFlight {
			return "", ErrInvalidTransition
		}
		return models.OutboundSent, nil
	case FinishRetry:
		if inFlight {
			return models.OutboundFailed, nil
		}
		return models.OutboundRetry, nil
	case FinishFailed:
		return models.OutboundFailed, nil
	case FinishDead:
		return models.OutboundDead, nil
	default:
		return "", ErrInvalidTransition
	}
}

const UncertainRetryPrefix = "Acceptance uncertain; review required: "

// AfterReconciliation only releases the job-level uncertainty marker after all
// recipient evidence is known. An empty or malformed ledger cannot prove that
// a send succeeded and must not be turned into 'sent' by a vacuous SQL test.
func AfterReconciliation(states []string) (models.OutboundState, bool) {
	if len(states) == 0 {
		return "", false
	}
	accepted := 0
	for _, state := range states {
		switch state {
		case Accepted:
			accepted++
		case Pending, Temporary, Permanent:
		default:
			return "", false
		}
	}
	if accepted == len(states) {
		return models.OutboundSent, true
	}
	return models.OutboundFailed, true
}
