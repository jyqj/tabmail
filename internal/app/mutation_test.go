package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"tabmail/internal/hooks"
	"tabmail/internal/models"
)

type mutationContextKey struct{}

type mutationTestTx struct {
	calls []string
}

func (t *mutationTestTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	t.calls = append(t.calls, "begin")
	err := fn(context.WithValue(ctx, mutationContextKey{}, true))
	if err != nil {
		t.calls = append(t.calls, "rollback")
		return err
	}
	t.calls = append(t.calls, "commit")
	return nil
}

type mutationTestAudit struct {
	calls *[]string
	err   error
}

func (a mutationTestAudit) InsertAudit(ctx context.Context, _ *models.AuditEntry) error {
	if ctx.Value(mutationContextKey{}) != true {
		return errors.New("audit escaped transaction context")
	}
	*a.calls = append(*a.calls, "audit")
	return a.err
}

type mutationTestEvents struct {
	calls *[]string
	err   error
}

func (e mutationTestEvents) Enqueue(ctx context.Context, _ hooks.Event) error {
	if ctx.Value(mutationContextKey{}) != true {
		return errors.New("event escaped transaction context")
	}
	*e.calls = append(*e.calls, "event")
	return e.err
}

func TestCommitMutationOrdersMutationAuditAndEventInOneContext(t *testing.T) {
	tx := &mutationTestTx{}
	calls := tx.calls
	// All collaborators share the same backing slice after this assignment.
	audits := mutationTestAudit{calls: &calls}
	events := mutationTestEvents{calls: &calls}
	err := CommitMutation(context.Background(), tx, audits, events,
		models.AuditEntry{Action: "asset.update"},
		&hooks.Event{Type: "asset.updated"},
		func(ctx context.Context) error {
			if ctx.Value(mutationContextKey{}) != true {
				return errors.New("mutation escaped transaction context")
			}
			calls = append(calls, "mutate")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// transaction calls live on tx.calls while collaborator calls live on calls;
	// combine their semantics through the expected callback order.
	if !reflect.DeepEqual(calls, []string{"mutate", "audit", "event"}) {
		t.Fatalf("unexpected mutation order: %v", calls)
	}
	if !reflect.DeepEqual(tx.calls, []string{"begin", "commit"}) {
		t.Fatalf("unexpected transaction lifecycle: %v", tx.calls)
	}
}

func TestCommitMutationFailsClosedOnAuditOrOutboxFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		auditErr error
		eventErr error
		want     []string
	}{
		{name: "audit", auditErr: errors.New("audit down"), want: []string{"mutate", "audit"}},
		{name: "event", eventErr: errors.New("outbox down"), want: []string{"mutate", "audit", "event"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mutationTestTx{}
			calls := []string{}
			err := CommitMutation(context.Background(), tx,
				mutationTestAudit{calls: &calls, err: tc.auditErr},
				mutationTestEvents{calls: &calls, err: tc.eventErr},
				models.AuditEntry{Action: "asset.update"},
				&hooks.Event{Type: "asset.updated"},
				func(context.Context) error { calls = append(calls, "mutate"); return nil })
			if err == nil {
				t.Fatal("expected mutation to fail")
			}
			if appErr, ok := As(err); !ok || appErr.Kind != KindInternal {
				t.Fatalf("expected internal application error, got %T %v", err, err)
			}
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("unexpected calls: got %v want %v", calls, tc.want)
			}
			if !reflect.DeepEqual(tx.calls, []string{"begin", "rollback"}) {
				t.Fatalf("expected rollback, got %v", tx.calls)
			}
		})
	}
}

func TestCommitMutationPreservesApplicationDenialAndSkipsObligations(t *testing.T) {
	tx := &mutationTestTx{}
	calls := []string{}
	err := CommitMutation(context.Background(), tx,
		mutationTestAudit{calls: &calls}, mutationTestEvents{calls: &calls},
		models.AuditEntry{}, &hooks.Event{},
		func(context.Context) error { calls = append(calls, "mutate"); return Forbidden("denied") })
	if appErr, ok := As(err); !ok || appErr.Kind != KindForbidden {
		t.Fatalf("expected forbidden application error, got %T %v", err, err)
	}
	if !reflect.DeepEqual(calls, []string{"mutate"}) {
		t.Fatalf("audit/event should not run after denial: %v", calls)
	}
}
